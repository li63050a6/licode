package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"licode/internal/agent"
	"licode/internal/ai"
	"licode/internal/audit"
	"licode/internal/plugin"
	"licode/internal/rag"
	"licode/internal/session"
	"licode/internal/settings"
	"licode/internal/version"
	"licode/internal/web"
	"licode/internal/websocket"
)

// ServeOptions holds resolved configuration for the serve command.
type ServeOptions struct {
	Host        string
	Port        int
	NoSubAgents bool
	Username    string
	Password    string
	HTTPS       bool
	TLSCert     string
	TLSKey      string
	ConfigPath  string
}

// NewServeCommand 返回根命令（licode 直接运行即启动服务器）。
func NewServeCommand() *cobra.Command { return newServeCmd() }

func newServeCmd() *cobra.Command {
	opts := &ServeOptions{}
	c := &cobra.Command{
		Use:   "web",
		Short: "AI 编程助手（Web 界面）",
		Long: `licode web —— 启动 Web 服务器。

启动 Web 服务器，浏览器访问 http://<host>:<port> 即可使用，例如：
    ./licode web --host 0.0.0.0 --port 8080
    ./licode web --password 你的密码            （设置后启用登录，默认用户名 licode）

浏览器访问 http://<host>:<port> 即可使用，支持手机/电脑。
所有 AI 推理都在本服务器执行。设置可在网页端实时修改并写回
~/.licode/config.json，无需重启。`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 配置文件：默认 ~/.licode/config.toml；用 -c 指定其他路径
			cfgPath := opts.ConfigPath
			if cfgPath == "" {
				cfgPath = settings.ConfigTOMLPath()
			}
			cfg, err := settings.LoadTOML(cfgPath)
			if os.IsNotExist(err) {
				cfg = settings.DefaultTOML()
				if gerr := settings.GenerateTOML(cfgPath, cfg); gerr != nil {
					return gerr
				}
				log.Printf("已生成配置文件 %s", cfgPath)
			} else if err != nil {
				return fmt.Errorf("加载配置文件 %s: %w", cfgPath, err)
			}
			if !cmd.Flags().Changed("host") && cfg.Server.Host != "" {
				opts.Host = cfg.Server.Host
			}
			if !cmd.Flags().Changed("port") && cfg.Server.Port != 0 {
				opts.Port = cfg.Server.Port
			}
			if !cmd.Flags().Changed("username") && cfg.Server.Username != "" {
				opts.Username = cfg.Server.Username
			}
			if !cmd.Flags().Changed("password") && cfg.Server.Password != "" {
				opts.Password = cfg.Server.Password
			}
			if !cmd.Flags().Changed("https") && cfg.Server.HTTPS {
				opts.HTTPS = true
			}
			if !cmd.Flags().Changed("tls-cert") && cfg.Server.TLSCert != "" {
				opts.TLSCert = cfg.Server.TLSCert
			}
			if !cmd.Flags().Changed("tls-key") && cfg.Server.TLSKey != "" {
				opts.TLSKey = cfg.Server.TLSKey
			}
			return runServe(opts)
		},
	}
	f := c.Flags()
	f.StringVar(&opts.Host, "host", "127.0.0.1", "监听主机（默认 127.0.0.1，局域网/手机访问用 0.0.0.0）")
	f.IntVar(&opts.Port, "port", 8080, "监听端口")
	f.BoolVar(&opts.NoSubAgents, "no-subagents", false, "禁用子代理编排")
	f.StringVar(&opts.Username, "username", "", "登录用户名（默认 licode；环境变量 LICODE_USERNAME）")
	f.StringVar(&opts.Password, "password", "", "登录密码（环境变量 LICODE_PASSWORD）；未设置则不启用登录")
	f.BoolVar(&opts.HTTPS, "https", false, "启用 HTTPS（未指定证书时自动生成自签名证书）")
	f.StringVar(&opts.TLSCert, "tls-cert", "", "TLS 证书文件路径（cert.pem）")
	f.StringVar(&opts.TLSKey, "tls-key", "", "TLS 私钥文件路径（key.pem）")
	f.StringVarP(&opts.ConfigPath, "config", "c", "", "配置文件路径（默认 ~/.licode/config.toml）")
	return c
}

// listenAddr 计算监听地址 host:port。
func listenAddr(opts *ServeOptions) string {
	return fmt.Sprintf("%s:%d", opts.Host, opts.Port)
}

// serverState 持有可变的全局设置与当前客户端。
type serverState struct {
	mu           sync.RWMutex
	settings     settings.Settings
	client       ai.LLMClient
	shuttingDown bool           // 收到关停信号后置位，拒绝新连接
	rag          *rag.Index     // 特性5：项目源码轻量 RAG 索引（懒构建）
	audit        *audit.Manager // 代码审计任务管理器
}

// connState 保存每个连接独立的会话（多对话）与待确认的工具调用。
type connState struct {
	mu              sync.Mutex
	sessions        *session.Manager
	pending         map[string]chan bool
	askTool         map[string]string // askID -> 工具名
	askSeq          atomic.Int64
	busy            bool
	interruptCancel context.CancelFunc
}

func newConnState(sessionsDir string) *connState {
	return &connState{
		sessions: session.NewManager(sessionsDir, false),
		pending:  map[string]chan bool{},
		askTool:  map[string]string{},
	}
}

func runServe(opts *ServeOptions) error {
	// 首次使用自动生成 ~/.licode 数据目录，并启用日志文件
	_ = settings.EnsureDirs()
	if lf, err := settings.LogFile(); err == nil {
		defer lf.Close()
		log.SetOutput(io.MultiWriter(os.Stderr, lf))
	}

	// 版本计数递增（0.0.0.0 → … → 0.0.0.100 → 0.0.1.0）
	runVersion := version.Bump()
	log.Printf("licode 版本 %s", runVersion)

	// 特性6：工具热加载（fsnotify 监视 ~/.licode/tools/，动态注册/卸载外部命令工具）
	if toolClose, terr := agent.StartExternalToolWatcher(settings.ToolsDir()); terr == nil {
		defer toolClose()
		log.Printf("外部工具热加载已启动: %s", settings.ToolsDir())
	}

	// WASM 插件热加载
	plugin.Default.SetDirs(plugin.Dirs()...)
	pluginCtx, pluginCancel := context.WithCancel(context.Background())
	defer pluginCancel()
	plugin.Default.Start(pluginCtx)

	st := &serverState{}
	st.settings = settings.Defaults()
	st.settings.ApplyFlags(opts.NoSubAgents)
	st.audit = audit.NewManager()

	client, err := st.settings.NewClient()
	if err != nil {
		return err
	}
	st.client = client

	hub := websocket.NewHub()
	hub.OnConnect(func(ctx context.Context, c *websocket.Client) {
		// 关停期间拒绝新连接
		st.mu.RLock()
		shuttingDown := st.shuttingDown
		st.mu.RUnlock()
		if shuttingDown {
			c.SendEvent(websocket.ServerEvent{Type: websocket.EvtError, Error: "服务正在关停，请稍后再试"})
			return
		}
		log.Printf("客户端已连接（当前 %d 个）", hub.Count())
		cs := newConnState(settings.SessionsDir())

		c.OnUserMessage(func(ctx context.Context, msg websocket.ClientMessage) {
			switch msg.Type {
			case websocket.TypeSettingsGet:
				c.SendEvent(websocket.ServerEvent{
					Type: websocket.EvtSettings, Settings: st.settings.Snapshot(),
				})

			case websocket.TypeSettingsSet:
				if err := applyServerSettings(st, msg); err != nil {
					c.SendEvent(websocket.ServerEvent{Type: websocket.EvtError, Error: err.Error()})
				} else {
					// 设置修改同步写回配置文件
					_ = st.settings.Save("")
				}
				c.SendEvent(websocket.ServerEvent{
					Type: websocket.EvtSettings, Settings: st.settings.Snapshot(),
				})

			case websocket.TypeSessionsGet:
				c.SendEvent(websocket.ServerEvent{
					Type: websocket.EvtSessions, Sessions: cs.sessions.List(), SessionID: cs.sessions.CurrentID(),
				})

			case websocket.TypeSessionNew:
				cs.sessions.New()
				_ = cs.sessions.SaveAll()
				c.SendEvent(websocket.ServerEvent{
					Type: websocket.EvtSessions, Sessions: cs.sessions.List(), SessionID: cs.sessions.CurrentID(),
				})

			case websocket.TypeSessionSwitch:
				if cs.sessions.SetCurrent(msg.SessionID) {
					_ = cs.sessions.SaveAll()
					c.SendEvent(websocket.ServerEvent{
						Type: websocket.EvtSessions, Sessions: cs.sessions.List(), SessionID: cs.sessions.CurrentID(),
					})
				}

			case websocket.TypeSessionHistory:
				msgs := cs.sessions.Messages(msg.SessionID)
				if msgs == nil {
					msgs = []ai.Message{}
				}
				c.SendEvent(websocket.ServerEvent{
					Type: websocket.EvtHistory, SessionID: msg.SessionID, Messages: msgs,
				})

			case websocket.TypeSessionRename:
				cs.sessions.Rename(msg.SessionID, msg.Content)
				_ = cs.sessions.SaveAll()
				c.SendEvent(websocket.ServerEvent{
					Type: websocket.EvtSessions, Sessions: cs.sessions.List(), SessionID: cs.sessions.CurrentID(),
				})

			case websocket.TypeSessionDelete:
				cs.sessions.Delete(msg.SessionID)
				_ = cs.sessions.SaveAll()
				c.SendEvent(websocket.ServerEvent{
					Type: websocket.EvtSessions, Sessions: cs.sessions.List(), SessionID: cs.sessions.CurrentID(),
				})

			case websocket.TypeSessionBranch:
				if _, ok := cs.sessions.Branch(msg.SessionID, msg.Index); !ok {
					c.SendEvent(websocket.ServerEvent{Type: websocket.EvtError, Error: "无法创建分支"})
					break
				}
				_ = cs.sessions.SaveAll()
				c.SendEvent(websocket.ServerEvent{
					Type: websocket.EvtSessions, Sessions: cs.sessions.List(), SessionID: cs.sessions.CurrentID(),
				})

			case websocket.TypeAuditLog:
				if strings.TrimSpace(msg.Content) != "" {
					cs.sessions.Current().Add(ai.Message{Role: ai.RoleAssistant, Content: msg.Content})
					_ = cs.sessions.SaveAll()
					c.SendEvent(websocket.ServerEvent{Type: websocket.EvtDone})
				}

			case websocket.TypeAskReply:
				cs.mu.Lock()
				ch, ok := cs.pending[msg.AskID]
				if ok {
					delete(cs.pending, msg.AskID)
				}
				tool := cs.askTool[msg.AskID]
				delete(cs.askTool, msg.AskID)
				cs.mu.Unlock()
				if msg.AskAlways && tool != "" {
					// 当前对话始终允许该工具
					cs.sessions.Current().SetAlwaysAllowed(tool)
				}
				if ok {
					ch <- msg.AskApprove
				}

case websocket.TypeMessage:
			if msg.Content == "/clear" {
				cs.sessions.Current().Clear()
				c.SendEvent(websocket.ServerEvent{Type: websocket.EvtDone})
				return
			}
			cs.mu.Lock()
			if cs.busy {
				cs.mu.Unlock()
				c.SendEvent(websocket.ServerEvent{Type: websocket.EvtError, Error: "上一条消息仍在处理中，请稍候"})
				return
			}
			cs.busy = true
			msgCtx, msgCancel := context.WithCancel(ctx)
			cs.interruptCancel = msgCancel
			cs.mu.Unlock()
			defer func() {
				cs.mu.Lock()
				cs.busy = false
				cs.interruptCancel = nil
				cs.mu.Unlock()
				msgCancel()
			}()
			atts := make([]ai.Attachment, 0, len(msg.Attachments))
			for _, a := range msg.Attachments {
				atts = append(atts, ai.Attachment{Type: a.Type, MIMEType: a.MIMEType, Data: a.Data, Filename: a.Filename})
			}
			runServerAgentWithAttachments(msgCtx, st, cs, c, msg.Content, msg.System, atts)
			_ = cs.sessions.SaveAll()

			case websocket.TypeInterrupt:
				cs.mu.Lock()
				cancel := cs.interruptCancel
				cs.mu.Unlock()
				if cancel != nil {
					cancel()
					c.SendEvent(websocket.ServerEvent{Type: websocket.EvtDone})
				}
			}
		})
	})

	authUser, authPass, authEnabled := ResolveAuth(opts.Username, opts.Password)
	auth := newAuthState(authUser, authPass, authEnabled)
	wsState := newWorkspace()
	searchAPI := newSearchState()

	// 静态资源（CSS/JS/HTMX，全部 go:embed 打进二进制）统一走 /static/
	staticServer := http.StripPrefix("/static/", http.FileServer(http.FS(web.StaticFS())))
	mux := http.NewServeMux()
	mux.Handle("/login", http.HandlerFunc(auth.handleLogin))
	// 健康检查 / 就绪探针（供容器编排 / 负载均衡，不要求登录）
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/ready", handleReady(st))

	// 文件浏览/编辑与工作目录 API
	mux.HandleFunc("/api/files", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		handleFiles(w, r, wsState)
	})
	mux.HandleFunc("/api/file", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		if r.Method == http.MethodGet {
			handleFile(w, r, wsState)
		} else {
			handleSaveFile(w, r, wsState)
		}
	})
	mux.HandleFunc("/api/mkdir", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		handleMkdir(w, r, wsState)
	})
	mux.HandleFunc("/api/export", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		handleExport(w, r)
	})
	mux.HandleFunc("/api/import", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		handleImport(w, r)
	})
	mux.HandleFunc("/api/delete", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		handleDeleteFile(w, r, wsState)
	})
	mux.HandleFunc("/api/chmod", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		handleChmod(w, r, wsState)
	})
	mux.HandleFunc("/api/chown", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		handleChown(w, r, wsState)
	})
	mux.HandleFunc("/api/upload", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		handleUpload(w, r, wsState)
	})
	mux.HandleFunc("/api/download", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		handleDownload(w, r, wsState)
	})
	mux.HandleFunc("/api/workspace", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		handleWorkspace(w, r, wsState)
	})
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"version": version.Current(),
			"counter": version.Parse(version.Current()),
		})
	})
	// 代码审计：状态 / 启动 / 结果 / 一键修复（预览 + 二次确认）
	registerAuditRoutes(mux, st, wsState, hub)
	// 联网搜索：本地库 + 多引擎 meta 搜索 + 网页预览/收录
	mux.HandleFunc("/api/search/engines", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		searchAPI.handleSearchEngines(w, r)
	})
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		searchAPI.handleSearch(w, r)
	})
	mux.HandleFunc("/api/search/fetch", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		searchAPI.handleSearchFetch(w, r)
	})
	mux.HandleFunc("/api/search/save", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		searchAPI.handleSearchSave(w, r)
	})
	mux.HandleFunc("/api/search/catalog", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		searchAPI.handleSearchCatalog(w, r)
	})
	mux.HandleFunc("/api/search/delete", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		searchAPI.handleSearchDelete(w, r)
	})
	mux.HandleFunc("/api/search/stats", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		searchAPI.handleSearchStats(w, r)
	})
	// HTMX 片段：设置弹窗 / 文件树 / 审计面板（服务器渲染 HTML）
	registerFragmentRoutes(mux, auth, st, wsState, hub)
	mux.HandleFunc("/api/models", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		st.mu.RLock()
		cfg := st.settings.AIConfig()
		st.mu.RUnlock()
		q := r.URL.Query()
		if t := q.Get("type"); t != "" {
			cfg.Type = t
		}
		if b := q.Get("base"); b != "" {
			cfg.BaseURL = b
		}
		if p := q.Get("provider"); p != "" {
			cfg.Provider = p
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		models, err := ai.ListModels(ctx, cfg)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"provider": cfg.Provider,
			"type":     cfg.Type,
			"models":   models,
		})
	})
	mux.HandleFunc("/api/auth", func(w http.ResponseWriter, r *http.Request) {
		// 登录信息不需要认证即可查询（用于页面提示）
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":          auth.enabled,
			"username":         auth.user,
			"default_username": DefaultUsername,
		})
	})
	mux.HandleFunc("/api/nodejs", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		out := map[string]any{"node": "", "npx": "", "ok": false}
		if b, err := exec.Command("node", "--version").Output(); err == nil {
			out["node"] = strings.TrimSpace(string(b))
		}
		// npx 依赖 node；检查时报错面给出的信息更友好。
		if b, err := exec.Command("npx", "--version").Output(); err == nil {
			out["npx"] = strings.TrimSpace(string(b))
		}
		out["ok"] = out["node"] != ""
		writeJSON(w, http.StatusOK, out)
	})
	mux.Handle("/_nuxt/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Nuxt 静态产物资源（公开）：登录页（SPA）也需要加载，故不要求认证。
		nuxt := web.NuxtFS()
		p := strings.TrimPrefix(r.URL.Path, "/")
		if f, err := nuxt.Open(p); err == nil {
			f.Close()
			serveNuxtFile(w, r, nuxt, p)
			return
		}
		http.NotFound(w, r)
	}))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		// Nuxt SPA：先尝试直接命中的静态文件，否则回退 index.html 由前端路由接管（如 /login）。
		// index.html 禁止浏览器缓存，保证升级/改版后刷新即可看到最新版本。
		nuxt := web.NuxtFS()
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if f, err := nuxt.Open(p); err == nil {
			f.Close()
			serveNuxtFile(w, r, nuxt, p)
			return
		}
		serveNuxtFile(w, r, nuxt, "index.html")
	}))
	mux.Handle("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		// 静态资源可缓存（版本变更时资源名不变但内容随二进制更新，
		// 配合首页 no-store 保证刷新后引用到的是当前二进制内的文件）。
		w.Header().Set("Cache-Control", "public, max-age=300")
		staticServer.ServeHTTP(w, r)
	}))
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		if !auth.require(w, r) {
			return
		}
		hub.ServeWS(w, r)
	})

	srv := &http.Server{
		Addr:              listenAddr(opts),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	host := listenAddr(opts)
	useTLS := opts.HTTPS || (opts.TLSCert != "" && opts.TLSKey != "")
	scheme := "http"
	if useTLS {
		scheme = "https"
	}
	log.Printf("licode serve 已启动: %s://%s/", scheme, host)
	if strings.HasPrefix(host, "0.0.0.0:") || strings.HasPrefix(host, "0.0.0.0") {
		log.Printf("可用链接: %s://%s:%d/  （局域网/手机访问）", scheme, opts.Host, opts.Port)
	}
	if authEnabled {
		log.Printf("登录已启用（浏览器打开后需登录）")
	} else {
		log.Printf("登录未启用（可用 --password 或环境变量 %s 开启）", EnvPassword)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	// SIGHUP：热重载配置（重读 ~/.licode/config.json 并重建客户端），不中断服务。
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			if err := reloadServerSettings(st); err != nil {
				log.Printf("SIGHUP 重载失败: %v", err)
			} else {
				log.Printf("SIGHUP 已重载配置")
				// RAGSource 可能变化，重建索引
				st.mu.Lock()
				st.rag = nil
				st.mu.Unlock()
			}
		}
	}()
	go func() {
		<-stop
		st.mu.Lock()
		st.shuttingDown = true
		to := st.settings.Snapshot().ShutdownTimeout
		st.mu.Unlock()
		if to <= 0 {
			to = 30
		}
		log.Printf("收到关停信号，优雅退出（等待上限 %ds）…", to)
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(to)*time.Second)
		defer cancel()
		// 等待当前 HTTP/WebSocket 请求（含正在运行的 DAG 子代理与 Shell 脚本）自然完成或超时
		_ = srv.Shutdown(ctx)
		// 关闭 WASM 插件与 MCP 子进程，避免资源泄漏/数据损坏
		plugin.Default.CloseAll()
		agent.CloseMCPClients()
		log.Printf("已停止")
	}()

	if useTLS {
		cert, key := opts.TLSCert, opts.TLSKey
		if cert == "" || key == "" {
			var err error
			cert, key, err = ensureSelfSignedCert()
			if err != nil {
				return fmt.Errorf("自动生成证书失败: %w", err)
			}
			log.Printf("已自动生成自签名证书：%s", cert)
		}
		err = srv.ListenAndServeTLS(cert, key)
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	}
	err = srv.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// applyServerSettings 校验并应用新的设置，重建客户端。
func applyServerSettings(st *serverState, msg websocket.ClientMessage) error {
	data, err := json.Marshal(msg.Settings)
	if err != nil {
		return fmt.Errorf("设置格式错误: %w", err)
	}
	var s settings.Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("设置格式错误: %w", err)
	}
	s.EnsureDefaults()
	if err := s.Validate(); err != nil {
		return fmt.Errorf("设置无效: %v", err)
	}
	client, err := s.NewClient()
	if err != nil {
		return err
	}
	st.mu.Lock()
	st.settings = s.Snapshot()
	st.client = client
	st.mu.Unlock()
	return nil
}

// reloadServerSettings 从磁盘重读配置并重建客户端（热重载，SIGHUP 触发）。
func reloadServerSettings(st *serverState) error {
	s, err := settings.Load()
	if err != nil {
		return err
	}
	client, err := s.NewClient()
	if err != nil {
		return err
	}
	st.mu.Lock()
	st.settings = s.Snapshot()
	st.client = client
	st.mu.Unlock()
	return nil
}

// runServerAgentWithAttachments 在当前设置下运行一次 Agent，支持附件。
func runServerAgentWithAttachments(ctx context.Context, st *serverState, cs *connState, c *websocket.Client, content, roleSystem string, attachments []ai.Attachment) {
	st.mu.RLock()
	s := st.settings.Snapshot()
	client := st.client
	st.mu.RUnlock()

	sess := cs.sessions.Current()
	if s.TitleGen && sess.Title() == "新对话" {
		sess.SetTitle(autoTitle(content))
	}

	ag := s.BuildAgent(client)
	if roleSystem != "" {
		ag.System = roleSystem + "\n" + ag.System
	}
	ag.Session = sess
	ag.Ask = func(ctx context.Context, toolName, args string) (bool, error) {
		if s.AutoAllow || sess.AlwaysAllowed(toolName) {
			return true, nil
		}
		askID := fmt.Sprintf("ask-%d", cs.askSeq.Add(1))
		ch := make(chan bool, 1)
		cs.mu.Lock()
		cs.pending[askID] = ch
		cs.askTool[askID] = toolName
		cs.mu.Unlock()
		c.SendEvent(websocket.ServerEvent{
			Type: websocket.EvtAsk, ToolName: toolName, ToolArgs: args, AskID: askID,
		})
		select {
		case ok := <-ch:
			return ok, nil
		case <-ctx.Done():
			cs.mu.Lock()
			delete(cs.pending, askID)
			delete(cs.askTool, askID)
			cs.mu.Unlock()
			return false, ctx.Err()
		}
	}

	stream := true
	if s.Streaming != nil {
		stream = *s.Streaming
	}
	if s.RAGEnabled {
		if snippets := st.ragLookup(content, s.RAGSource, s.RAGTopFiles); snippets != "" {
			ag.System += "\n\n以下是用户当前项目中的相关源码片段（来自 RAG 检索），" +
				"请优先据此准确回答，不要编造不存在的内容：\n" + snippets
		}
	}
	var textBuf strings.Builder
	_ = ag.RunWithAttachments(ctx, content, attachments, func(e agent.Event) error {
		if e.Type == agent.EventText && !stream {
			textBuf.WriteString(e.Content)
		} else if e.Type == agent.EventText {
			c.SendEvent(websocket.ServerEvent{Type: websocket.EvtDelta, Content: e.Content})
		} else if e.Type == agent.EventToolStart {
			c.SendEvent(websocket.ServerEvent{
				Type: websocket.EvtToolStart, ToolName: e.ToolName, ToolArgs: e.ToolArgs,
			})
		} else if e.Type == agent.EventToolDone {
			c.SendEvent(websocket.ServerEvent{
				Type: websocket.EvtToolDone, ToolName: e.ToolName, ToolOut: e.ToolOut,
			})
		} else if e.Type == agent.EventDone {
			if !stream && textBuf.Len() > 0 {
				c.SendEvent(websocket.ServerEvent{Type: websocket.EvtDelta, Content: textBuf.String()})
			}
			c.SendEvent(websocket.ServerEvent{Type: websocket.EvtDone})
		} else if e.Type == agent.EventError {
			c.SendEvent(websocket.ServerEvent{Type: websocket.EvtError, Error: e.Error})
		} else if e.Type == agent.EventStatus {
			c.SendEvent(websocket.ServerEvent{Type: websocket.EvtStatus, Content: e.Content})
		}
		return nil
	})
}

// runServerAgent 在当前设置下运行一次 Agent，流式回传事件。
func runServerAgent(ctx context.Context, st *serverState, cs *connState, c *websocket.Client, content, roleSystem string) {
	runServerAgentWithAttachments(ctx, st, cs, c, content, roleSystem, nil)
}

// sessionStats 估算会话 token 用量并附带提供商/模型/上下文/缓存统计。
func sessionStats(s *session.Session) map[string]any {
	messages := s.Messages()
	tokens := 0
	for _, m := range messages {
		tokens += session.EstimateTokens(m.Content)
		for _, tc := range m.ToolCalls {
			tokens += session.EstimateTokens(tc.Function.Arguments)
		}
	}
	maxTok := s.MaxTokens()
	ctxPct := 0
	if maxTok > 0 && tokens > 0 {
		ctxPct = int(float64(tokens) / float64(maxTok) * 100)
		if ctxPct > 100 {
			ctxPct = 100
		}
		if ctxPct < 1 {
			ctxPct = 1
		}
	}
	u := s.Usage()
	hit, hitRate := 0, 0
	if u.CachedTokens > 0 {
		hit = u.CachedTokens
	}
	if in := u.InputTokens + u.CachedTokens; in > 0 {
		hitRate = int(float64(hit) / float64(in) * 100)
	}
	return map[string]any{
		"messages":         len(messages),
		"context_tokens":   tokens,
		"context_max":      maxTok,
		"context_pct":      ctxPct,
		"provider":         "",
		"model":            "",
		"conversation_in":  u.InputTokens,
		"conversation_out": u.OutputTokens,
		"usage_cached":     hit,
		"cache_hit_rate":   hitRate,
		"always_allow":     s.AlwaysAllowedList(),
	}
}

// autoTitle 从第一条用户消息生成对话标题。
func autoTitle(content string) string {
	trimmed := strings.TrimSpace(content)
	if utf8.RuneCountInString(trimmed) > 18 {
		runes := []rune(trimmed)
		return string(runes[:18])
	}
	return trimmed
}

// serveNuxtFile 以正确的 Content-Type 与缓存策略提供 Nuxt 静态产物文件。
// .html 不缓存（改版即时生效）；/_nuxt/ 资源名带内容哈希，可长缓存。
func serveNuxtFile(w http.ResponseWriter, r *http.Request, nuxt fs.FS, name string) {
	if _, err := fs.Stat(nuxt, name); err != nil {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(name, ".html") {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=604800")
	}
	http.ServeFileFS(w, r, nuxt, name)
}

func mapEventType(t agent.EventType) string {
	switch t {
	case agent.EventText:
		return websocket.EvtDelta
	case agent.EventToolStart:
		return websocket.EvtToolStart
	case agent.EventToolDone:
		return websocket.EvtToolDone
	case agent.EventDone:
		return websocket.EvtDone
	case agent.EventError:
		return websocket.EvtError
	case agent.EventStatus:
		return websocket.EvtStatus
	case agent.EventAsk:
		return websocket.EvtAsk
	default:
		return websocket.EvtStatus
	}
}

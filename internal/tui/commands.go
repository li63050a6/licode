package tui

import (
	"strconv"
	"strings"
)

var settingFields = []string{"provider", "model", "base_url", "api_key", "temperature", "max_tokens", "max_iterations", "plan_exclude"}

func commandList() []cmdItem {
	return []cmdItem{
		{name: "new", title: "新建会话", category: "session", run: cmdNew},
		{name: "delete", title: "删除当前会话", category: "session", run: cmdDelete},
		{name: "branch", title: "复制当前会话为分支", category: "session", run: cmdBranch},
		{name: "clear", title: "清空当前对话", category: "session", run: cmdClear},
		{name: "sessions", title: "打开会话面板", category: "session", run: cmdSessions},
		{name: "set", title: "打开设置面板", category: "settings", run: cmdSet},
		{name: "plan", title: "切换到 Plan 只读模式", category: "action", run: cmdPlan},
		{name: "build", title: "切换到 Build 可执行模式", category: "action", run: cmdBuild},
		{name: "help", title: "显示帮助", category: "action", run: cmdHelp},
	}
}

func cmdNew(m *Model) {
	m.backend.NewSession()
	m.clearLines()
	m.listItems = m.sessionList()
	m.showToast("新会话已创建")
}

func cmdDelete(m *Model) {
	m.backend.DeleteSession(m.backend.CurrentID())
	m.clearLines()
	m.listItems = m.sessionList()
	m.showToast("会话已删除")
}

func cmdBranch(m *Model) {
	m.backend.BranchSession()
	m.listItems = m.sessionList()
	m.showToast("已生成分支会话")
}

func cmdClear(m *Model) {
	m.clearLines()
	m.showToast("对话已清空")
}

func cmdSessions(m *Model) {
	m.listItems = m.sessionList()
	m.listSelected = 0
	m.listOpen = true
}

func cmdSet(m *Model) {
	m.settingField = 0
	m.settingOpen = true
}

func cmdPlan(m *Model) {
	m.mode = ModePlan
	m.showToast("PLAN 只读模式：AI 仅思考，不会写入文件或执行命令")
}

func cmdBuild(m *Model) {
	m.mode = ModeBuild
	m.showToast("BUILD 模式：AI 可以读写文件、执行命令")
}

func cmdHelp(m *Model) {
	m.appendBody(kindNote, colorMuted, "可用命令")
	m.appendBody(kindText, colorText, "/new  新建会话 · /delete 删除 · /branch 复制 · /clear 清空")
	m.appendBody(kindText, colorText, "/sessions 会话列表 · /set 设置 · /help 帮助")
	m.appendBody(kindText, colorText, "/plan 只读 · /build 可执行")
	m.appendBody(kindNote, colorMuted, "快捷键：Tab 切换 BUILD/PLAN，Ctrl+C 两次退出，Esc 关闭菜单")
}

func (m *Model) clearLines() { m.lines = nil }

func (m *Model) appendBody(kind lineKind, color, text string) {
	m.lines = append(m.lines, line{kind: kind, color: color, text: text})
}

func (m *Model) sessionList() []sessItem {
	infos := m.backend.Sessions()
	items := make([]sessItem, 0, len(infos))
	for _, s := range infos {
		t := s.Title
		if t == "" {
			t = "未命名会话"
		}
		rs := []rune(t)
		if len(rs) > 30 {
			t = string(rs[:30]) + "…"
		}
		items = append(items, sessItem{id: s.ID, title: t})
	}
	return items
}

// doSetting 处理 /set key value
func (m *Model) doSetting(text string) {
	parts := strings.SplitN(text, " ", 3)
	if len(parts) < 3 || strings.TrimSpace(parts[2]) == "" {
		m.settingField = 0
		m.settingOpen = true
		return
	}
	key, val := parts[1], strings.TrimSpace(parts[2])

	if key == "plan_exclude" {
		m.setPlanExclude(val)
		m.showToast("plan_exclude 已更新: " + val)
		return
	}

	s := m.backend.Settings()
	switch key {
	case "provider":
		s.Provider = val
	case "model":
		s.Model = val
	case "base_url":
		s.BaseURL = strings.TrimRight(val, "/")
	case "api_key":
		s.APIKey = val
	case "temperature":
		if v, ok := parseFloat(val); ok {
			s.Temperature = v
		}
	case "max_tokens":
		if v, ok := parseIntVal(val); ok && v > 0 {
			s.MaxTokens = v
		}
	case "max_iterations":
		if v, ok := parseIntVal(val); ok && v > 0 {
			s.MaxIterations = v
		}
	default:
		m.showToast("未知设置项: " + key)
		return
	}
	_ = s.Save("")
	m.backend.RefreshClients()
	m.showToast("已更新 " + key)
}

func parseFloat(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func parseIntVal(s string) (int, bool) {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, false
	}
	return v, true
}

func (m *Model) modelName() string {
	s := m.backend.Settings()
	if s != nil {
		if s.Model != "" {
			return s.Model
		}
		if s.Provider != "" {
			return s.Provider
		}
	}
	return "llm"
}

func (m *Model) settingValue(key string) string {
	s := m.backend.Settings()
	switch key {
	case "provider":
		return s.Provider
	case "model":
		return s.Model
	case "base_url":
		if s.BaseURL == "" {
			return "（默认）"
		}
		return s.BaseURL
	case "api_key":
		if s.APIKey == "" {
			return "（未设置）"
		}
		return "********"
	case "temperature":
		return strconv.FormatFloat(float64(s.Temperature), 'f', 2, 32)
	case "max_tokens":
		return strconv.Itoa(s.MaxTokens)
	case "max_iterations":
		return strconv.Itoa(s.MaxIterations)
	case "plan_exclude":
		return m.planExclude
	}
	return ""
}

func (m *Model) settingHint(key string) string {
	switch key {
	case "provider":
		return "LLM 提供商，例如 openai / anthropic / deepseek / qwen"
	case "model":
		return "模型名，例如 deepseek-chat / gpt-4o-mini"
	case "base_url":
		return "API 地址"
	case "api_key":
		return "API 密钥"
	case "temperature":
		return "采样温度 0.0 ~ 2.0"
	case "max_tokens":
		return "单次回复最大 token"
	case "max_iterations":
		return "最大工具调用轮数"
	case "plan_exclude":
		return "PLAN 只读模式禁用的工具"
	}
	return ""
}

func (m *Model) usage() string {
	total := 0
	for _, l := range m.lines {
		total += len([]byte(l.text)) + len(l.payload)
	}
	if total < 1024 {
		return ""
	}
	if total < 1024*1024 {
		return strconv.Itoa(total/1024) + " KB"
	}
	return strconv.Itoa(total/(1024*1024)) + " MB"
}
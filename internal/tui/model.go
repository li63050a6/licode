package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"licode/internal/agent"
	"licode/internal/settings"
)

// Msg 类型
type eventMsg agent.Event

// Model 是 TUI 主模型
type Model struct {
	backend *Backend
	width   int
	height  int

	// 聊天
	messages []agent.Event
	input    string

	// 历史
	history   []string
	histIndex int

	// 状态
	running bool
	busy    bool

	// 面板
	showSessions bool
	showSettings bool
	sessions     []sessionInfo
	sessSelected int
	settingField int

	// 设置字段名（用于编辑）
	settingKeys []string

	// 事件通道
	eventCh chan agent.Event
}

type sessionInfo struct {
	id    string
	title string
	count int
}

func NewModel(backend *Backend) *Model {
	return &Model{
		backend: backend,
		eventCh: make(chan agent.Event, 128),
		sessions: make([]sessionInfo, 0, 8),
		settingKeys: []string{
			"provider", "model", "base_url", "api_key",
			"temperature", "max_tokens", "max_iterations",
		},
	}
}

func (m *Model) Init() tea.Cmd {
	m.refreshSessions()
	return nil
}

func (m *Model) refreshSessions() {
	infos := m.backend.Sessions()
	m.sessions = make([]sessionInfo, len(infos))
	for i, s := range infos {
		m.sessions[i] = sessionInfo{id: s.ID, title: s.Title, count: s.Count}
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(v)
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
	case eventMsg:
		m.messages = append(m.messages, agent.Event(v))
		if v.Type == agent.EventDone || v.Type == agent.EventError {
			m.busy = false
			m.running = false
		}
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// 设置面板优先
	if m.showSettings {
		return m.handleSettings(msg)
	}
	// 会话面板
	if m.showSessions {
		return m.handleSessions(msg)
	}
	// 主聊天视图
	switch msg.String() {
	case "ctrl+c":
		if m.running {
			m.backend.Interrupt()
			return m, nil
		}
		return m, tea.Quit
	case "tab":
		m.showSessions = true
		m.refreshSessions()
		return m, nil
	case "`":
		m.showSettings = true
		return m, nil
	case "enter":
		return m.sendInput()
	case "backspace":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	case "up":
		if len(m.history) > 0 && m.histIndex < len(m.history)-1 {
			if m.histIndex == -1 {
				m.histIndex = len(m.history) - 1
			} else if m.histIndex > 0 {
				m.histIndex--
			}
			m.input = m.history[m.histIndex]
		}
	case "down":
		if m.histIndex >= 0 {
			m.histIndex++
			if m.histIndex >= len(m.history) {
				m.histIndex = -1
				m.input = ""
			} else {
				m.input = m.history[m.histIndex]
			}
		}
	case "ctrl+u":
		m.input = ""
	default:
		if msg.Type == tea.KeyRunes && len(msg.String()) == 1 {
			m.input += msg.String()
		}
	}
	return m, nil
}

func (m *Model) handleSessions(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab", "q", "esc":
		m.showSessions = false
	case "j", "down":
		if m.sessSelected < len(m.sessions)-1 {
			m.sessSelected++
		}
	case "k", "up":
		if m.sessSelected > 0 {
			m.sessSelected--
		}
	case "enter":
		if m.sessSelected < len(m.sessions) {
			m.backend.SwitchSession(m.sessions[m.sessSelected].id)
			m.messages = nil
		}
		m.showSessions = false
	case "n":
		m.backend.NewSession()
		m.refreshSessions()
		m.showSessions = false
		m.messages = nil
	case "d":
		if m.sessSelected < len(m.sessions) {
			m.backend.DeleteSession(m.sessions[m.sessSelected].id)
			m.refreshSessions()
			if m.sessSelected >= len(m.sessions) && m.sessSelected > 0 {
				m.sessSelected--
			}
		}
	}
	return m, nil
}

func (m *Model) handleSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "`", "q", "esc":
		m.showSettings = false
	case "j", "down":
		if m.settingField < len(m.settingKeys)-1 {
			m.settingField++
		}
	case "k", "up":
		if m.settingField > 0 {
			m.settingField--
		}
	case "enter":
		m.editSetting()
	}
	return m, nil
}

func (m *Model) editSetting() {
	key := m.settingKeys[m.settingField]
	s := m.backend.Settings()
	current := settingValue(s, key)
	m.input = fmt.Sprintf("/set %s %s", key, current)
}

func (m *Model) sendInput() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input)
	if text == "" || m.busy {
		return m, nil
	}
	m.history = append(m.history, text)
	m.histIndex = -1
	m.input = ""

	// 处理 /set 命令
	if strings.HasPrefix(text, "/set ") {
		parts := strings.SplitN(text, " ", 3)
		if len(parts) == 3 {
			m.backend.UpdateSettings(func(s *settings.Settings) {
				updateSettingField(s, parts[1], parts[2])
			})
			m.backend.RefreshClients()
			m.messages = append(m.messages, agent.Event{
				Type:    agent.EventText,
				Content: fmt.Sprintf("⚙️ 已更新设置: %s = %s", parts[1], parts[2]),
			})
		}
		return m, nil
	}

	// 添加用户消息
	m.messages = append(m.messages, agent.Event{Type: agent.EventText, Content: "你: " + text})

	events, err := m.backend.RunAgent(context.Background(), text)
	if err != nil {
		m.messages = append(m.messages, agent.Event{Type: agent.EventError, Content: err.Error()})
		return m, nil
	}
	m.busy = true
	m.running = true

	go func() {
		for e := range events {
			m.eventCh <- e
		}
	}()

	return m, tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg {
		select {
		case e := <-m.eventCh:
			return eventMsg(e)
		default:
			return nil
		}
	})
}

func (m *Model) View() string {
	if m.showSessions {
		return m.viewSessions()
	}
	if m.showSettings {
		return m.viewSettings()
	}
	return m.viewChat()
}

func (m *Model) viewChat() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(" licode TUI ") + "\n")

	chatH := m.height - 6
	if chatH < 3 {
		chatH = 3
	}
	msgStr := m.renderMessages()
	lines := strings.Split(msgStr, "\n")
	if len(lines) > chatH {
		lines = lines[len(lines)-chatH:]
	}
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	for i := len(lines); i < chatH; i++ {
		b.WriteString("\n")
	}

	b.WriteString(strings.Repeat("─", m.width) + "\n")
	b.WriteString(m.viewStatus() + "\n")
	b.WriteString("› ")
	if m.input == "" {
		b.WriteString(helpStyle.Render("输入消息，Enter 发送，Tab 会话，` 设置，Ctrl+C 退出..."))
	} else {
		b.WriteString(m.input)
	}
	return b.String()
}

func (m *Model) renderMessages() string {
	var lines []string
	for _, evt := range m.messages {
		switch evt.Type {
		case agent.EventText:
			if strings.HasPrefix(evt.Content, "你: ") {
				lines = append(lines, userMsgStyle.Render("你: ")+evt.Content[3:])
			} else {
				lines = append(lines, aiMsgStyle.Render("AI: ")+evt.Content)
			}
		case agent.EventToolStart:
			lines = append(lines, toolStyle.Render(fmt.Sprintf("🔧 %s(%s)", evt.ToolName, evt.ToolArgs)))
		case agent.EventToolDone:
			out := evt.ToolOut
			if len(out) > 100 {
				out = out[:100] + "…"
			}
			lines = append(lines, toolStyle.Render(fmt.Sprintf("✅ %s → %s", evt.ToolName, out)))
		case agent.EventError:
			lines = append(lines, errorStyle.Render("❌ "+evt.Error))
		case agent.EventStatus:
			lines = append(lines, statusStyle.Render(evt.Content))
		}
	}
	return strings.Join(lines, "\n")
}

func (m *Model) viewStatus() string {
	s := m.backend.Settings()
	parts := []string{
		fmt.Sprintf("模型: %s", s.Model),
		fmt.Sprintf("提供商: %s", s.Provider),
	}
	if m.running {
		parts = append(parts, statusStyle.Render("运行中..."))
	}
	u := m.backend.SessionUsage()
	if u.InputTokens > 0 {
		parts = append(parts, fmt.Sprintf("Token: %d/%d", u.InputTokens, u.OutputTokens))
	}
	return statusStyle.Render(" " + strings.Join(parts, " · ") + " ")
}

func (m *Model) viewSessions() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(" 会话列表 ") + "\n\n")
	for i, s := range m.sessions {
		style := baseStyle
		if i == m.sessSelected {
			style = selectedStyle
		}
		title := s.title
		if len(title) > 20 {
			title = title[:20] + "…"
		}
		b.WriteString(style.Render(fmt.Sprintf(" %s (%d 条)", title, s.count)) + "\n")
	}
	b.WriteString("\n" + helpStyle.Render(" j/k 移动 · enter 切换 · n 新建 · d 删除 · tab/` 返回"))
	return b.String()
}

func (m *Model) viewSettings() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(" 设置 ") + "\n\n")
	for i, key := range m.settingKeys {
		style := baseStyle
		if i == m.settingField {
			style = selectedStyle
		}
		val := settingValue(m.backend.Settings(), key)
		b.WriteString(style.Render(fmt.Sprintf(" %-16s %s", key+":", val)) + "\n")
	}
	b.WriteString("\n" + helpStyle.Render(" j/k 移动 · enter 编辑 · ` 关闭"))
	return b.String()
}

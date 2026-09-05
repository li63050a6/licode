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

type eventMsg agent.Event

type SlashCommand struct {
	Name        string
	Description string
	Category    string
}

type Mode int

const (
	ModeBuild Mode = iota
	ModePlan
)

type Model struct {
	backend *Backend
	width   int
	height  int

	mode Mode

	messages []agent.Event
	input    string

	history   []string
	histIndex int

	running bool
	busy    bool

	showSessions bool
	showSettings bool
	sessions     []sessionInfo
	sessSelected int
	settingField int
	settingKeys  []string

	showSlashMenu bool
	slashIndex    int
	slashResults  []SlashCommand

	eventCh  chan agent.Event
	commands []SlashCommand
}

type sessionInfo struct {
	id    string
	title string
	count int
}

func NewModel(backend *Backend) *Model {
	m := &Model{
		backend:      backend,
		mode:         ModeBuild,
		eventCh:      make(chan agent.Event, 128),
		sessions:     make([]sessionInfo, 0, 8),
		settingKeys:  []string{"provider", "model", "base_url", "api_key", "temperature", "max_tokens", "max_iterations"},
		slashResults: make([]SlashCommand, 0),
		commands:     buildCommands(),
	}
	return m
}

func buildCommands() []SlashCommand {
	return []SlashCommand{
		{Name: "/new", Description: "新建对话", Category: "session"},
		{Name: "/delete", Description: "删除当前会话", Category: "session"},
		{Name: "/branch", Description: "复制当前会话为分支", Category: "session"},
		{Name: "/clear", Description: "清空当前对话", Category: "session"},
		{Name: "/sessions", Description: "打开会话面板", Category: "session"},
		{Name: "/set", Description: "打开设置面板", Category: "settings"},
		{Name: "/help", Description: "显示帮助", Category: "action"},
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
	if m.showSlashMenu {
		return m.handleSlashMenu(msg)
	}
	if m.showSettings {
		return m.handleSettings(msg)
	}
	if m.showSessions {
		return m.handleSessions(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		if m.running {
			m.backend.Interrupt()
			return m, nil
		}
		return m, tea.Quit
	case "tab":
		if m.mode == ModeBuild {
			m.mode = ModePlan
			m.messages = append(m.messages, agent.Event{Type: agent.EventText, Content: "已切换到 Plan 模式"})
		} else {
			m.mode = ModeBuild
			m.messages = append(m.messages, agent.Event{Type: agent.EventText, Content: "已切换到 Build 模式"})
		}
		return m, nil
	case "enter":
		return m.sendInput()
	case "backspace":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
			m.updateSlashMenu()
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
		m.showSlashMenu = false
	default:
		if msg.Type == tea.KeyRunes && len(msg.String()) == 1 {
			m.input += msg.String()
			m.updateSlashMenu()
		}
	}
	return m, nil
}

func (m *Model) updateSlashMenu() {
	input := strings.TrimSpace(m.input)
	if !strings.HasPrefix(input, "/") {
		m.showSlashMenu = false
		m.slashResults = nil
		return
	}

	query := strings.TrimPrefix(input, "/")
	query = strings.TrimSpace(query)

	var results []SlashCommand
	for _, cmd := range m.commands {
		name := strings.TrimPrefix(cmd.Name, "/")
		if strings.HasPrefix(name, query) || query == "" {
			results = append(results, cmd)
		}
	}

	if len(results) > 0 {
		m.showSlashMenu = true
		m.slashResults = results
		m.slashIndex = 0
	} else {
		m.showSlashMenu = false
	}
}

func (m *Model) handleSlashMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.showSlashMenu = false
		m.input = ""
		return m, nil
	case "enter":
		if m.slashIndex < len(m.slashResults) {
			cmd := m.slashResults[m.slashIndex]
			m.executeCommand(cmd)
			m.input = ""
			m.showSlashMenu = false
			m.slashResults = nil
			return m, nil
		}
	case "backspace":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
			m.updateSlashMenu()
		}
	case "down", "j":
		if m.slashIndex < len(m.slashResults)-1 {
			m.slashIndex++
		}
	case "up", "k":
		if m.slashIndex > 0 {
			m.slashIndex--
		}
	default:
		if msg.Type == tea.KeyRunes && len(msg.String()) == 1 {
			m.input += msg.String()
			m.updateSlashMenu()
		}
	}
	return m, nil
}

func (m *Model) executeCommand(cmd SlashCommand) {
	switch cmd.Name {
	case "/new":
		m.backend.NewSession()
		m.messages = nil
		m.refreshSessions()
	case "/delete":
		m.backend.DeleteSession(m.backend.CurrentID())
		m.messages = nil
		m.refreshSessions()
	case "/branch":
		m.backend.BranchSession()
		m.refreshSessions()
	case "/clear":
		m.messages = nil
	case "/sessions":
		m.showSessions = true
		m.refreshSessions()
	case "/set":
		m.showSettings = true
	case "/help":
		help := strings.Join([]string{
			"可用命令：",
			"  /new            新建对话",
			"  /delete         删除当前会话",
			"  /branch         复制当前会话",
			"  /clear          清空对话",
			"  /sessions       打开会话面板",
			"  /set            打开设置面板",
			"  /help           显示此帮助",
			"",
			"快捷键：",
			"  Tab      切换 Build/Plan 模式",
			"  Ctrl+C   停止/退出",
			"  上下键   历史翻动/菜单选择",
			"  Enter    发送/确认选择",
		}, "\n")
		m.messages = append(m.messages, agent.Event{Type: agent.EventText, Content: help})
	}
}

func (m *Model) handleSessions(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
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
	case "q", "esc":
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
		m.input = "/set " + m.settingKeys[m.settingField] + " "
		m.showSettings = false
	}
	return m, nil
}

func (m *Model) sendInput() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input)
	if text == "" || m.busy {
		return m, nil
	}
	m.history = append(m.history, text)
	m.histIndex = -1
	m.input = ""
	m.showSlashMenu = false
	m.slashResults = nil

	if strings.HasPrefix(text, "/set ") {
		parts := strings.SplitN(text, " ", 3)
		if len(parts) >= 3 && parts[2] != "" {
			m.backend.UpdateSettings(func(s *settings.Settings) {
				updateSettingField(s, parts[1], parts[2])
			})
			m.backend.RefreshClients()
			m.messages = append(m.messages, agent.Event{
				Type:    agent.EventText,
				Content: fmt.Sprintf("已更新: %s = %s", parts[1], parts[2]),
			})
		}
		return m, nil
	}

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
	b.WriteString(m.viewHeader())
	b.WriteString("\n")

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

	if m.showSlashMenu && len(m.slashResults) > 0 {
		b.WriteString("\n")
		for i, cmd := range m.slashResults {
			style := baseStyle
			if i == m.slashIndex {
				style = selectedStyle
			}
			b.WriteString(style.Render(fmt.Sprintf(" %-22s %s", cmd.Name, cmd.Description)) + "\n")
		}
		b.WriteString(helpStyle.Render(" ↑↓ 选择 · Enter 确认 · Esc 关闭") + "\n")
	}

	b.WriteString("› ")
	if m.input == "" && !m.showSlashMenu {
		b.WriteString(helpStyle.Render("输入消息，/ 命令菜单，Tab 切换 Build/Plan..."))
	} else {
		b.WriteString(m.input)
	}

	return b.String()
}

func (m *Model) viewHeader() string {
	modeStr := "Build"
	if m.mode == ModePlan {
		modeStr = "Plan"
	}
	return titleStyle.Render(" licode TUI ") + "  " + statusStyle.Render("["+modeStr+"]")
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
	b.WriteString("\n" + helpStyle.Render(" j/k 移动 · enter 切换 · n 新建 · d 删除 · q/esc 返回"))
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
		b.WriteString(style.Render(fmt.Sprintf(" %-18s %s", key+":", val)) + "\n")
	}
	b.WriteString("\n" + helpStyle.Render(" j/k 移动 · enter 编辑(自动填充/set命令) · q/esc 关闭"))
	return b.String()
}

func settingValue(s *settings.Settings, key string) string {
	switch key {
	case "provider":
		return s.Provider
	case "model":
		return s.Model
	case "base_url":
		return s.BaseURL
	case "api_key":
		if s.APIKey != "" {
			return "********"
		}
		return ""
	case "temperature":
		return fmt.Sprintf("%.2f", s.Temperature)
	case "max_tokens":
		return fmt.Sprintf("%d", s.MaxTokens)
	case "max_iterations":
		return fmt.Sprintf("%d", s.MaxIterations)
	}
	return ""
}

func updateSettingField(s *settings.Settings, key, val string) {
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
		fmt.Sscanf(val, "%f", &s.Temperature)
	case "max_tokens":
		fmt.Sscanf(val, "%d", &s.MaxTokens)
	case "max_iterations":
		fmt.Sscanf(val, "%d", &s.MaxIterations)
	}
}

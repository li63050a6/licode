package tui

import (
	"context"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"licode/internal/agent"
	"licode/internal/ai"
)

const Version = "0.0.44"

type lipglossColor = string

type lineKind int

const (
	kindUser lineKind = iota
	kindText
	kindTool
	kindThought
	kindNote
	kindFooter
)

type line struct {
	kind     lineKind
	color    string
	text     string
	expanded bool
	payload  string // 工具输出完整内容（折叠时保留）
	tool     string // 工具名
	args     string // 工具参数(JSON)
	running  bool   // 工具是否运行中
}

type cmdItem struct {
	name     string
	title    string
	category string
	run      func(m *Model)
}

type sessItem struct {
	id    string
	title string
}

type Mode int

const (
	ModeBuild Mode = iota
	ModePlan
)

func (m Mode) String() string {
	if m == ModePlan {
		return "Plan"
	}
	return "Build"
}

func (m Mode) Key() string {
	if m == ModePlan {
		return "PLAN"
	}
	return "BUILD"
}

func (m Mode) Color() string {
	if m == ModePlan {
		return colorPlan
	}
	return colorBuild
}

type Model struct {
	backend *Backend
	w, h    int

	home bool
	mode Mode
	lines []line

	basePath string

	input   string
	hist    []string
	histIdx int

	busy        bool
	cancel      func()
	events      chan agent.Event
	spinnerIdx  int
	toolPending string // 正在运行的工具显示文本（用于 spinner 前缀）

	cmdMenu  bool
	cmdItems []cmdItem
	cmdIdx   int

	listOpen     bool
	listItems    []sessItem
	listSelected int

	settingOpen  bool
	settingField int

	toast    string
	toastExp time.Time

	planExclude string

	ctrlCArmed    bool
	ctrlCArmUntil time.Time

	rows []int // 每次渲染后，把每个屏幕行 → 可切换的工具行下标(-1 无)
}

func NewModel(backend *Backend) *Model {
	m := &Model{
		backend:     backend,
		home:        true,
		basePath:    cwd(),
		events:      make(chan agent.Event, 512),
		cmdItems:    commandList(),
		planExclude: "Write,Edit,Delete,Move,Bash,Shell",
	}
	m.listItems = m.sessionList()
	return m
}

func cwd() string {
	d, err := os.Getwd()
	if err != nil {
		return ""
	}
	return d
}

func (m *Model) showToast(msg string) {
	m.toast = msg
	m.toastExp = time.Now().Add(4 * time.Second)
}

// ── 生命周期 ──
func (m *Model) Init() tea.Cmd {
	return tea.Tick(70*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

type tickMsg struct{}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = v.Width, v.Height
	case tickMsg:
		if m.busy {
			m.spinnerIdx++
			m.drainEvents()
		}
		if !m.toastExp.IsZero() && m.toastExp.Before(time.Now()) {
			m.toast = ""
		}
		return m, tea.Tick(70*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
	case tea.MouseMsg:
		m.mouse(v)
	case agent.Event:
		m.onEvent(v)
	case tea.KeyMsg:
		return m.keyMsg(v)
	}
	return m, nil
}

func (m *Model) drainEvents() {
	for {
		select {
		case e := <-m.events:
			m.onEvent(e)
		default:
			return
		}
	}
}

// ── 事件 → 行 ──
func (m *Model) onEvent(e agent.Event) {
	switch e.Type {
	case agent.EventText:
		if len(m.lines) > 0 && m.lines[len(m.lines)-1].kind == kindText && m.toolPending == "" {
			m.lines[len(m.lines)-1].text += e.Content
			return
		}
		m.lines = append(m.lines, line{kind: kindText, color: m.mode.Color(), text: e.Content})
	case agent.EventToolStart:
		m.toolPending = e.ToolName
		m.lines = append(m.lines, line{kind: kindTool, color: m.mode.Color(), tool: e.ToolName, args: e.ToolArgs, running: true})
	case agent.EventToolDone:
		m.toolPending = ""
		if len(m.lines) > 0 && m.lines[len(m.lines)-1].kind == kindTool {
			idx := len(m.lines) - 1
			out := strings.TrimSpace(e.ToolOut)
			m.lines[idx].running = false
			if out != "" {
				m.lines[idx].payload = out
				m.lines[idx].expanded = false
			}
		}
	case agent.EventError:
		if !m.busy {
			return
		}
		m.busy = false
		m.cancel = nil
		m.toolPending = ""
		m.lines = append(m.lines, line{kind: kindNote, color: colorError, text: "错误: " + e.Error})
	case agent.EventDone:
		if !m.busy {
			return
		}
		m.busy = false
		m.cancel = nil
		m.toolPending = ""
		if len(m.lines) > 0 {
			m.lines = append(m.lines, line{kind: kindFooter, color: m.mode.Color()})
		}
	}
}

// ── 鼠标：点击 “Click to expand” 行切换 ──
func (m *Model) mouse(v tea.MouseMsg) {
	if v.Type != tea.MouseLeft || v.Y < 0 || v.Y >= len(m.rows) {
		return
	}
	idx := m.rows[v.Y]
	if idx >= 0 && idx < len(m.lines) && m.lines[idx].kind == kindTool {
		m.lines[idx].expanded = !m.lines[idx].expanded
	}
}

// ── 按键 ──
func (m *Model) keyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "ctrl+q", "ctrl+d":
		return m.onCtrlC()
	case "tab":
		if !m.busy {
			if m.mode == ModeBuild {
				m.mode = ModePlan
			} else {
				m.mode = ModeBuild
			}
		}
		return m, nil
	case "esc":
		switch {
		case m.cmdMenu:
			m.cmdMenu = false
			m.input = ""
		case m.listOpen:
			m.listOpen = false
		case m.settingOpen:
			m.settingOpen = false
		}
		return m, nil
	case "enter":
		if m.cmdMenu {
			if m.cmdIdx < len(m.cmdItems) {
				it := m.cmdItems[m.cmdIdx]
				m.cmdMenu = false
				m.input = ""
				if it.run != nil {
					it.run(m)
				}
			}
			return m, nil
		}
		if m.listOpen {
			if m.listSelected < len(m.listItems) {
				it := m.listItems[m.listSelected]
				m.backend.SwitchSession(it.id)
				m.clearLines()
				m.replaySession()
				m.listOpen = false
			}
			return m, nil
		}
		if m.settingOpen {
			m.input = "/set " + settingFields[m.settingField] + " "
			m.settingOpen = false
			m.cmdMenu = false
			return m, nil
		}
		m.sendInput()
		return m, nil
	case "backspace":
		if len(m.input) > 0 {
			rs := []rune(m.input)
			m.input = string(rs[:len(rs)-1])
			m.updateCmdMenu()
		}
		return m, nil
	case "up":
		m.up()
		return m, nil
	case "down":
		m.down()
		return m, nil
	case "ctrl+u":
		m.input = ""
		m.cmdMenu = false
		return m, nil
	default:
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
			m.input += string(msg.Runes[0])
			m.updateCmdMenu()
		}
	}
	return m, nil
}

func (m *Model) onCtrlC() (tea.Model, tea.Cmd) {
	now := time.Now()
	if m.ctrlCArmed && now.Before(m.ctrlCArmUntil) {
		return m, tea.Quit
	}
	m.ctrlCArmed = true
	m.ctrlCArmUntil = now.Add(2 * time.Second)
	if m.busy {
		if m.cancel != nil {
			m.cancel()
		}
		m.showToast("已停止（再按 Ctrl+C 退出）")
	} else {
		m.showToast("再按 Ctrl+C 退出")
	}
	return m, nil
}

func (m *Model) up() {
	switch {
	case m.cmdMenu:
		if m.cmdIdx > 0 {
			m.cmdIdx--
		}
	case m.listOpen:
		if m.listSelected > 0 {
			m.listSelected--
		}
	case m.settingOpen:
		if m.settingField > 0 {
			m.settingField--
		}
	default:
		if len(m.hist) == 0 {
			return
		}
		if m.histIdx < 0 {
			m.histIdx = len(m.hist) - 1
		} else if m.histIdx > 0 {
			m.histIdx--
		}
		m.input = m.hist[m.histIdx]
	}
}

func (m *Model) down() {
	switch {
	case m.cmdMenu:
		if m.cmdIdx < len(m.cmdItems)-1 {
			m.cmdIdx++
		}
	case m.listOpen:
		if m.listSelected < len(m.listItems)-1 {
			m.listSelected++
		}
	case m.settingOpen:
		if m.settingField < len(settingFields)-1 {
			m.settingField++
		}
	default:
		if m.histIdx >= 0 {
			m.histIdx++
			if m.histIdx >= len(m.hist) {
				m.histIdx = -1
				m.input = ""
			} else {
				m.input = m.hist[m.histIdx]
			}
		}
	}
}

func (m *Model) updateCmdMenu() {
	trim := strings.TrimSpace(m.input)
	m.cmdMenu = strings.HasPrefix(trim, "/") && !m.busy
	if m.cmdMenu {
		q := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trim, "/")))
		items := make([]cmdItem, 0, len(commandList()))
		for _, c := range commandList() {
			if q == "" || strings.HasPrefix(strings.ToLower(c.name), q) {
				items = append(items, c)
			}
		}
		m.cmdItems = items
		m.cmdIdx = 0
	}
}

// ── 发送 ──
func (m *Model) sendInput() {
	text := strings.TrimSpace(m.input)
	if text == "" || m.busy {
		return
	}
	m.hist = append(m.hist, text)
	m.histIdx = -1
	m.input = ""
	m.cmdMenu = false
	m.home = false

	if strings.HasPrefix(text, "/set ") {
		m.doSetting(text)
		return
	}

	prompt := text
	if m.mode == ModePlan {
		prompt = text + "\n\n[PLAN 只读模式：仅搜索/查看，禁止写入/修改/删除]"
	}

	m.lines = append(m.lines, line{kind: kindUser, color: m.mode.Color(), text: text})

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := m.backend.RunAgent(ctx, prompt)
	if err != nil {
		cancel()
		m.lines = append(m.lines, line{kind: kindNote, color: colorError, text: "错误: " + err.Error()})
		return
	}
	m.cancel = cancel
	m.busy = true
	go func() {
		for e := range ch {
			m.events <- e
		}
		m.events <- agent.Event{Type: agent.EventDone}
	}()
}

func (m *Model) setPlanExclude(val string) {
	m.planExclude = val
}

func (m *Model) replaySession() {
	msgs := m.backend.CurrentSessionMessages()
	for _, msg := range msgs {
		content := strings.TrimSpace(msg.Content)
		if content == "" || strings.HasPrefix(content, "[PLAN 只读") {
			continue
		}
		kind := kindText
		if msg.Role == ai.RoleUser {
			kind = kindUser
		}
		m.appendBody(kind, m.mode.Color(), content)
	}
}

// ── 工具显示 ──
func argsPayload(args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return ""
	}
	if len(args) > 120 {
		return args[:120] + "..."
	}
	return args
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
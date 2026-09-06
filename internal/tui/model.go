package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"licode/internal/version"
)

// 消息类型
type msgType int

const (
	msgUser msgType = iota
	msgAI
	msgThought
	msgCommand
	msgHeading
	msgExpand
)

// 一条消息
type message struct {
	typ     msgType
	content string
}

// Model
type Model struct {
	width  int
	height int

	messages []message

	input     string
	histIndex int

	mode string // "BUILD" / "PLAN"
}

func NewModel() *Model {
	return &Model{
		mode: "BUILD",
		messages: []message{
			{typ: msgHeading, content: "创建 Go CLI 工具"},
			{typ: msgUser, content: "写一个 Go 的 hello world"},
			{typ: msgThought, content: "需要用 main 包和 fmt.Println"},
			{typ: msgCommand, content: "$ go run main.go"},
			{typ: msgAI, content: "已创建 main.go，输出 Hello World"},
			{typ: msgExpand, content: "Click to expand"},
		},
	}
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		switch v.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			text := strings.TrimSpace(m.input)
			if text != "" {
				m.messages = append(m.messages, message{typ: msgUser, content: text})
				m.input = ""
			}
		case "backspace":
			if len(m.input) > 0 {
				m.input = m.input[:len(m.input)-1]
			}
		case "up":
			// noop
		case "down":
			// noop
		case "tab":
			if m.mode == "BUILD" {
				m.mode = "PLAN"
			} else {
				m.mode = "BUILD"
			}
		default:
			if v.Type == tea.KeyRunes && len(v.String()) == 1 {
				m.input += v.String()
			}
		}
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
	}
	return m, nil
}

func (m *Model) View() string {
	var b strings.Builder

	// 1. 对话内容区（占满上方，纯文本，无边框）
	contentH := m.height - 2 // 底部留 2 行：输入框 + 状态栏
	if contentH < 3 {
		contentH = 3
	}
	contentLines := strings.Split(m.renderContent(), "\n")
	if len(contentLines) > contentH {
		contentLines = contentLines[len(contentLines)-contentH:]
	}
	for _, l := range contentLines {
		b.WriteString(l + "\n")
	}
	// 填充空白
	for i := len(contentLines); i < contentH; i++ {
		b.WriteString("\n")
	}

	// 2. 输入框（无前缀）
	b.WriteString(m.input)
	b.WriteString("\n")

	// 3. 状态栏
	b.WriteString(m.viewStatusBar())

	return b.String()
}

func (m *Model) renderContent() string {
	var msgs []string
	for _, msg := range m.messages {
		switch msg.typ {
		case msgHeading:
			msgs = append(msgs, "# "+msg.content)
		case msgUser:
			msgs = append(msgs, msg.content)
		case msgThought:
			msgs = append(msgs, "+ Thought: "+msg.content)
		case msgCommand:
			msgs = append(msgs, msg.content)
		case msgAI:
			msgs = append(msgs, msg.content)
		case msgExpand:
			msgs = append(msgs, msg.content)
		}
	}
	return strings.Join(msgs, "\n")
}

func (m *Model) viewStatusBar() string {
	dir := "/home/admin/licode"
	size := "1.2 MB"
	tip := "Ctrl+p commands"
	ver := version.Current()
	if ver == "" {
		ver = "0.0.36"
	}
	// 两个空格分隔
	return dir + "  " + size + "  " + tip + "  " + "LiCode " + ver
}

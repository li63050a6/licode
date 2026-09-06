package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type row struct {
	indent      string
	color       string
	text        string
	bg          string // 背景填充色（空 = 终端底色）
	toggle      int    // 可点击切换的工具行下标(-1)
	borderColor string // 左侧竖线颜色（用户消息）
}

func (m *Model) View() string {
	switch {
	case m.listOpen:
		return m.viewList()
	case m.settingOpen:
		return m.viewSettings()
	default:
		return m.viewChat()
	}
}

func (m *Model) viewChat() string {
	var sb strings.Builder

	chatH := m.h - 4
	if chatH < 2 {
		chatH = 2
	}

	rows := m.buildRows()
	m.rows = make([]int, len(rows))
	for i := range rows {
		m.rows[i] = rows[i].toggle
	}
	if len(rows) > chatH {
		rows = rows[len(rows)-chatH:]
		m.rows = m.rows[len(m.rows)-chatH:]
	}

	for _, r := range rows {
		sb.WriteString(m.renderRow(r))
		sb.WriteString("\n")
	}
	for i := len(rows); i < chatH; i++ {
		sb.WriteString(strings.Repeat(" ", max(0, m.w)) + "\n")
	}

	sb.WriteString(m.renderInputLine())
	sb.WriteString("\n")
	sb.WriteString(m.renderMetaLine())
	sb.WriteString("\n")
	sb.WriteString(m.renderSeparator())
	sb.WriteString("\n")
	sb.WriteString(m.renderStatusLine())
	return sb.String()
}

func (m *Model) buildRows() []row {
	var rows []row
	maxW := m.chunkMax()
	for i := range m.lines {
		l := &m.lines[i]
		switch l.kind {
		case kindUser:
			rows = append(rows, m.userRows(l, maxW)...)
		case kindText:
			rows = append(rows, wrapRows(l.indent(), l.color, l.text, maxW)...)
		case kindTool:
			rows = append(rows, m.toolRows(i, l, maxW)...)
		case kindThought:
			rows = append(rows, row{indent: "   ", color: colorWarning, text: "+ Thought: " + l.text, toggle: -1})
		case kindNote:
			rows = append(rows, wrapRows("   ", l.color, l.text, maxW)...)
		}
	}
	if !m.toastExp.IsZero() && m.toast != "" {
		rows = append(rows, row{indent: "  ", color: colorMuted, text: m.toast, toggle: -1})
	}
	if m.cmdMenu {
		rows = append(rows, m.menuRows()...)
	}
	return rows
}

func (m *Model) chunkMax() int {
	w := m.w - 12
	if w < 40 {
		w = 40
	}
	return w
}

func (l *line) indent() string {
	switch l.kind {
	case kindText:
		return "     "
	case kindUser:
		return "  ▍  "
	default:
		return "     "
	}
}

func (m *Model) userRows(l *line, maxW int) []row {
	rows := wrapRows("", "", l.text, maxW)
	out := make([]row, len(rows))
	for i, r := range rows {
		out[i] = row{indent: "  ", color: colorText, text: r.text, bg: colorPanel, toggle: -1, borderColor: l.color}
	}
	return out
}

func (m *Model) toolRows(idx int, l *line, maxW int) []row {
	// 运行中：spinner 前缀；完成：图标颜色转弱
	if l.payload == "" {
		frame := spinnerFrames[(m.spinnerIdx+idx)%len(spinnerFrames)]
		return []row{{indent: "   ", color: l.color, text: frame + " " + l.text, toggle: -1}}
	}

	// 完成且带输出：折叠/展开
	preview := strings.Split(l.payload, "\n")
	expanded := l.expanded
	if !expanded {
		maxN := 5
		if len(preview) > maxN {
			preview = preview[:maxN]
		}
	}
	rows := []row{}
	if expanded || len(preview) == 1 {
		rows = append(rows, row{indent: "   ", color: colorMuted, text: l.text, toggle: -1})
	} else {
		rows = append(rows, row{indent: "   ", color: colorMuted, text: "✓ " + l.text, toggle: -1})
	}
	for _, p := range preview {
		for _, sub := range wrapRows("      ", colorText, p, maxW) {
			rows = append(rows, sub)
		}
	}
	if len(strings.Split(l.payload, "\n")) > 5 || len(l.payload) > 500 {
		if expanded {
			rows = append(rows, row{indent: "      ", color: colorMuted, text: "Click to collapse", toggle: idx})
		} else {
			rows = append(rows, row{indent: "      ", color: colorMuted, text: "Click to expand", toggle: idx})
		}
	}
	return rows
}

func (m *Model) menuRows() []row {
	var rows []row
	for i, c := range m.cmdItems {
		if i > 6 {
			break
		}
		colors := colorMuted
		if i == m.cmdIdx {
			colors = colorAccent
		}
		rows = append(rows, row{indent: "  ", color: colors, text: "/" + c.name + "   " + c.title, toggle: -1, bg: bgForSelected(i, m.cmdIdx)})
	}
	return rows
}

func (m *Model) renderRow(r row) string {
	indent := r.indent
	border := ""
	padBase := indent
	if r.borderColor != "" {
		border = lipgloss.NewStyle().Foreground(lipgloss.Color(r.borderColor)).Render(borderChar)
		padBase += border + "  "
	}
	text := r.text
	colored := lipgloss.NewStyle().Foreground(lipgloss.Color(r.color)).Render(text)
	pad := m.w - lipgloss.Width(padBase+text)
	if r.bg != "" {
		bgFill := ""
		if pad > 0 {
			bgFill = lipgloss.NewStyle().Background(lipgloss.Color(r.bg)).Render(strings.Repeat(" ", pad))
		}
		return padBase + colored + bgFill
	}
	if pad > 0 {
		return padBase + colored + strings.Repeat(" ", pad)
	}
	return padBase + colored
}

func wrapRows(indent, color, text string, maxW int) []row {
	lines := strings.Split(text, "\n")
	var rows []row
	for _, ln := range lines {
		if ln == "" {
			continue
		}
		for _, chunk := range chunkLines(ln, maxW) {
			rows = append(rows, row{indent: indent, color: color, text: chunk, toggle: -1})
		}
	}
	return rows
}

func chunkLines(line string, max int) []string {
	if lipgloss.Width(line) <= max {
		return []string{line}
	}
	var chunks []string
	runes := []rune(line)
	for len(runes) > 0 {
		var cur []rune
		w := 0
		for len(runes) > 0 {
			rw := runeWidth(runes[0])
			if w+rw > max {
				break
			}
			w += rw
			cur = append(cur, runes[0])
			runes = runes[1:]
		}
		if len(cur) == 0 {
			cur = append(cur, runes[0])
			runes = runes[1:]
		}
		chunks = append(chunks, string(cur))
	}
	return chunks
}

func runeWidth(r rune) int {
	if r > 0x2E7F {
		return 2
	}
	return 1
}

func bgForSelected(i, sel int) string {
	if i == sel {
		return colorElement
	}
	return ""
}

// ── 输入 / 底部 ──
func (m *Model) renderInputLine() string {
	border := lipgloss.NewStyle().Foreground(lipgloss.Color(m.mode.Color())).Render(borderChar)
	prefix := "  " + border + " "
	rest := m.w - lipgloss.Width(prefix)
	if rest < 1 {
		rest = 1
	}
	input := m.input
	rs := []rune(input)
	show := rs
	if len(show) > rest-1 {
		show = show[len(show)-(rest-1):]
	}
	text := string(show)

	if m.busy {
		text += spinnerFrames[(m.spinnerIdx)%len(spinnerFrames)]
	} else if m.ctrlCArmed {
		text += "▊"
	} else {
		text += "▊"
	}

	bg := colorElement
	if m.busy {
		bg = colorPanel
	}
	fg := colorText
	if m.busy {
		fg = colorMuted
	}
	fill := lipgloss.NewStyle().Background(lipgloss.Color(bg)).Render(lipgloss.NewStyle().Foreground(lipgloss.Color(fg)).Render(text) + strings.Repeat(" ", max(0, rest-lipgloss.Width(text))))
	return prefix + fill
}

func (m *Model) renderMetaLine() string {
	agent := lipgloss.NewStyle().Foreground(lipgloss.Color(m.mode.Color())).Render(m.mode.String())
	sep := lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render(" · ")
	model := lipgloss.NewStyle().Foreground(lipgloss.Color(colorText)).Render(m.modelName())
	right := ""
	left := "   " + agent + sep + model
	if m.busy {
		right = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render("esc 中断")
	}
	pad := m.w - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right
}

func (m *Model) renderSeparator() string {
	prefix := "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(m.mode.Color())).Render("╹")
	fill := lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render(strings.Repeat("▀", max(0, m.w-3)))
	return prefix + fill
}

func (m *Model) renderStatusLine() string {
	cwd := m.basePath
	if cwd == "" {
		cwd = "~"
	}
	if len([]rune(cwd)) > 40 {
		rs := []rune(cwd)
		cwd = "…" + string(rs[len(rs)-38:])
	}
	size := m.usage()
	if size == "" {
		size = "0 KB"
	}

	leftParts := []string{}
	if m.busy {
		leftParts = append(leftParts, spinnerFrames[(m.spinnerIdx)%len(spinnerFrames)])
	}
	leftParts = append(leftParts, cwd, size)

	left := " " + strings.Join(leftParts, "  ")
	right := lipgloss.NewStyle().Foreground(lipgloss.Color(colorText)).Render("Ctrl+p") +
		lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render(" commands") +
		"  " +
		lipgloss.NewStyle().Foreground(lipgloss.Color(colorText)).Render("LiCode") +
		" " +
		lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render(Version)

	pad := m.w - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right
}

// ── 会话列表 / 设置 ──
func (m *Model) viewList() string {
	var sb strings.Builder
	sb.WriteString("  会话列表\n\n")
	for i, it := range m.listItems {
		line := "  " + it.title
		if i == m.listSelected {
			line = lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)).Background(lipgloss.Color(colorElement)).Render("▍ " + it.title)
		} else {
			line = "  " + it.title
		}
		sb.WriteString(line + "\n")
	}
	sb.WriteString("\n  ↑/↓ 选择 · Enter 打开 · d 删除 · n 新建 · Esc 返回\n")
	return sb.String()
}

func (m *Model) viewSettings() string {
	var sb strings.Builder
	sb.WriteString("  设置\n\n")
	for i, key := range settingFields {
		label := key
		if key == "plan_exclude" {
			label = "PLAN禁用工具"
		}
		val := m.settingValue(key)
		if i == m.settingField {
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)).Background(lipgloss.Color(colorElement)).Render("▍ "+label+": "+val) + "\n")
			hint := m.settingHint(key)
			if hint != "" {
				sb.WriteString("     " + lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render(hint) + "\n")
			}
		} else {
			sb.WriteString("  " + label + ": " + val + "\n")
		}
	}
	sb.WriteString("\n  Enter 编辑（自动填充 /set 命令）· Esc 返回\n")
	return sb.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
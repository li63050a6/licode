package tui

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// 忠实复刻 opencode TUI 的会话视图。
// 屏幕列：外层 paddingLeft=2；消息边框在第 2 列；正文/图标在第 5 列。
const (
	colBody     = "     " // 助手正文 / 工具图标
	colBlockTxt = "     " // 块工具正文
	colBlockTt  = "        " // 块工具标题
)

type seg struct {
	text  string
	color string
}

type row struct {
	indent      string
	color       string
	text        string
	bg          string // 背景色（空 = colorBg）
	toggle      int    // 可点击切换的工具行下标(-1)
	borderColor string // 左侧竖线颜色（仅有两端框起时才画）
	top         int    // 上方空行数
	segs        []seg  // 多段着色（▣ 页脚等）
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
	w := m.w
	if w < 10 {
		w = 10
	}

	chatH := m.h - 4
	if chatH < 2 {
		chatH = 2
	}

	// 顶部留白 + 浮动 toast
	if !m.toastExp.IsZero() && m.toast != "" {
		sb.WriteString(m.toastLine())
	} else {
		sb.WriteString(blankLine(w, colorBg))
	}

	rows := m.buildRows()
	m.rows = nil
	for i := range rows {
		m.rows = append(m.rows, rows[i].toggle)
	}
	remain := chatH - 1
	if len(rows) > remain {
		rows = rows[len(rows)-remain:]
		m.rows = m.rows[len(m.rows)-remain:]
	}
	for _, r := range rows {
		sb.WriteString(m.renderRow(r))
		sb.WriteString("\n")
	}
	for i := len(rows); i < remain; i++ {
		sb.WriteString(blankLine(w, colorBg))
	}

	// Prompt 区块：输入框(+元信息行) / 分隔线 / 状态行
	sb.WriteString(m.renderInputLine())
	sb.WriteString("\n")
	sb.WriteString(m.renderMetaLine())
	sb.WriteString("\n")
	sb.WriteString(m.renderSeparator())
	sb.WriteString("\n")
	sb.WriteString(m.renderStatusLine())
	return sb.String()
}

func blankLine(w int, bg string) string {
	return lipgloss.NewStyle().Background(lipgloss.Color(bg)).Render(strings.Repeat(" ", max(0, w)))
}

// ── 消息区 ──

func (m *Model) buildRows() []row {
	var rows []row
	maxW := m.chunkMax()
	first := true // 首条用户消息不另加 margin（顶部留白已占一行）
	lastInline := false

	for i := range m.lines {
		l := &m.lines[i]
		switch l.kind {
		case kindUser:
			rs := m.userRows(l, first, maxW)
			rows = append(rows, rs...)
			first = false
			lastInline = false
		case kindText:
			rs := wrapRows(colBody, colorText, l.text, maxW)
			rs[0].top = 1
			rows = append(rows, rs...)
			lastInline = false
		case kindThought:
			rows = append(rows, row{indent: colBody, color: colorWarning, text: "+ Thought: " + l.text, top: 1})
			lastInline = false
		case kindTool:
			rs := m.toolRows(i, l, lastInline, maxW)
			rows = append(rows, rs...)
			lastInline = len(rs) == 1 && rs[0].toggle < 0
		case kindFooter:
			f := m.mode.Color()
			rows = append(rows, row{
				indent: colBody,
				top:    1,
				segs: []seg{
					{"▣ ", f},
					{m.mode.String(), colorText},
					{" · " + m.modelName(), colorMuted},
				},
			})
			lastInline = false
		case kindNote:
			rs := wrapRows(colBody, l.color, l.text, maxW)
			rs[0].top = 1
			rows = append(rows, rs...)
			lastInline = false
		}
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

func (m *Model) userRows(l *line, first bool, maxW int) []row {
	rs := wrapRows("", colorText, l.text, maxW)
	out := make([]row, 0, len(rs))
	for i, r := range rs {
		top := 0
		if !first && i == 0 {
			top = 1
		}
		out = append(out, row{indent: "  ", color: colorText, text: r.text, bg: colorPanel, toggle: -1, borderColor: l.color, top: top})
	}
	return out
}

func (m *Model) toolRows(idx int, l *line, lastInline bool, maxW int) []row {
	icon, desc, blockTitle, maxLines := describeTool(l.tool, l.args)

	// 运行中：spinner + 描述（无图标，与 opencode 一致）
	if l.running {
		frame := spinnerFrames[(m.spinnerIdx+idx)%len(spinnerFrames)]
		top := 0
		if !lastInline {
			top = 1
		}
		return []row{{indent: colBody, color: colorText, text: frame + " " + desc, bg: colorBg, toggle: -1, top: top}}
	}

	payload := strings.TrimSpace(l.payload)

	// 块工具（输出可见）：左框+面板底色，标题行 + 输出 + 折叠/展开
	if payload != "" {
		var rs []row
		top := 1
		if blockTitle != "" {
			rs = append(rs, row{indent: colBlockTt, color: colorMuted, text: blockTitle, bg: colorPanel, toggle: -1, top: top, borderColor: colorBg})
			top = 0
		} else if icon == "$" {
			rs = append(rs, row{indent: colBlockTxt, color: colorText, text: icon + " " + desc, bg: colorPanel, toggle: -1, top: top, borderColor: colorBg})
			top = 0
		}
		lines := strings.Split(payload, "\n")
		overflow := len(lines) > maxLines || len(payload) > maxLines*80
		if !l.expanded && overflow {
			lines = lines[:maxLines]
		}
		for _, ln := range lines {
			for _, wr := range wrapRows(colBlockTxt, colorText, ln, maxW) {
				rs = append(rs, row{indent: colBlockTxt, color: colorText, text: wr.text, bg: colorPanel, toggle: -1, borderColor: colorBg, top: top})
				top = 0
			}
		}
		if overflow {
			txt := "Click to expand"
			if l.expanded {
				txt = "Click to collapse"
			}
			rs = append(rs, row{indent: colBlockTxt, color: colorMuted, text: txt, bg: colorPanel, toggle: idx, borderColor: colorBg})
		}
		return rs
	}

	// 内联工具行：完成 → 图标+描述（muted）；task/execute 完成 ✓
	ic := icon
	if l.tool == "task" || l.tool == "execute" {
		ic = "✓"
	}
	top := 0
	if !lastInline {
		top = 1
	}
	return []row{{indent: colBody, color: colorMuted, text: ic + " " + desc, bg: colorBg, toggle: -1, top: top}}
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
		rows = append(rows, row{indent: colBody, color: colors, text: "/" + c.name + "   " + c.title, toggle: -1, bg: bgForSelected(i, m.cmdIdx), top: 1})
	}
	return rows
}

// describeTool 返回 (图标, 描述, 块标题, 折叠行数)。
func describeTool(tool, args string) (icon, desc, blockTitle string, maxLines int) {
	lower := strings.ToLower(tool)
	a := parseArgs(args)
	switch lower {
	case "bash", "shell":
		icon, maxLines = "$", 10
		desc = str(a, "command")
		if desc == "" {
			desc = argsPayload(args)
		}
		if desc == "" {
			desc = "(no command)"
		}
	case "read":
		icon = "→"
		desc = "Read " + short(a, "filePath", "path")
		if v := str(a, "offset"); v != "" {
			desc += " [offset=" + v
			if l := str(a, "limit"); l != "" {
				desc += ", limit=" + l
			}
			desc += "]"
		}
	case "glob":
		icon = "✱"
		desc = `Glob "` + short(a, "pattern", "include") + `"`
		if p := short(a, "path"); p != "" {
			desc += " in " + p
		}
		maxLines = 5
	case "grep":
		icon = "✱"
		desc = `Grep "` + short(a, "pattern", "query") + `"`
		if p := short(a, "path"); p != "" {
			desc += " in " + p
		}
		maxLines = 5
	case "webfetch":
		icon = "%"
		desc = "WebFetch " + short(a, "url")
	case "websearch":
		icon = "◈"
		desc = `Web Search "` + short(a, "query") + `"`
		if n := str(a, "numResults"); n != "" {
			desc += " (" + n + " results)"
		}
	case "write":
		icon = "←"
		p := short(a, "filePath", "path")
		desc = "Write " + p
		blockTitle = "# Wrote " + p
		maxLines = 20
	case "edit":
		icon = "←"
		p := short(a, "filePath", "path")
		desc = "Edit " + p
		if v := str(a, "replaceAll"); v == "true" {
			desc += " [replaceAll=true]"
		}
		blockTitle = "← Edit " + p
		maxLines = 20
	case "apply_patch":
		icon = "%"
		desc = "Patch"
		p := short(a, "filePath", "path")
		if p != "" {
			blockTitle = "# Patched " + p
		} else {
			blockTitle = "# Patch"
		}
		maxLines = 20
	case "todowrite":
		icon = "⚙"
		desc = "Updating todos…"
		blockTitle = "# Todos"
		maxLines = 20
	case "question":
		icon = "→"
		maxLines = 20
		if c := str(a, "questions"); c != "" {
			desc = countDesc(c)
		} else {
			desc = "Asking questions…"
		}
		blockTitle = "# Questions"
	case "task":
		icon = "│"
		desc = taskDesc(a, args)
		maxLines = 20
	case "execute":
		icon = "│"
		desc = "execute"
	case "skill":
		icon = "→"
		desc = `Skill "` + short(a, "name") + `"`
	default:
		icon = "⚙"
		desc = tool
		if p := argsPayload(args); p != "" {
			desc += " " + p
		}
		maxLines = 3
	}
	if maxLines <= 0 {
		maxLines = 3
	}
	return icon, desc, blockTitle, maxLines
}

func taskDesc(a map[string]any, args string) string {
	if s := str(a, "description"); s != "" {
		if t := str(a, "subagent_type"); t != "" {
			return titlecase(t) + " Task — " + s
		}
		return "Task — " + s
	}
	return argsPayload(args)
}

func titlecase(s string) string {
	if s == "" {
		return s
	}
	rs := []rune(s)
	return strings.ToUpper(string(rs[0])) + string(rs[1:])
}

func countDesc(v string) string {
	n := 0
	for _, c := range v {
		if c == '{' {
			n++
		}
	}
	s := strconv.Itoa(n)
	if n == 1 {
		return "Asked " + s + " question"
	}
	return "Asked " + s + " questions"
}

func short(a map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := a[k]; ok {
			return truncate(fmtVal(v), 60)
		}
	}
	return ""
}

func str(a map[string]any, key string) string {
	if v, ok := a[key]; ok {
		return fmtVal(v)
	}
	return ""
}

func fmtVal(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == float64(int(t)) {
			return strconv.Itoa(int(t))
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "true"
		}
		return "false"
	case nil:
		return ""
	}
	return ""
}

func parseArgs(args string) map[string]any {
	args = strings.TrimSpace(args)
	if args == "" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(args), &m); err != nil {
		return map[string]any{}
	}
	return m
}

// ── 行渲染 ──

func (m *Model) renderRow(r row) string {
	w := m.w
	prefix := r.indent
	if r.borderColor != "" {
		prefix += lipgloss.NewStyle().Foreground(lipgloss.Color(r.borderColor)).Render(borderChar) + "  "
	}
	colored := lipgloss.NewStyle().Foreground(lipgloss.Color(r.color)).Render(r.text)
	if len(r.segs) > 0 {
		var b strings.Builder
		for _, s := range r.segs {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(s.color)).Render(s.text))
		}
		colored = b.String()
	}
	bg := r.bg
	if bg == "" {
		bg = colorBg
	}
	pad := w - lipgloss.Width(prefix+r.text)
	if pad > 0 {
		fill := lipgloss.NewStyle().Background(lipgloss.Color(bg)).Render(strings.Repeat(" ", pad))
		return prefix + colored + fill
	}
	return prefix + colored
}

func wrapRows(indent, color, text string, maxW int) []row {
	lines := strings.Split(text, "\n")
	var rows []row
	for _, ln := range lines {
		if ln == "" {
			continue
		}
		for _, chunk := range chunkLines(ln, maxW) {
			if len(rows) == 0 {
				rows = append(rows, row{indent: indent, color: color, text: chunk, toggle: -1})
			} else {
				rows = append(rows, row{indent: indent, color: color, text: chunk, toggle: -1})
			}
		}
	}
	if len(rows) == 0 {
		rows = append(rows, row{indent: indent, color: color, text: "", toggle: -1})
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
		wid := 0
		for len(runes) > 0 {
			rw := runeWidth(runes[0])
			if wid+rw > max {
				break
			}
			wid += rw
			cur = append(cur, runes[0])
			runes = runes[1:]
		}
		if wid == 0 {
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

// ── 底部 Prompt 区块 ──

// 输入框 + 元信息行（同框，左框 ▍，背景 element）
func (m *Model) renderInputLine() string {
	bor := lipgloss.NewStyle().Foreground(lipgloss.Color(m.mode.Color())).Render(borderChar)
	prefix := "  " + bor + "  "
	avail := m.w - lipgloss.Width(prefix) - 2
	if avail < 1 {
		avail = 1
	}
	text := m.input
	rs := []rune(text)
	if lipgloss.Width(text) > avail-1 {
		for lipgloss.Width(string(rs)) > avail-1 {
			rs = rs[1:]
		}
	}
	line := string(rs) + "▊"
	fill := lipgloss.NewStyle().Background(lipgloss.Color(colorElement)).
		Foreground(lipgloss.Color(colorText)).
		Render(line + strings.Repeat(" ", max(0, avail-lipgloss.Width(line))))
	return prefix + fill + "  "
}

// 元信息行：agent · model provider（与输入同框背景）
func (m *Model) renderMetaLine() string {
	bor := lipgloss.NewStyle().Foreground(lipgloss.Color(m.mode.Color())).Render(borderChar)
	prefix := "  " + bor + "  "
	agent := lipgloss.NewStyle().Foreground(lipgloss.Color(m.mode.Color())).Render(m.mode.String())
	sep := lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render(" · ")
	model := lipgloss.NewStyle().Foreground(lipgloss.Color(colorText)).Render(m.modelName())
	text := agent + sep + model
	avail := m.w - lipgloss.Width(prefix) - 2
	fill := lipgloss.NewStyle().Background(lipgloss.Color(colorElement)).Render(text + strings.Repeat(" ", max(1, avail-lipgloss.Width(text))))
	return prefix + fill + "  "
}

// 分隔线：╹ + ▀ 填充（backgroundElement 色）
func (m *Model) renderSeparator() string {
	prefix := "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(m.mode.Color())).Render("╹")
	fill := lipgloss.NewStyle().Foreground(lipgloss.Color(colorElement)).Render(strings.Repeat("▀", max(0, m.w-3)))
	return prefix + fill
}

// 状态行：左 cwd / 忙碌时 spinner+esc interrupt；右 快捷键 + 版本
func (m *Model) renderStatusLine() string {
	var left string
	if m.busy {
		frame := statusFrames[(m.spinnerIdx)%len(statusFrames)]
		left = "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(colorText)).Render(frame) + " " +
			lipgloss.NewStyle().Foreground(lipgloss.Color(colorText)).Render("esc") +
			lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render(" interrupt")
	} else {
		cwd := m.basePath
		if cwd == "" {
			cwd = "~"
		}
		left = "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render(cwd)
	}

	right := lipgloss.NewStyle().Foreground(lipgloss.Color(colorText)).Render("a") +
		lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render(" agents") +
		"  " +
		lipgloss.NewStyle().Foreground(lipgloss.Color(colorText)).Render("Ctrl+p") +
		lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render(" commands") +
		"  " +
		lipgloss.NewStyle().Foreground(lipgloss.Color(colorText)).Render("LiCode") +
		" " +
		lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render(Version)

	pad := m.w - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 1 {
		pad = 1
	}
	return lipgloss.NewStyle().Background(lipgloss.Color(colorBg)).Render(left + strings.Repeat(" ", pad) + right)
}

var statusFrames = []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█", "▇", "▆", "▅", "▄", "▃", "▂"}

// 顶部 toast（右对齐浮动）
func (m *Model) toastLine() string {
	w := m.w
	msg := m.toast
	lim := min(60, w-6) - 4
	if lim < 1 {
		lim = 1
	}
	if lipgloss.Width(msg) > lim {
		msg = truncate(msg, lim)
	}
	bw := lipgloss.Width(msg) + 6
	bor := lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)).Render(borderChar)
	inner := " " + lipgloss.NewStyle().Background(lipgloss.Color(colorPanel)).Foreground(lipgloss.Color(colorText)).Render(" "+msg+" ") + " "
	pad := max(0, w-bw)
	return strings.Repeat(" ", pad) + bor + inner + bor
}

// ── 会话列表 / 设置 ──
func (m *Model) viewList() string {
	var sb strings.Builder
	sb.WriteString(blankLine(m.w, colorBg) + "\n")
	sb.WriteString("  会话列表\n\n")
	for i, it := range m.listItems {
		if i == m.listSelected {
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)).Background(lipgloss.Color(colorElement)).Render("▍ "+it.title) + "\n")
		} else {
			sb.WriteString("  " + it.title + "\n")
		}
	}
	sb.WriteString("\n  ↑/↓ 选择 · Enter 打开 · Esc 返回\n")
	return sb.String()
}

func (m *Model) viewSettings() string {
	var sb strings.Builder
	sb.WriteString(blankLine(m.w, colorBg) + "\n")
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
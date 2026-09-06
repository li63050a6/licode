package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// opencode 深色主题近似色（16 进制，ANSI truecolor）
const (
	colorAccent  = "#79d0ff"
	colorWarning = "#eab55f"
	colorError   = "#f16c6c"
	colorSuccess = "#7fd88f"
	colorText    = "#e7e7e7"
	colorMuted   = "#8a8a8a"
	colorBorder  = "#3d3d3d"
	colorBg      = "#1b1b1b"
	colorPanel   = "#212121"
	colorElement = "#2a2a2a"

	colorBuild = "#6fa8ff"
	colorPlan  = "#e8a33d"

	borderChar = "▍"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// 供渲染使用的样式（按需构造，避免全局可变状态）
var (
	textStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(colorText))
	mutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(colorWarning))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(colorError))
	accentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent))
	panelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(colorText)).Background(lipgloss.Color(colorPanel))
	elementStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorText)).Background(lipgloss.Color(colorElement))
)

// shadowOf 复刻 opencode logo 阴影：把前景色向背景色混合 25%（tint(bg, fg, 0.25)）
func shadowOf(fg string) string {
	return hexBlend(colorBg, fg, 0.25)
}

func hexBlend(base, mix string, p float64) string {
	rb, gb, bb := parseHex(base)
	rm, gm, bm := parseHex(mix)
	return hex8(
		rb+int(float64(rm-rb)*p),
		gb+int(float64(gm-gb)*p),
		bb+int(float64(bm-bb)*p),
	)
}

func parseHex(s string) (int, int, int) {
	s = strings.TrimPrefix(s, "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) < 6 {
		return 0, 0, 0
	}
	v, err := strconv.ParseInt(s[:6], 16, 32)
	if err != nil {
		return 0, 0, 0
	}
	return int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff)
}

func hex8(r, g, b int) string {
	clamp := func(v int) int {
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return v
	}
	return fmt.Sprintf("#%02x%02x%02x", clamp(r), clamp(g), clamp(b))
}
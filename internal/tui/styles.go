package tui

import "github.com/charmbracelet/lipgloss"

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
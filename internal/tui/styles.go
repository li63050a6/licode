package tui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	baseStyle = lipgloss.NewStyle().Padding(0, 1)

	titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FAFAFA")).
		Background(lipgloss.Color("#7D56F4")).
		Padding(0, 2)

	userMsgStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#00D1FF")).
		Bold(true)

	aiMsgStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF79C6"))

	toolStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F1FA8C")).
		Italic(true)

	statusStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6272A4")).
		Italic(true)

	errorStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF5555")).
		Bold(true)

	selectedStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("#44475A")).
		Foreground(lipgloss.Color("#F8F8F2"))

	helpStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6272A4")).
		Italic(true)
)

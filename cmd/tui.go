package cmd

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"licode/internal/tui"
)

// NewTUICommand 返回 TUI 命令（默认命令）。
func NewTUICommand() *cobra.Command {
	return &cobra.Command{
		Use:   "licode",
		Short: "AI 编程助手（TUI 终端界面）",
		Long: `licode —— 启动终端 TUI 界面。

启动后即可在终端中与 licode 对话。
启动 Web 服务器：./licode web`,
		RunE: func(cmd *cobra.Command, args []string) error {
			backend, err := tui.NewBackend()
			if err != nil {
				return err
			}
			model := tui.NewModel(backend)
			p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseAllMotion())
			if _, err := p.Run(); err != nil {
				log.Printf("TUI 退出: %v", err)
				return err
			}
			return nil
		},
	}
}

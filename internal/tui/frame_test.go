package tui

import (
	"strings"
	"testing"
)

func mkModel(atHome bool, w, h int) *Model {
	m := &Model{
		w:          w,
		h:          h,
		home:       atHome,
		spinnerIdx: 3,
		mode:       ModeBuild,
	}
	m.basePath = "/home/admin1/work/licode"
	return m
}

func TestFrameHome(t *testing.T) {
	m := mkModel(true, 120, 40)
	for i, ln := range strings.Split(m.View(), "\n") {
		t.Logf("%02d w=%d %q", i, displayWidth(ln), ln)
	}
}

func TestFrameSession(t *testing.T) {
	m := mkModel(false, 120, 40)
	m.lines = []line{
		{kind: kindUser, color: "#6fa8ff", text: "hello world"},
		{kind: kindText, color: "#6fa8ff", text: "Hi there, I am the assistant. This is a fairly long assistant reply to check wrapping behaviour across the terminal width."},
		{kind: kindTool, color: "#6fa8ff", tool: "bash", args: `{"command":"cat main.go","subagent_type":"bash"}`},
		{kind: kindTool, color: "#6fa8ff", tool: "bash", args: `{"command":"cat main.go"}`, payload: "line1\nline2\nline3\n", expanded: false},
		{kind: kindFooter, color: "#6fa8ff"},
	}
	m.showToast("新会话已创建")
	for i, ln := range strings.Split(m.View(), "\n") {
		t.Logf("%02d w=%d %q", i, displayWidth(ln), ln)
	}
}

func displayWidth(s string) int {
	n := 0
	strip := s
	// strip ANSI: minimal
	n = len([]rune(strip))
	return n
}

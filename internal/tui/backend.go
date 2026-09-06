package tui

import (
	"context"
	"fmt"
	"log"
	"sync"

	"licode/internal/agent"
	"licode/internal/ai"
	"licode/internal/session"
	"licode/internal/settings"
)

type Backend struct {
	mu       sync.Mutex
	settings *settings.Settings
	sessions *session.Manager
	client   ai.LLMClient
	running  bool
	cancel   context.CancelFunc
}

func NewBackend() (*Backend, error) {
	s, err := settings.Load()
	if err != nil {
		return nil, err
	}
	s.EnsureDefaults()
	client, err := s.NewClient()
	if err != nil {
		log.Printf("LLM 客户端初始化失败: %v", err)
		client = nil
	}
	mgr := session.NewManager(settings.SessionsDir(), true)
	return &Backend{settings: &s, sessions: mgr, client: client}, nil
}

func (b *Backend) Settings() *settings.Settings {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.settings
}

func (b *Backend) Sessions() []session.Info {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessions.List()
}

func (b *Backend) CurrentID() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessions.CurrentID()
}

func (b *Backend) CurrentSessionMessages() []ai.Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessions.Current().Messages()
}

func (b *Backend) SessionUsage() ai.Usage {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessions.Current().Usage()
}

func (b *Backend) SwitchSession(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sessions.SetCurrent(id)
	_ = b.sessions.SaveAll()
}

func (b *Backend) NewSession() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sessions.New()
	_ = b.sessions.SaveAll()
}

func (b *Backend) DeleteSession(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sessions.Delete(id)
	_ = b.sessions.SaveAll()
}

func (b *Backend) BranchSession() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sessions.Branch(b.sessions.CurrentID(), -1)
	_ = b.sessions.SaveAll()
}

func (b *Backend) RunAgent(ctx context.Context, input string) (<-chan agent.Event, error) {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return nil, fmt.Errorf("上一条消息仍在处理中")
	}
	if b.client == nil {
		b.mu.Unlock()
		return nil, fmt.Errorf("LLM 客户端未配置")
	}
	b.running = true
	s := b.settings.Snapshot()
	sess := b.sessions.Current()
	ag := s.BuildAgent(b.client)
	ag.Session = sess
	ag.Ask = func(ctx context.Context, toolName, args string) (bool, error) {
		if s.AutoAllow || sess.AlwaysAllowed(toolName) {
			return true, nil
		}
		return true, nil
	}

	var cancelCtx context.Context
	cancelCtx, b.cancel = context.WithCancel(ctx)
	b.mu.Unlock()

	events := make(chan agent.Event, 64)
	go func() {
		defer close(events)
		_ = ag.RunWithAttachments(cancelCtx, input, nil, func(e agent.Event) error {
			events <- e
			return nil
		})
		b.mu.Lock()
		b.running = false
		b.cancel = nil
		_ = b.sessions.SaveAll()
		b.mu.Unlock()
	}()
	return events, nil
}

func (b *Backend) Interrupt() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel != nil {
		b.cancel()
	}
}

func (b *Backend) IsRunning() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running
}

func (b *Backend) UpdateSettings(fn func(s *settings.Settings)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	fn(b.settings)
	_ = b.settings.Save("")
}

func (b *Backend) RefreshClients() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.client != nil {
		if c, ok := b.client.(interface{ Close() }); ok {
			c.Close()
		}
	}
	client, err := b.settings.NewClient()
	if err != nil {
		b.client = nil
		return
	}
	b.client = client
}

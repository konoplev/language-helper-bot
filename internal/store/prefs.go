package store

import (
	"context"
	"sync"
)

// PrefsStore persists per-user preferences across the bot session.
type PrefsStore interface {
	Language(ctx context.Context, userID int64) (string, bool)
	SetLanguage(ctx context.Context, userID int64, lang string) error
}

// InMemoryPrefs is a thread-safe in-memory PrefsStore.
type InMemoryPrefs struct {
	mu    sync.RWMutex
	langs map[int64]string
}

func NewInMemoryPrefs() *InMemoryPrefs {
	return &InMemoryPrefs{langs: make(map[int64]string)}
}

func (p *InMemoryPrefs) Language(_ context.Context, userID int64) (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	l, ok := p.langs[userID]
	return l, ok
}

func (p *InMemoryPrefs) SetLanguage(_ context.Context, userID int64, lang string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.langs[userID] = lang
	return nil
}

package store

import (
	"context"
	"sync"

	"deutsch-helper/pkg/models"
)

// InMemoryPrefs is a thread-safe in-memory PrefsStore used in tests.
type InMemoryPrefs struct {
	mu   sync.RWMutex
	data map[int64]*models.UserSettings
}

func NewInMemoryPrefs() *InMemoryPrefs {
	return &InMemoryPrefs{data: make(map[int64]*models.UserSettings)}
}

func (p *InMemoryPrefs) GetSettings(_ context.Context, userID int64) (*models.UserSettings, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s, ok := p.data[userID]
	if !ok {
		return nil, false
	}
	cp := *s
	return &cp, true
}

func (p *InMemoryPrefs) SaveSettings(_ context.Context, userID int64, settings *models.UserSettings) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := *settings
	p.data[userID] = &cp
	return nil
}

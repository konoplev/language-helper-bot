package flows

import (
	"context"
	"sync"
)

type InMemoryStorage struct {
	mu     sync.RWMutex
	states map[int64]*UserState
}

func NewInMemoryStorage() *InMemoryStorage {
	return &InMemoryStorage{states: make(map[int64]*UserState)}
}

func (s *InMemoryStorage) Get(_ context.Context, userID int64) (*UserState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.states[userID]
	if !ok {
		return nil, nil
	}
	copy := *st
	copy.Payload = make(map[string]any, len(st.Payload))
	for k, v := range st.Payload {
		copy.Payload[k] = v
	}
	return &copy, nil
}

func (s *InMemoryStorage) Set(_ context.Context, state *UserState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *state
	copy.Payload = make(map[string]any, len(state.Payload))
	for k, v := range state.Payload {
		copy.Payload[k] = v
	}
	s.states[state.UserID] = &copy
	return nil
}

func (s *InMemoryStorage) Delete(_ context.Context, userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.states, userID)
	return nil
}

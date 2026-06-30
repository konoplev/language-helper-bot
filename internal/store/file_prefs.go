package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"deutsch-helper/pkg/models"
)

// FilePrefsStore persists user settings to a JSON file.
// The file contains a map of string(userID) → UserSettings.
type FilePrefsStore struct {
	mu       sync.RWMutex
	filePath string
	data     map[int64]*models.UserSettings
}

// NewFilePrefsStore loads existing data from filePath (creates the file if absent)
// and returns a ready-to-use store.
func NewFilePrefsStore(filePath string) (*FilePrefsStore, error) {
	s := &FilePrefsStore{
		filePath: filePath,
		data:     make(map[int64]*models.UserSettings),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FilePrefsStore) GetSettings(_ context.Context, userID int64) (*models.UserSettings, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[userID]
	if !ok {
		return nil, false
	}
	cp := *v
	return &cp, true
}

func (s *FilePrefsStore) SaveSettings(_ context.Context, userID int64, settings *models.UserSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *settings
	s.data[userID] = &cp
	return s.save()
}

func (s *FilePrefsStore) load() error {
	raw, err := os.ReadFile(s.filePath)
	if os.IsNotExist(err) {
		return nil // first run — no file yet
	}
	if err != nil {
		return fmt.Errorf("read prefs file: %w", err)
	}
	if len(raw) == 0 {
		return nil
	}

	// File stores string keys (JSON doesn't support integer keys).
	var strMap map[string]*models.UserSettings
	if err := json.Unmarshal(raw, &strMap); err != nil {
		return fmt.Errorf("parse prefs file: %w", err)
	}
	for k, v := range strMap {
		id, err := strconv.ParseInt(k, 10, 64)
		if err != nil {
			continue // skip corrupt entries
		}
		s.data[id] = v
	}
	return nil
}

func (s *FilePrefsStore) save() error {
	strMap := make(map[string]*models.UserSettings, len(s.data))
	for id, v := range s.data {
		strMap[strconv.FormatInt(id, 10)] = v
	}
	raw, err := json.MarshalIndent(strMap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal prefs: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0750); err != nil {
		return fmt.Errorf("create prefs dir: %w", err)
	}
	return os.WriteFile(s.filePath, raw, 0600)
}

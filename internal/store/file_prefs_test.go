package store_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"deutsch-helper/internal/store"
	"deutsch-helper/pkg/models"
)

func TestFilePrefsStoreGetSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.json")
	s, err := store.NewFilePrefsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Not found initially.
	if _, ok := s.GetSettings(ctx, 1); ok {
		t.Error("expected not found for new user")
	}

	settings := &models.UserSettings{
		NativeLanguage:   "en",
		LearningLanguage: "de",
		Level:            "B1",
		ActiveCommand:    "from",
	}
	if err := s.SaveSettings(ctx, 1, settings); err != nil {
		t.Fatal(err)
	}

	got, ok := s.GetSettings(ctx, 1)
	if !ok {
		t.Fatal("settings not found after save")
	}
	if got.NativeLanguage != "en" || got.LearningLanguage != "de" || got.Level != "B1" || got.ActiveCommand != "from" {
		t.Errorf("settings mismatch: %+v", got)
	}
}

func TestFilePrefsStorePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.json")
	ctx := context.Background()

	s1, err := store.NewFilePrefsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = s1.SaveSettings(ctx, 42, &models.UserSettings{
		NativeLanguage:   "ru",
		LearningLanguage: "en",
		Level:            "A2",
	})

	// New instance loading from same file.
	s2, err := store.NewFilePrefsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s2.GetSettings(ctx, 42)
	if !ok {
		t.Fatal("settings not persisted")
	}
	if got.NativeLanguage != "ru" || got.LearningLanguage != "en" || got.Level != "A2" {
		t.Errorf("reloaded settings mismatch: %+v", got)
	}
}

func TestFilePrefsStoreIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.json")
	ctx := context.Background()
	s, _ := store.NewFilePrefsStore(path)

	orig := &models.UserSettings{NativeLanguage: "en", LearningLanguage: "de", Level: "C1"}
	_ = s.SaveSettings(ctx, 1, orig)

	got, _ := s.GetSettings(ctx, 1)
	got.Level = "C2" // mutate the returned copy

	got2, _ := s.GetSettings(ctx, 1)
	if got2.Level != "C1" {
		t.Error("store not isolated: mutation of returned copy affected stored value")
	}
}

func TestFilePrefsStoreConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.json")
	ctx := context.Background()
	s, _ := store.NewFilePrefsStore(path)

	var wg sync.WaitGroup
	for i := int64(0); i < 10; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			_ = s.SaveSettings(ctx, id, &models.UserSettings{
				NativeLanguage:   "en",
				LearningLanguage: "de",
				Level:            "B2",
			})
			_, _ = s.GetSettings(ctx, id)
		}(i)
	}
	wg.Wait()
}

func TestFilePrefsStoreMissingDir(t *testing.T) {
	// The store should create the directory automatically.
	path := filepath.Join(t.TempDir(), "subdir", "nested", "prefs.json")
	ctx := context.Background()

	s, err := store.NewFilePrefsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSettings(ctx, 1, &models.UserSettings{Level: "A1"}); err != nil {
		t.Fatalf("save to nested path: %v", err)
	}
}

package middleware_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"deutsch-helper/internal/middleware"
	"deutsch-helper/pkg/models"
)

type stubSender struct {
	sentTo   int64
	sentText string
}

func (s *stubSender) SendMessage(_ context.Context, chatID int64, text string) (int, error) {
	s.sentTo = chatID
	s.sentText = text
	return 1, nil
}

func makeUC(userID, chatID int64) models.UpdateContext {
	return models.UpdateContext{
		UserID: userID,
		ChatID: chatID,
		Type:   models.UpdateTypeText,
	}
}

func TestAllowlistPermitsAllowedUser(t *testing.T) {
	sender := &stubSender{}
	al := middleware.NewAllowlist([]int64{42}, sender, nil)

	called := false
	mw := al.Middleware()(func(_ context.Context, _ models.UpdateContext) error {
		called = true
		return nil
	})

	if err := mw(context.Background(), makeUC(42, 42)); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("handler not called for allowed user")
	}
	if sender.sentText != "" {
		t.Error("unexpected denial message sent to allowed user")
	}
}

func TestAllowlistRejectsDeniedUser(t *testing.T) {
	sender := &stubSender{}
	al := middleware.NewAllowlist([]int64{42}, sender, nil)

	called := false
	mw := al.Middleware()(func(_ context.Context, _ models.UpdateContext) error {
		called = true
		return nil
	})

	if err := mw(context.Background(), makeUC(99, 99)); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("handler called for denied user")
	}
	if sender.sentTo != 99 {
		t.Errorf("denial message sent to %d, want 99", sender.sentTo)
	}
}

func TestAllowlistRejectsZeroUserID(t *testing.T) {
	sender := &stubSender{}
	al := middleware.NewAllowlist([]int64{0}, sender, nil) // even with 0 in list

	called := false
	mw := al.Middleware()(func(_ context.Context, _ models.UpdateContext) error {
		called = true
		return nil
	})

	if err := mw(context.Background(), makeUC(0, 0)); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("handler called for zero userID")
	}
}

func TestLoadAllowedUsers(t *testing.T) {
	content := "# comment\n42\n\n100\n# another comment\n999\n"
	f := filepath.Join(t.TempDir(), "users.txt")
	if err := os.WriteFile(f, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	ids, err := middleware.LoadAllowedUsers(f)
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{42, 100, 999}
	if len(ids) != len(want) {
		t.Fatalf("got %d ids, want %d", len(ids), len(want))
	}
	for i, id := range ids {
		if id != want[i] {
			t.Errorf("ids[%d] = %d, want %d", i, id, want[i])
		}
	}
}

func TestLoadAllowedUsersFileNotFound(t *testing.T) {
	_, err := middleware.LoadAllowedUsers("/nonexistent/path.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadAllowedUsersInvalidLine(t *testing.T) {
	f := filepath.Join(t.TempDir(), "bad.txt")
	if err := os.WriteFile(f, []byte("42\nnot-a-number\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := middleware.LoadAllowedUsers(f)
	if err == nil {
		t.Fatal("expected error for invalid line")
	}
}

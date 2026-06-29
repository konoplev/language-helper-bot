package telegram_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"go-telegram-template/internal/services/telegram"
	"go-telegram-template/pkg/models"
)

// apiResp mirrors the Telegram envelope for test servers.
type apiResp struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
}

func okJSON(t *testing.T, w http.ResponseWriter, result any) {
	t.Helper()
	raw, _ := json.Marshal(result)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(apiResp{OK: true, Result: raw})
}

func newTestClient(t *testing.T, mux *http.ServeMux) (*httptest.Server, *telegram.Client) {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := telegram.NewClient("testtoken",
		telegram.WithBaseURL(srv.URL),
		telegram.WithRetryDelay(0),
	)
	return srv, client
}

func TestSendMessage(t *testing.T) {
	mux := http.NewServeMux()
	var gotBody []byte
	mux.HandleFunc("/bottesttoken/sendMessage", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: want POST, got %s", r.Method)
		}
		gotBody, _ = io.ReadAll(r.Body)
		okJSON(t, w, map[string]any{"message_id": 42, "chat": map[string]any{"id": 100}})
	})

	_, client := newTestClient(t, mux)

	msgID, err := client.SendMessage(context.Background(), 100, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msgID != 42 {
		t.Fatalf("message_id: want 42, got %d", msgID)
	}
	if !strings.Contains(string(gotBody), "hello") {
		t.Fatalf("body does not contain text: %s", gotBody)
	}
}

func TestSendMessageWithKeyboard(t *testing.T) {
	mux := http.NewServeMux()
	var gotBody []byte
	mux.HandleFunc("/bottesttoken/sendMessage", func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		okJSON(t, w, map[string]any{"message_id": 7, "chat": map[string]any{"id": 1}})
	})

	_, client := newTestClient(t, mux)

	kb := models.NewInlineKeyboard(
		models.NewKeyboardRow(models.NewCallbackButton("OK", "cb:ok")),
	)
	msgID, err := client.SendMessageWithKeyboard(context.Background(), 1, "draft", kb)
	if err != nil {
		t.Fatal(err)
	}
	if msgID != 7 {
		t.Fatalf("message_id: want 7, got %d", msgID)
	}
	if !strings.Contains(string(gotBody), "inline_keyboard") {
		t.Fatalf("reply_markup missing from body: %s", gotBody)
	}
}

func TestGetFile(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/bottesttoken/getFile", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ FileID string `json:"file_id"` }
		json.NewDecoder(r.Body).Decode(&req)
		if req.FileID != "file123" {
			t.Errorf("file_id: want file123, got %s", req.FileID)
		}
		okJSON(t, w, map[string]any{"file_id": "file123", "file_path": "voice/file_123.ogg"})
	})

	_, client := newTestClient(t, mux)

	path, err := client.GetFile(context.Background(), "file123")
	if err != nil {
		t.Fatal(err)
	}
	if path != "voice/file_123.ogg" {
		t.Fatalf("path: want %q, got %q", "voice/file_123.ogg", path)
	}
}

func TestDownloadFile(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/file/bottesttoken/voice/test.ogg", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("OGG-DATA"))
	})

	_, client := newTestClient(t, mux)

	data, err := client.DownloadFile(context.Background(), "voice/test.ogg")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "OGG-DATA" {
		t.Fatalf("data: want OGG-DATA, got %s", data)
	}
}

func TestAnswerCallbackQuery(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/bottesttoken/answerCallbackQuery", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			CallbackQueryID string `json:"callback_query_id"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.CallbackQueryID != "cq-id" {
			t.Errorf("callback_query_id: want cq-id, got %s", req.CallbackQueryID)
		}
		okJSON(t, w, true)
	})

	_, client := newTestClient(t, mux)
	if err := client.AnswerCallbackQuery(context.Background(), "cq-id"); err != nil {
		t.Fatal(err)
	}
}

func TestSendVoice(t *testing.T) {
	cases := []struct {
		name      string
		chatID    int64
		fileID    string
		wantMsgID int
	}{
		{"standard send", 100, "voice-file-id-123", 55},
		{"different chat", 999, "voice-xyz", 56},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			var gotBody []byte
			mux.HandleFunc("/bottesttoken/sendVoice", func(w http.ResponseWriter, r *http.Request) {
				gotBody, _ = io.ReadAll(r.Body)
				okJSON(t, w, map[string]any{"message_id": tc.wantMsgID, "chat": map[string]any{"id": tc.chatID}})
			})
			_, client := newTestClient(t, mux)

			msgID, err := client.SendVoice(context.Background(), tc.chatID, tc.fileID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if msgID != tc.wantMsgID {
				t.Fatalf("message_id: want %d, got %d", tc.wantMsgID, msgID)
			}
			if !strings.Contains(string(gotBody), tc.fileID) {
				t.Fatalf("body missing file_id %q: %s", tc.fileID, gotBody)
			}
		})
	}
}

func TestSendPhoto(t *testing.T) {
	cases := []struct {
		name      string
		chatID    int64
		fileID    string
		caption   string
		wantMsgID int
	}{
		{"with caption", 100, "photo-file-id-abc", "Nice photo", 77},
		{"empty caption", 200, "photo-xyz", "", 78},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			var gotBody []byte
			mux.HandleFunc("/bottesttoken/sendPhoto", func(w http.ResponseWriter, r *http.Request) {
				gotBody, _ = io.ReadAll(r.Body)
				okJSON(t, w, map[string]any{"message_id": tc.wantMsgID, "chat": map[string]any{"id": tc.chatID}})
			})
			_, client := newTestClient(t, mux)

			msgID, err := client.SendPhoto(context.Background(), tc.chatID, tc.fileID, tc.caption)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if msgID != tc.wantMsgID {
				t.Fatalf("message_id: want %d, got %d", tc.wantMsgID, msgID)
			}
			if !strings.Contains(string(gotBody), tc.fileID) {
				t.Fatalf("body missing file_id %q: %s", tc.fileID, gotBody)
			}
			if tc.caption != "" && !strings.Contains(string(gotBody), tc.caption) {
				t.Fatalf("body missing caption %q: %s", tc.caption, gotBody)
			}
		})
	}
}

func TestRetryOnServerError(t *testing.T) {
	var attempts atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/bottesttoken/sendMessage", func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		okJSON(t, w, map[string]any{"message_id": 1, "chat": map[string]any{"id": 1}})
	})

	_, client := newTestClient(t, mux)
	_, err := client.SendMessage(context.Background(), 1, "retry me")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("attempts: want 3, got %d", attempts.Load())
	}
}

func TestTelegramErrorResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/bottesttoken/sendMessage", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":          false,
			"description": "Bad Request: chat not found",
		})
	})

	_, client := newTestClient(t, mux)
	_, err := client.SendMessage(context.Background(), 1, "test")
	if err == nil {
		t.Fatal("expected error for non-ok response")
	}
	if !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("error message: %v", err)
	}
}

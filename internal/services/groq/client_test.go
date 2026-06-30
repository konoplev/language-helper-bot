package groq_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"deutsch-helper/internal/services/groq"
)

type transcriptionResponse struct {
	Text string `json:"text"`
}

func newTestServer(t *testing.T, handler http.Handler) (*httptest.Server, *groq.Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := groq.NewClient("test-key",
		groq.WithBaseURL(srv.URL),
		groq.WithRetryDelay(0), // no sleep in tests
	)
	return srv, client
}

func TestTranscribeSuccess(t *testing.T) {
	_, client := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: want POST, got %s", r.Method)
		}
		if r.URL.Path != "/openai/v1/audio/transcriptions" {
			t.Errorf("path: want /openai/v1/audio/transcriptions, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("auth header: want 'Bearer test-key', got %q", r.Header.Get("Authorization"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
		}
		if r.FormValue("model") == "" {
			t.Error("model field missing")
		}
		if _, _, err := r.FormFile("file"); err != nil {
			t.Errorf("file field missing: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(transcriptionResponse{Text: "hello world"})
	}))

	text, err := client.Transcribe(context.Background(), []byte("fake-audio"), "voice.ogg", "en")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hello world" {
		t.Fatalf("text: want %q, got %q", "hello world", text)
	}
}

func TestTranscribeRetryOnServerError(t *testing.T) {
	var attempts atomic.Int32

	_, client := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(transcriptionResponse{Text: "retried text"})
	}))

	text, err := client.Transcribe(context.Background(), []byte("audio"), "voice.ogg", "en")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "retried text" {
		t.Fatalf("text: want %q, got %q", "retried text", text)
	}
	if attempts.Load() != 3 {
		t.Fatalf("attempts: want 3, got %d", attempts.Load())
	}
}

func TestTranscribeExhaustsRetries(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	client2 := groq.NewClient("test-key",
		groq.WithBaseURL(srv.URL),
		groq.WithMaxRetries(2),
		groq.WithRetryDelay(0),
	)

	_, err := client2.Transcribe(context.Background(), []byte("audio"), "voice.ogg", "en")
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
}

func TestTranscribeRespectsContextCancellation(t *testing.T) {
	_, client := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow response — context should cancel first.
		select {
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Second):
		}
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Transcribe(ctx, []byte("audio"), "voice.ogg", "en")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestTranscribeAuthHeader(t *testing.T) {
	var gotAuth string
	_, client := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(transcriptionResponse{Text: "ok"})
	}))

	_, err := client.Transcribe(context.Background(), []byte("a"), "f.ogg", "en")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("Authorization: want %q, got %q", "Bearer test-key", gotAuth)
	}
}

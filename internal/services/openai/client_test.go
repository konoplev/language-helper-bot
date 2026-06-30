package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"deutsch-helper/internal/services/openai"
)

func successResponse(text string) map[string]any {
	return map[string]any{
		"id":     "resp_test",
		"object": "response",
		"output": []map[string]any{
			{
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": text},
				},
			},
		},
	}
}

func okJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func newTestClient(t *testing.T, mux *http.ServeMux) *openai.Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return openai.NewClient("test-key", "gpt-4o", openai.WithBaseURL(srv.URL))
}

func TestCompleteSuccess(t *testing.T) {
	want := "Guten Tag"
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: want POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("auth header: got %q", r.Header.Get("Authorization"))
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req["model"] != "gpt-4o" {
			t.Errorf("model: want gpt-4o, got %v", req["model"])
		}
		if req["input"] == "" {
			t.Error("input field empty")
		}
		okJSON(t, w, successResponse(want))
	})

	client := newTestClient(t, mux)
	got, err := client.Complete(context.Background(), "Translate to German.", "Good day")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCompleteAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "invalid api key",
				"type":    "invalid_request_error",
			},
		})
	})

	client := newTestClient(t, mux)
	_, err := client.Complete(context.Background(), "system", "input")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCompleteContextCancellation(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	client := newTestClient(t, mux)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Complete(ctx, "system", "input")
	if err == nil {
		t.Fatal("expected error after context cancellation")
	}
}

func TestCompleteAuthHeader(t *testing.T) {
	const key = "sk-myTestKey123"
	var gotAuth string

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		okJSON(t, w, successResponse("ok"))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := openai.NewClient(key, "gpt-4o", openai.WithBaseURL(srv.URL))

	if _, err := client.Complete(context.Background(), "", "hi"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotAuth != "Bearer "+key {
		t.Errorf("Authorization: got %q, want %q", gotAuth, "Bearer "+key)
	}
}

func TestCompleteInstructionsField(t *testing.T) {
	const systemPrompt = "You are a translator."
	var gotInstructions string

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotInstructions, _ = req["instructions"].(string)
		okJSON(t, w, successResponse("translation"))
	})

	client := newTestClient(t, mux)
	if _, err := client.Complete(context.Background(), systemPrompt, "hello"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotInstructions != systemPrompt {
		t.Errorf("instructions: got %q, want %q", gotInstructions, systemPrompt)
	}
}

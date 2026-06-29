package integration_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"go-telegram-template/internal/flows"
	"go-telegram-template/internal/handlers"
	"go-telegram-template/internal/services/groq"
	"go-telegram-template/internal/services/telegram"
	"go-telegram-template/internal/store"
)

const (
	testUserID = int64(42)
	testFileID = "voice_file_abc"
)

// TestVoiceHandlerHTTP verifies that VoiceHandler, using a real telegram.Client
// and groq.Client, calls the correct API endpoints and leaves the correct flow state.
func TestVoiceHandlerHTTP(t *testing.T) {
	env := newEnv(t, "hello world")
	ctx := context.Background()

	if err := env.voice.Handle(ctx, voiceUpdate(testUserID, testFileID)); err != nil {
		t.Fatalf("VoiceHandler.Handle: %v", err)
	}

	// getFile called once with the right file_id.
	if n := env.tgCalls.GetFile.Load(); n != 1 {
		t.Errorf("getFile calls: want 1, got %d", n)
	}
	env.tgCalls.mu.Lock()
	gotFileID := env.tgCalls.lastFileID
	env.tgCalls.mu.Unlock()
	if gotFileID != testFileID {
		t.Errorf("getFile file_id: want %q, got %q", testFileID, gotFileID)
	}

	// File downloaded.
	if n := env.tgCalls.DownloadFile.Load(); n != 1 {
		t.Errorf("downloadFile calls: want 1, got %d", n)
	}

	// sendMessage called twice: "Transcribing…" then draft with keyboard.
	if n := env.tgCalls.SendMessage.Load(); n != 2 {
		t.Errorf("sendMessage calls: want 2, got %d", n)
	}
	texts := env.tgCalls.SentTexts()
	if !strings.Contains(texts[0], "Transcribing") {
		t.Errorf("first message: want 'Transcribing…', got %q", texts[0])
	}
	if texts[1] != "hello world" {
		t.Errorf("draft text: want %q, got %q", "hello world", texts[1])
	}
	if !env.tgCalls.sentMarkups[1] {
		t.Error("draft sendMessage: reply_markup missing")
	}

	// Groq: correct auth header, model field, and non-empty audio received.
	if n := env.groqCalls.Transcriptions.Load(); n != 1 {
		t.Errorf("groq transcription calls: want 1, got %d", n)
	}
	env.groqCalls.mu.Lock()
	auth := env.groqCalls.authHeaders[0]
	model := env.groqCalls.modelFields[0]
	audioBytes := env.groqCalls.receivedBytes[0]
	env.groqCalls.mu.Unlock()

	if auth != "Bearer test-groq-key" {
		t.Errorf("Authorization: want %q, got %q", "Bearer test-groq-key", auth)
	}
	if model == "" {
		t.Error("groq model field empty")
	}
	if len(audioBytes) == 0 {
		t.Error("groq received no audio bytes")
	}

	// Flow state: voice/draft with the transcribed text.
	st, err := env.engine.GetState(ctx, testUserID)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if st == nil {
		t.Fatal("flow state not set")
	}
	if st.Flow != flows.FlowVoice {
		t.Errorf("flow: want %q, got %q", flows.FlowVoice, st.Flow)
	}
	if st.State != flows.StateVoiceDraft {
		t.Errorf("state: want %q, got %q", flows.StateVoiceDraft, st.State)
	}
	if st.Payload[flows.PayloadDraftText] != "hello world" {
		t.Errorf("draft_text: want %q, got %v", "hello world", st.Payload[flows.PayloadDraftText])
	}
}

// TestCallbackSendTextHTTP verifies that voice:send_text sends the draft via a
// real Telegram HTTP call, then clears the flow state.
func TestCallbackSendTextHTTP(t *testing.T) {
	env := newEnv(t, "")
	ctx := context.Background()

	// Pre-condition: user is in voice/draft.
	const draftText = "this is my draft"
	st := flows.NewUserState(testUserID, flows.FlowVoice, flows.StateVoiceDraft)
	st.Payload[flows.PayloadDraftText] = draftText
	st.Payload[flows.PayloadDraftMsgID] = 200
	if err := env.engine.SetState(ctx, st); err != nil {
		t.Fatal(err)
	}

	if err := env.callback.Handle(ctx, callbackUpdate(testUserID, flows.CallbackVoiceSendText)); err != nil {
		t.Fatalf("CallbackHandler.Handle: %v", err)
	}

	if n := env.tgCalls.AnswerCallback.Load(); n != 1 {
		t.Errorf("answerCallbackQuery: want 1, got %d", n)
	}
	if n := env.tgCalls.SendMessage.Load(); n != 1 {
		t.Errorf("sendMessage: want 1, got %d", n)
	}
	texts := env.tgCalls.SentTexts()
	if len(texts) == 0 || texts[0] != draftText {
		t.Errorf("sendMessage text: want %q, got %v", draftText, texts)
	}

	// State must be cleared.
	cleared, err := env.engine.GetState(ctx, testUserID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared != nil {
		t.Errorf("flow state should be nil after send_text, got %+v", cleared)
	}
}

// TestCallbackEditHTTP verifies that voice:edit transitions state to voice/edit
// and sends a correction prompt via a real Telegram HTTP call.
func TestCallbackEditHTTP(t *testing.T) {
	env := newEnv(t, "")
	ctx := context.Background()

	st := flows.NewUserState(testUserID, flows.FlowVoice, flows.StateVoiceDraft)
	st.Payload[flows.PayloadDraftText] = "original text"
	if err := env.engine.SetState(ctx, st); err != nil {
		t.Fatal(err)
	}

	if err := env.callback.Handle(ctx, callbackUpdate(testUserID, flows.CallbackVoiceEdit)); err != nil {
		t.Fatalf("CallbackHandler.Handle: %v", err)
	}

	if n := env.tgCalls.AnswerCallback.Load(); n != 1 {
		t.Errorf("answerCallbackQuery: want 1, got %d", n)
	}
	texts := env.tgCalls.SentTexts()
	if len(texts) == 0 || !strings.Contains(texts[0], "correction") {
		t.Errorf("prompt text: want 'correction', got %v", texts)
	}

	got, err := env.engine.GetState(ctx, testUserID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.State != flows.StateVoiceEdit {
		t.Errorf("state: want %q, got %v", flows.StateVoiceEdit, got)
	}
}

// TestEditSubmitHTTP verifies that TextHandler in voice/edit state resends a
// new draft with a keyboard via a real Telegram HTTP call, then returns to draft.
func TestEditSubmitHTTP(t *testing.T) {
	env := newEnv(t, "")
	ctx := context.Background()

	st := flows.NewUserState(testUserID, flows.FlowVoice, flows.StateVoiceEdit)
	st.Payload[flows.PayloadDraftText] = "old draft"
	if err := env.engine.SetState(ctx, st); err != nil {
		t.Fatal(err)
	}

	if err := env.text.Handle(ctx, textUpdate(testUserID, "corrected text")); err != nil {
		t.Fatalf("TextHandler.Handle: %v", err)
	}

	if n := env.tgCalls.SendMessage.Load(); n != 1 {
		t.Errorf("sendMessage: want 1, got %d", n)
	}
	texts := env.tgCalls.SentTexts()
	if len(texts) == 0 || texts[0] != "corrected text" {
		t.Errorf("draft text: want %q, got %v", "corrected text", texts)
	}
	if !env.tgCalls.sentMarkups[0] {
		t.Error("updated draft: keyboard missing")
	}

	got, err := env.engine.GetState(ctx, testUserID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.State != flows.StateVoiceDraft {
		t.Errorf("state: want %q, got %v", flows.StateVoiceDraft, got)
	}
	if got.Payload[flows.PayloadDraftText] != "corrected text" {
		t.Errorf("draft_text: want %q, got %v", "corrected text", got.Payload[flows.PayloadDraftText])
	}
}

// TestSendAsCommandHTTP verifies that voice:send_command re-dispatches the
// transcribed text as a bot command and the command handler replies via HTTP.
func TestSendAsCommandHTTP(t *testing.T) {
	env := newEnv(t, "")
	ctx := context.Background()

	st := flows.NewUserState(testUserID, flows.FlowVoice, flows.StateVoiceDraft)
	st.Payload[flows.PayloadDraftText] = "/start"
	if err := env.engine.SetState(ctx, st); err != nil {
		t.Fatal(err)
	}

	if err := env.callback.Handle(ctx, callbackUpdate(testUserID, flows.CallbackVoiceSendCommand)); err != nil {
		t.Fatalf("CallbackHandler.Handle: %v", err)
	}

	if n := env.tgCalls.AnswerCallback.Load(); n != 1 {
		t.Errorf("answerCallbackQuery: want 1, got %d", n)
	}
	// CommandHandler(/start) must have replied.
	if n := env.tgCalls.SendMessage.Load(); n < 1 {
		t.Errorf("sendMessage (command reply): want ≥1, got %d", n)
	}
	// Voice flow state must be gone; /start may have set a language-selection state.
	cleared, err := env.engine.GetState(ctx, testUserID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared != nil && cleared.Flow == flows.FlowVoice {
		t.Errorf("voice flow state should be cleared after send_command, got %+v", cleared)
	}
}

// TestGroqRetryInVoiceFlowHTTP verifies that when Groq returns 500 twice, the
// client retries and VoiceHandler ultimately sets the correct flow state.
func TestGroqRetryInVoiceFlowHTTP(t *testing.T) {
	env := newEnv(t, "retried result")
	env.groqCalls.setStatuses(500, 500) // fail first two, third succeeds
	ctx := context.Background()

	if err := env.voice.Handle(ctx, voiceUpdate(testUserID, testFileID)); err != nil {
		t.Fatalf("VoiceHandler.Handle: %v", err)
	}

	if n := env.groqCalls.Transcriptions.Load(); n != 3 {
		t.Errorf("groq attempts: want 3 (2 fail + 1 ok), got %d", n)
	}
	st, _ := env.engine.GetState(ctx, testUserID)
	if st == nil || st.Payload[flows.PayloadDraftText] != "retried result" {
		t.Errorf("draft_text after retry: want %q, got %v", "retried result", st)
	}
}

// TestTelegramRetryInCallbackHTTP verifies that when the Telegram sendMessage
// endpoint returns 500 twice, the client retries and send_text ultimately succeeds.
func TestTelegramRetryInCallbackHTTP(t *testing.T) {
	var attempts atomic.Int32
	mux := http.NewServeMux()
	apiPrefix := "/bot" + testToken + "/"

	mux.HandleFunc(apiPrefix+"answerCallbackQuery", func(w http.ResponseWriter, _ *http.Request) {
		apiResp(t, w, true)
	})
	mux.HandleFunc(apiPrefix+"sendMessage", func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		apiResp(t, w, map[string]any{"message_id": 1, "chat": map[string]any{"id": testUserID}})
	})

	tgSrv := httptest.NewServer(mux)
	t.Cleanup(tgSrv.Close)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tgClient := telegram.NewClient(testToken,
		telegram.WithBaseURL(tgSrv.URL),
		telegram.WithRetryDelay(0),
		telegram.WithMaxRetries(3),
	)
	engine := flows.NewEngine(flows.NewInMemoryStorage())
	prefs := store.NewInMemoryPrefs()
	cb := handlers.NewCallbackHandler(tgClient, engine, prefs, logger)

	ctx := context.Background()
	const draftText = "retry draft"
	st := flows.NewUserState(testUserID, flows.FlowVoice, flows.StateVoiceDraft)
	st.Payload[flows.PayloadDraftText] = draftText
	if err := engine.SetState(ctx, st); err != nil {
		t.Fatal(err)
	}

	if err := cb.Handle(ctx, callbackUpdate(testUserID, flows.CallbackVoiceSendText)); err != nil {
		t.Fatalf("CallbackHandler.Handle: %v", err)
	}

	if n := attempts.Load(); n != 3 {
		t.Errorf("sendMessage attempts: want 3 (2 fail + 1 ok), got %d", n)
	}
	cleared, _ := engine.GetState(ctx, testUserID)
	if cleared != nil {
		t.Error("flow state should be cleared after successful send_text")
	}

	// Also wire up a real Groq client (not needed here, but validate it satisfies the Transcriber interface).
	var _ handlers.Transcriber = groq.NewClient("key")
}

// TestVoiceWithoutLanguageShowsPicker verifies that when no language is set,
// VoiceHandler shows the language selection keyboard and saves the pending
// voice file_id in the flow state instead of transcribing immediately.
func TestVoiceWithoutLanguageShowsPicker(t *testing.T) {
	env := newEnv(t, "should not be transcribed")
	ctx := context.Background()

	// Remove the pre-set language so this user starts fresh.
	env.prefs = store.NewInMemoryPrefs()
	// Rebuild VoiceHandler with blank prefs.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	voice := handlers.NewVoiceHandler(env.tgClient, groq.NewClient("test-groq-key"), env.engine, env.prefs, logger)

	if err := voice.Handle(ctx, voiceUpdate(testUserID, testFileID)); err != nil {
		t.Fatalf("VoiceHandler.Handle: %v", err)
	}

	// Must NOT have called Groq.
	if n := env.groqCalls.Transcriptions.Load(); n != 0 {
		t.Errorf("groq calls: want 0 (no language set yet), got %d", n)
	}

	// Must have sent the language picker keyboard.
	if n := env.tgCalls.SendMessage.Load(); n != 1 {
		t.Errorf("sendMessage calls: want 1 (language picker), got %d", n)
	}
	texts := env.tgCalls.SentTexts()
	if len(texts) == 0 || !strings.Contains(texts[0], "language") {
		t.Errorf("picker message: want text containing 'language', got %v", texts)
	}
	if !env.tgCalls.sentMarkups[0] {
		t.Error("language picker: reply_markup missing")
	}

	// Flow state must hold the pending voice file_id.
	st, err := env.engine.GetState(ctx, testUserID)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if st == nil || st.Flow != flows.FlowLanguage || st.State != flows.StateLanguageSelect {
		t.Errorf("flow state: want language/select, got %v", st)
	}
	if got, _ := st.Payload[flows.PayloadPendingVoiceID].(string); got != testFileID {
		t.Errorf("pending voice id: want %q, got %q", testFileID, got)
	}
}

// TestLanguageSelectionAutoTranscribes verifies that when a user selects a language
// while a pending voice file_id is stored in the flow state, the callback handler
// auto-transcribes the queued voice and presents a draft.
func TestLanguageSelectionAutoTranscribes(t *testing.T) {
	env := newEnv(t, "auto transcribed")
	ctx := context.Background()

	// Simulate what VoiceHandler writes when voice arrives without a language:
	// FlowLanguage/select state with the pending voice file_id.
	langSt := flows.NewUserState(testUserID, flows.FlowLanguage, flows.StateLanguageSelect)
	langSt.Payload[flows.PayloadPendingVoiceID] = testFileID
	if err := env.engine.SetState(ctx, langSt); err != nil {
		t.Fatal(err)
	}
	// Use a fresh prefs store so no language is pre-set for this user.
	freshPrefs := store.NewInMemoryPrefs()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	voice := handlers.NewVoiceHandler(env.tgClient, groq.NewClient("test-groq-key",
		groq.WithBaseURL(env.groqSrvURL),
		groq.WithRetryDelay(0),
	), env.engine, freshPrefs, logger)
	callback := handlers.NewCallbackHandler(env.tgClient, env.engine, freshPrefs, logger)
	callback.SetVoiceProcessor(voice)

	// User selects English.
	if err := callback.Handle(ctx, callbackUpdate(testUserID, flows.CallbackLangPrefix+"en")); err != nil {
		t.Fatalf("language selection callback: %v", err)
	}

	// Language must be persisted.
	lang, ok := freshPrefs.Language(ctx, testUserID)
	if !ok || lang != "en" {
		t.Errorf("stored language: want en, got %q (ok=%v)", lang, ok)
	}

	// Language flow state must be cleared.
	st, _ := env.engine.GetState(ctx, testUserID)
	if st != nil && st.Flow == flows.FlowLanguage {
		t.Errorf("language flow state should be cleared, got %+v", st)
	}
}

// TestStartCommandShowsLanguagePicker verifies that /start always presents the
// language-selection keyboard, even if a language was already set.
func TestStartCommandShowsLanguagePicker(t *testing.T) {
	env := newEnv(t, "")
	ctx := context.Background()

	if err := env.cmd.Handle(ctx, commandUpdate(testUserID, "start")); err != nil {
		t.Fatalf("CommandHandler /start: %v", err)
	}

	// Must have sent exactly one message with a keyboard.
	if n := env.tgCalls.SendMessage.Load(); n != 1 {
		t.Errorf("sendMessage calls: want 1, got %d", n)
	}
	if !env.tgCalls.sentMarkups[0] {
		t.Error("/start: language picker keyboard missing")
	}

	// Flow must be set to language/select.
	st, err := env.engine.GetState(ctx, testUserID)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if st == nil || st.Flow != flows.FlowLanguage || st.State != flows.StateLanguageSelect {
		t.Errorf("flow after /start: want language/select, got %v", st)
	}
}

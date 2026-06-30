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

	"deutsch-helper/internal/flows"
	"deutsch-helper/internal/handlers"
	"deutsch-helper/internal/services/groq"
	"deutsch-helper/internal/services/openai"
	"deutsch-helper/internal/services/telegram"
	"deutsch-helper/internal/store"
	"deutsch-helper/pkg/models"
)

// TestVoiceHandlerTranscribesAndAwaitsText verifies the full voice flow:
// download → transcribe → send transcription back → set voice/pending state.
func TestVoiceHandlerTranscribesAndAwaitsText(t *testing.T) {
	env := newEnv(t, "hello world", "")
	ctx := context.Background()

	if err := env.voice.Handle(ctx, voiceUpdate(testUserID, testFileID)); err != nil {
		t.Fatalf("VoiceHandler.Handle: %v", err)
	}

	if n := env.tgCalls.GetFile.Load(); n != 1 {
		t.Errorf("getFile calls: want 1, got %d", n)
	}
	if n := env.tgCalls.DownloadFile.Load(); n != 1 {
		t.Errorf("downloadFile calls: want 1, got %d", n)
	}

	// Two messages: "Transcribing…" then transcription + prompt.
	if n := env.tgCalls.SendMessage.Load(); n != 2 {
		t.Errorf("sendMessage calls: want 2, got %d", n)
	}
	texts := env.tgCalls.SentTexts()
	if !strings.Contains(texts[0], "Transcribing") {
		t.Errorf("first message: want Transcribing…, got %q", texts[0])
	}
	if !strings.Contains(texts[1], "hello world") {
		t.Errorf("transcription message: want 'hello world', got %q", texts[1])
	}

	// Flow state must be voice/pending.
	st, err := env.engine.GetState(ctx, testUserID)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if st == nil {
		t.Fatal("flow state not set")
	}
	if st.Flow != flows.FlowVoice || st.State != flows.StateVoicePending {
		t.Errorf("state: want voice/pending, got %s/%s", st.Flow, st.State)
	}
	if got, _ := st.Payload[flows.PayloadPendingTranscription].(string); got != "hello world" {
		t.Errorf("transcription payload: want %q, got %q", "hello world", got)
	}
	if got, _ := st.Payload[flows.PayloadActiveCommand].(string); got != "from" {
		t.Errorf("active_command payload: want 'from', got %q", got)
	}
}

// TestTextHandlerConfirmsVoiceAndCallsAI verifies that after voice/pending,
// the next text message is treated as the final input and sent to OpenAI.
func TestTextHandlerConfirmsVoiceAndCallsAI(t *testing.T) {
	const aiReply = "Guten Tag"
	env := newEnv(t, "", aiReply)
	ctx := context.Background()

	st := flows.NewUserState(testUserID, flows.FlowVoice, flows.StateVoicePending)
	st.Payload[flows.PayloadPendingTranscription] = "Hello"
	st.Payload[flows.PayloadActiveCommand] = "from"
	if err := env.engine.SetState(ctx, st); err != nil {
		t.Fatal(err)
	}

	if err := env.text.Handle(ctx, textUpdateCtx(testUserID, "Hello")); err != nil {
		t.Fatalf("TextHandler.Handle: %v", err)
	}

	if n := env.aiCalls.Completions.Load(); n != 1 {
		t.Errorf("AI completions: want 1, got %d", n)
	}
	texts := env.tgCalls.SentTexts()
	if len(texts) == 0 || texts[0] != aiReply {
		t.Errorf("response text: want %q, got %v", aiReply, texts)
	}

	// Flow state must be cleared.
	cleared, _ := env.engine.GetState(ctx, testUserID)
	if cleared != nil {
		t.Errorf("flow state should be cleared, got %+v", cleared)
	}
}

// TestDirectTextCallsAI verifies text → active command → OpenAI.
func TestDirectTextCallsAI(t *testing.T) {
	const aiReply = "Guten Morgen"
	env := newEnv(t, "", aiReply)
	ctx := context.Background()

	if err := env.text.Handle(ctx, textUpdateCtx(testUserID, "Good morning")); err != nil {
		t.Fatalf("TextHandler.Handle: %v", err)
	}

	if n := env.aiCalls.Completions.Load(); n != 1 {
		t.Errorf("AI completions: want 1, got %d", n)
	}
	texts := env.tgCalls.SentTexts()
	if len(texts) == 0 || texts[0] != aiReply {
		t.Errorf("response text: want %q, got %v", aiReply, texts)
	}
}

// TestSetupFlowCompletes verifies the three-step setup via callback buttons.
func TestSetupFlowCompletes(t *testing.T) {
	env := newEnv(t, "", "")
	ctx := context.Background()

	// /start sets setup flow and sends native language keyboard.
	if err := env.cmd.Handle(ctx, commandUpdateCtx(testUserID, "start")); err != nil {
		t.Fatalf("/start: %v", err)
	}
	if n := env.tgCalls.SendMessage.Load(); n != 1 {
		t.Errorf("messages after /start: want 1, got %d", n)
	}
	if !env.tgCalls.sentMarkups[0] {
		t.Error("/start: language keyboard missing")
	}

	// Pick native language.
	if err := env.callback.Handle(ctx, callbackUpdate(testUserID, flows.CallbackSetupNativePrefix+"ru")); err != nil {
		t.Fatalf("native lang: %v", err)
	}
	// Pick learning language.
	if err := env.callback.Handle(ctx, callbackUpdate(testUserID, flows.CallbackSetupLearningPrefix+"de")); err != nil {
		t.Fatalf("learning lang: %v", err)
	}
	// Pick level.
	if err := env.callback.Handle(ctx, callbackUpdate(testUserID, flows.CallbackSetupLevelPrefix+"B2")); err != nil {
		t.Fatalf("level: %v", err)
	}

	settings, ok := env.prefs.GetSettings(ctx, testUserID)
	if !ok {
		t.Fatal("settings not saved")
	}
	if settings.NativeLanguage != "ru" || settings.LearningLanguage != "de" || settings.Level != "B2" {
		t.Errorf("settings mismatch: %+v", settings)
	}

	// Flow state must be cleared after level selection.
	st, _ := env.engine.GetState(ctx, testUserID)
	if st != nil && st.Flow == flows.FlowSetup {
		t.Error("setup flow not cleared after completion")
	}
}

// TestCommandActivation verifies /from /to /polish update ActiveCommand in prefs.
func TestCommandActivation(t *testing.T) {
	cases := []struct{ cmd, want string }{
		{"from", "from"},
		{"to", "to"},
		{"polish", "polish"},
	}
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			env := newEnv(t, "", "")
			ctx := context.Background()
			if err := env.cmd.Handle(ctx, commandUpdateCtx(testUserID, tc.cmd)); err != nil {
				t.Fatalf("/%s: %v", tc.cmd, err)
			}
			settings, ok := env.prefs.GetSettings(ctx, testUserID)
			if !ok {
				t.Fatal("settings not found")
			}
			if settings.ActiveCommand != tc.want {
				t.Errorf("active command: want %q, got %q", tc.want, settings.ActiveCommand)
			}
		})
	}
}

// TestGroqRetryInVoiceFlow verifies that a failing Groq server is retried.
func TestGroqRetryInVoiceFlow(t *testing.T) {
	env := newEnv(t, "retried result", "")
	env.groqCalls.setStatuses(500, 500)
	ctx := context.Background()

	if err := env.voice.Handle(ctx, voiceUpdate(testUserID, testFileID)); err != nil {
		t.Fatalf("VoiceHandler.Handle: %v", err)
	}
	if n := env.groqCalls.Transcriptions.Load(); n != 3 {
		t.Errorf("groq attempts: want 3, got %d", n)
	}
	st, _ := env.engine.GetState(ctx, testUserID)
	if st == nil || st.Payload[flows.PayloadPendingTranscription] != "retried result" {
		t.Errorf("transcription payload after retry: got %v", st)
	}
}

// TestVoiceWithoutSetupRejectsAndDoesNotTranscribe verifies that voice is rejected
// when the user hasn't done /start yet.
func TestVoiceWithoutSetupRejectsAndDoesNotTranscribe(t *testing.T) {
	env := newEnv(t, "should not appear", "")
	ctx := context.Background()

	freshPrefs := store.NewInMemoryPrefs()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	voice := handlers.NewVoiceHandler(env.tgClient, groq.NewClient("test-groq-key",
		groq.WithBaseURL(env.groqSrvURL),
		groq.WithRetryDelay(0),
	), env.engine, freshPrefs, logger)

	if err := voice.Handle(ctx, voiceUpdate(testUserID, testFileID)); err != nil {
		t.Fatalf("VoiceHandler.Handle: %v", err)
	}
	if n := env.groqCalls.Transcriptions.Load(); n != 0 {
		t.Errorf("groq calls: want 0, got %d", n)
	}
	texts := env.tgCalls.SentTexts()
	if len(texts) == 0 || !strings.Contains(texts[0], "/start") {
		t.Errorf("expected /start guidance, got %v", texts)
	}
}

// TestTelegramRetryOnSendMessage verifies exponential backoff for sendMessage.
func TestTelegramRetryOnSendMessage(t *testing.T) {
	var attempts atomic.Int32
	mux := http.NewServeMux()
	apiPrefix := "/bot" + testToken + "/"

	mux.HandleFunc(apiPrefix+"sendMessage", func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		apiResp(t, w, map[string]any{"message_id": 1, "chat": map[string]any{"id": testUserID}})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tgClient := telegram.NewClient(testToken,
		telegram.WithBaseURL(srv.URL),
		telegram.WithRetryDelay(0),
		telegram.WithMaxRetries(3),
	)
	engine := flows.NewEngine(flows.NewInMemoryStorage())
	prefs := store.NewInMemoryPrefs()
	_ = prefs.SaveSettings(context.Background(), testUserID, &models.UserSettings{
		NativeLanguage:   "en",
		LearningLanguage: "de",
		Level:            "B1",
		ActiveCommand:    "from",
	})
	aiSrv, _ := newOpenAIMock(t, "translation")
	aiClient := openai.NewClient("key", "gpt-4o", openai.WithBaseURL(aiSrv.URL))
	textH := handlers.NewTextHandler(tgClient, engine, prefs, aiClient, logger)

	if err := textH.Handle(context.Background(), textUpdateCtx(testUserID, "hello")); err != nil {
		t.Fatalf("TextHandler.Handle: %v", err)
	}
	if n := attempts.Load(); n != 3 {
		t.Errorf("sendMessage attempts: want 3 (2 fail + 1 ok), got %d", n)
	}
}

// Compile-time checks that concrete clients satisfy the handler interfaces.
var _ handlers.Transcriber = groq.NewClient("key")
var _ handlers.TextProcessor = openai.NewClient("key", "gpt-4o")

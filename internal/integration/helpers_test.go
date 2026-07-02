// Package integration_test wires real telegram.Client, groq.Client, and openai.Client
// to local httptest servers and exercises complete handler pipelines end-to-end.
package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"deutsch-helper/internal/flows"
	"deutsch-helper/internal/handlers"
	"deutsch-helper/internal/services/groq"
	"deutsch-helper/internal/services/openai"
	"deutsch-helper/internal/services/telegram"
	"deutsch-helper/internal/store"
	"deutsch-helper/pkg/models"
)

const testToken = "integration-test-token"

// ---- minimal dispatcher ----

type minDispatcher struct {
	mu       sync.RWMutex
	handlers []handlers.Handler
}

func newDispatcher() *minDispatcher { return &minDispatcher{} }

func (d *minDispatcher) Register(h handlers.Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers = append(d.handlers, h)
}

func (d *minDispatcher) Dispatch(ctx context.Context, uc models.UpdateContext) error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, h := range d.handlers {
		if h.CanHandle(uc) {
			return h.Handle(ctx, uc)
		}
	}
	return nil
}

// ---- Telegram mock ----

type telegramCalls struct {
	GetFile           atomic.Int32
	DownloadFile      atomic.Int32
	SendMessage       atomic.Int32
	AnswerCallback    atomic.Int32
	AnswerInlineQuery atomic.Int32
	SetCommands       atomic.Int32

	mu          sync.Mutex
	sentTexts   []string
	sentMarkups []bool
	lastFileID  string
}

func (c *telegramCalls) appendSend(text string, hasMarkup bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sentTexts = append(c.sentTexts, text)
	c.sentMarkups = append(c.sentMarkups, hasMarkup)
}

func (c *telegramCalls) SentTexts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.sentTexts))
	copy(out, c.sentTexts)
	return out
}

func newTelegramMock(t *testing.T, filePath string, fileBytes []byte) (*httptest.Server, *telegramCalls) {
	t.Helper()
	calls := &telegramCalls{}
	mux := http.NewServeMux()
	apiPrefix := "/bot" + testToken + "/"

	mux.HandleFunc(apiPrefix+"getFile", func(w http.ResponseWriter, r *http.Request) {
		calls.GetFile.Add(1)
		var req struct{ FileID string `json:"file_id"` }
		_ = json.NewDecoder(r.Body).Decode(&req)
		calls.mu.Lock()
		calls.lastFileID = req.FileID
		calls.mu.Unlock()
		apiResp(t, w, map[string]any{"file_id": req.FileID, "file_path": filePath})
	})

	mux.HandleFunc("/file/bot"+testToken+"/", func(w http.ResponseWriter, _ *http.Request) {
		calls.DownloadFile.Add(1)
		w.Write(fileBytes)
	})

	mux.HandleFunc(apiPrefix+"sendMessage", func(w http.ResponseWriter, r *http.Request) {
		n := calls.SendMessage.Add(1)
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Text        string          `json:"text"`
			ReplyMarkup json.RawMessage `json:"reply_markup"`
		}
		_ = json.Unmarshal(body, &req)
		calls.appendSend(req.Text, len(req.ReplyMarkup) > 0)
		apiResp(t, w, map[string]any{"message_id": int(n) * 10, "chat": map[string]any{"id": 99999}})
	})

	mux.HandleFunc(apiPrefix+"answerCallbackQuery", func(w http.ResponseWriter, _ *http.Request) {
		calls.AnswerCallback.Add(1)
		apiResp(t, w, true)
	})

	mux.HandleFunc(apiPrefix+"answerInlineQuery", func(w http.ResponseWriter, _ *http.Request) {
		calls.AnswerInlineQuery.Add(1)
		apiResp(t, w, true)
	})

	mux.HandleFunc(apiPrefix+"setMyCommands", func(w http.ResponseWriter, _ *http.Request) {
		calls.SetCommands.Add(1)
		apiResp(t, w, true)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, calls
}

// ---- Groq mock ----

type groqCalls struct {
	Transcriptions atomic.Int32

	mu            sync.Mutex
	authHeaders   []string
	modelFields   []string
	receivedBytes [][]byte
	nextStatus    []int
}

func (g *groqCalls) setStatuses(codes ...int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nextStatus = codes
}

func (g *groqCalls) popStatus() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.nextStatus) == 0 {
		return http.StatusOK
	}
	code := g.nextStatus[0]
	g.nextStatus = g.nextStatus[1:]
	return code
}

func newGroqMock(t *testing.T, transcribedText string) (*httptest.Server, *groqCalls) {
	t.Helper()
	calls := &groqCalls{}
	mux := http.NewServeMux()
	mux.HandleFunc("/openai/v1/audio/transcriptions", func(w http.ResponseWriter, r *http.Request) {
		status := calls.popStatus()
		calls.Transcriptions.Add(1)
		calls.mu.Lock()
		calls.authHeaders = append(calls.authHeaders, r.Header.Get("Authorization"))
		calls.mu.Unlock()
		if err := r.ParseMultipartForm(4 << 20); err == nil {
			calls.mu.Lock()
			calls.modelFields = append(calls.modelFields, r.FormValue("model"))
			if f, _, err2 := r.FormFile("file"); err2 == nil {
				data, _ := io.ReadAll(f)
				calls.receivedBytes = append(calls.receivedBytes, data)
			}
			calls.mu.Unlock()
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"text": transcribedText})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, calls
}

// ---- OpenAI mock ----

type openAICalls struct {
	Completions atomic.Int32

	mu       sync.Mutex
	response string
}

func (o *openAICalls) setResponse(r string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.response = r
}

func newOpenAIMock(t *testing.T, response string) (*httptest.Server, *openAICalls) {
	t.Helper()
	calls := &openAICalls{response: response}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, _ *http.Request) {
		calls.Completions.Add(1)
		calls.mu.Lock()
		resp := calls.response
		calls.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":     "resp_test",
			"object": "response",
			"output": []map[string]any{
				{
					"type": "message",
					"role": "assistant",
					"content": []map[string]any{
						{"type": "output_text", "text": resp},
					},
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, calls
}

// ---- helpers ----

func apiResp(t *testing.T, w http.ResponseWriter, result any) {
	t.Helper()
	type envelope struct {
		OK     bool `json:"ok"`
		Result any  `json:"result"`
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(envelope{OK: true, Result: result}); err != nil {
		t.Errorf("apiResp encode: %v", err)
	}
}

// ---- Update constructors ----

const testFileID = "voice_file_abc"

func voiceUpdate(userID int64, fileID string) models.UpdateContext {
	return models.NewUpdateContext(tgbotapi.Update{
		UpdateID: 1,
		Message: &tgbotapi.Message{
			MessageID: 100,
			From:      &tgbotapi.User{ID: userID, FirstName: "Test"},
			Chat:      &tgbotapi.Chat{ID: userID, Type: "private"},
			Voice:     &tgbotapi.Voice{FileID: fileID, Duration: 3, MimeType: "audio/ogg"},
		},
	})
}

func callbackUpdate(userID int64, data string) models.UpdateContext {
	return models.NewUpdateContext(tgbotapi.Update{
		UpdateID: 2,
		CallbackQuery: &tgbotapi.CallbackQuery{
			ID:   "cq-" + data,
			From: &tgbotapi.User{ID: userID, FirstName: "Test"},
			Message: &tgbotapi.Message{
				MessageID: 200,
				Chat:      &tgbotapi.Chat{ID: userID},
				From:      &tgbotapi.User{ID: 0},
			},
			Data: data,
		},
	})
}

func textUpdateCtx(userID int64, text string) models.UpdateContext {
	return models.NewUpdateContext(tgbotapi.Update{
		UpdateID: 3,
		Message: &tgbotapi.Message{
			MessageID: 300,
			From:      &tgbotapi.User{ID: userID, FirstName: "Test"},
			Chat:      &tgbotapi.Chat{ID: userID, Type: "private"},
			Text:      text,
		},
	})
}

func commandUpdateCtx(userID int64, cmd string) models.UpdateContext {
	return models.NewUpdateContext(tgbotapi.Update{
		UpdateID: 4,
		Message: &tgbotapi.Message{
			MessageID: 400,
			From:      &tgbotapi.User{ID: userID, FirstName: "Test"},
			Chat:      &tgbotapi.Chat{ID: userID, Type: "private"},
			Text:      "/" + cmd,
			Entities:  []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: len(cmd) + 1}},
		},
	})
}

// ---- Full wired environment ----

const testUserID = int64(42)

type testEnv struct {
	tgCalls    *telegramCalls
	groqCalls  *groqCalls
	aiCalls    *openAICalls
	tgClient   *telegram.Client
	groqSrvURL string
	aiSrvURL   string
	engine     *flows.Engine
	prefs      *store.InMemoryPrefs
	dispatch   *minDispatcher

	voice    *handlers.VoiceHandler
	text     *handlers.TextHandler
	callback *handlers.CallbackHandler
	cmd      *handlers.CommandHandler
}

func newEnv(t *testing.T, transcribedText, aiResponse string) *testEnv {
	t.Helper()

	fakeAudio := []byte("OGG-BYTES")
	tgSrv, tgCalls := newTelegramMock(t, "voice/test.ogg", fakeAudio)
	groqSrv, groqCalls := newGroqMock(t, transcribedText)
	aiSrv, aiCalls := newOpenAIMock(t, aiResponse)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tgClient := telegram.NewClient(testToken,
		telegram.WithBaseURL(tgSrv.URL),
		telegram.WithRetryDelay(0),
	)
	groqClient := groq.NewClient("test-groq-key",
		groq.WithBaseURL(groqSrv.URL),
		groq.WithRetryDelay(0),
	)
	aiClient := openai.NewClient("test-openai-key", "gpt-4o",
		openai.WithBaseURL(aiSrv.URL),
	)

	engine := flows.NewEngine(flows.NewInMemoryStorage())
	prefs := store.NewInMemoryPrefs()

	// Pre-configure a fully set-up user so most tests skip the setup flow.
	_ = prefs.SaveSettings(context.Background(), testUserID, &models.UserSettings{
		NativeLanguage:   "en",
		LearningLanguage: "de",
		Level:            "B1",
		ActiveCommand:    "from",
	})

	disp := newDispatcher()

	voice := handlers.NewVoiceHandler(tgClient, groqClient, engine, prefs, logger)
	text := handlers.NewTextHandler(tgClient, engine, prefs, aiClient, logger)
	callback := handlers.NewCallbackHandler(tgClient, engine, prefs, logger)
	cmd := handlers.NewCommandHandler(tgClient, engine, prefs, logger)

	disp.Register(callback)
	disp.Register(cmd)
	disp.Register(voice)
	disp.Register(text)

	return &testEnv{
		tgCalls:    tgCalls,
		groqCalls:  groqCalls,
		aiCalls:    aiCalls,
		tgClient:   tgClient,
		groqSrvURL: groqSrv.URL,
		aiSrvURL:   aiSrv.URL,
		engine:     engine,
		prefs:      prefs,
		dispatch:   disp,
		voice:      voice,
		text:       text,
		callback:   callback,
		cmd:        cmd,
	}
}

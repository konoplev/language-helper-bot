# go-telegram-template

A production-ready Go template for building Telegram bots. Includes a handler-based dispatcher, per-user flow/state-machine engine, middleware pipeline, explicit HTTP clients for all external services, and HTTP-level integration tests via `net/http/httptest`.

---

## Table of contents

1. [Project structure](#1-project-structure)
2. [Architecture overview](#2-architecture-overview)
3. [Handler system](#3-handler-system)
4. [Flow / state-machine system](#4-flow--state-machine-system)
5. [Voice message flow (built-in example)](#5-voice-message-flow-built-in-example)
6. [Middleware pipeline](#6-middleware-pipeline)
7. [External services](#7-external-services)
8. [Configuration](#8-configuration)
9. [Testing](#9-testing)
10. [Running with real Telegram — step by step](#10-running-with-real-telegram--step-by-step)
11. [Docker / docker-compose](#11-docker--docker-compose)
12. [Extensibility guide](#12-extensibility-guide)

---

## 1. Project structure

```
go-telegram-template/
├── cmd/
│   └── bot/
│       └── main.go                  entry point: config, logger, wiring, signal handling
├── internal/
│   ├── bot/
│   │   ├── bot.go                   Bot struct, New(), Run(), polling/webhook branching
│   │   ├── dispatcher.go            Dispatcher — routes updates to handlers
│   │   └── webhook.go               WebhookServer — HTTP handler for Telegram webhook mode
│   ├── flows/
│   │   ├── state.go                 UserState, FlowName, StateName types
│   │   ├── storage.go               StateStorage interface
│   │   ├── memory_storage.go        InMemoryStorage (thread-safe, copy-on-read/write)
│   │   ├── engine.go                Engine struct + Manager interface
│   │   ├── voice_flow.go            Voice flow constants (states, payload keys, callback data)
│   │   └── engine_test.go
│   ├── handlers/
│   │   ├── handler.go               Handler, Dispatcher, TelegramAPI, Transcriber interfaces
│   │   ├── command.go               CommandHandler — /start, /help, extensible registry
│   │   ├── text.go                  TextHandler — plain text + voice draft editing
│   │   ├── voice.go                 VoiceHandler — download → transcribe → draft message
│   │   ├── audio.go                 AudioHandler — audio file messages
│   │   ├── image.go                 ImageHandler — photo messages
│   │   ├── callback.go              CallbackHandler — inline keyboard responses
│   │   └── dispatch_test.go
│   ├── middleware/
│   │   ├── middleware.go            HandlerFunc, Middleware, Chain, Logging, Recovery, Timing
│   │   └── middleware_test.go
│   └── services/
│       ├── groq/
│       │   ├── client.go            Groq transcription client (multipart POST, exponential backoff)
│       │   └── client_test.go
│       └── telegram/
│           ├── types.go             API request/response structs
│           ├── client.go            Telegram HTTP client (no hidden SDK calls)
│           └── client_test.go
└── pkg/
    └── models/
        ├── config.go                Config, LoadConfig() — reads environment variables
        ├── update.go                UpdateContext, UpdateType, NewUpdateContext()
        └── keyboard.go              InlineKeyboardMarkup, helpers
```

---

## 2. Architecture overview

```
Telegram ──► tgbotapi (long-poll) ──► Bot.runPolling()
                                           │
                        ┌──────────────────▼──────────────────────┐
                        │              Dispatcher                  │
                        │  middleware chain (Recovery→Timing→Log)  │
                        │  ordered handler list (first CanHandle)  │
                        └──┬────────┬────────┬────────┬───────────┘
                           │        │        │        │
                      Callback  Command   Voice   Text/Audio/Image
                      Handler   Handler  Handler    Handlers
                           │                │
                      flows.Manager    groq.Client
                      (per-user state)  (Transcribe)
                                             │
                                     telegram.Client
                                     (SendMessage, etc.)
```

Key principles:

- **tgbotapi is used only for receiving updates** (long-polling transport). All outgoing API calls go through the custom `telegram.Client` using a plain `http.Client`. This makes every outbound call auditable and testable.
- **All external service clients accept `WithBaseURL()`** so tests can point them at a local `httptest.Server` without any mocking frameworks.
- **The flow engine is storage-agnostic** — swap `InMemoryStorage` for Redis by implementing three methods (`Get`, `Set`, `Delete`) and changing one line in `bot.go`.

---

## 3. Handler system

### The Handler interface

```go
// internal/handlers/handler.go
type Handler interface {
    CanHandle(uc models.UpdateContext) bool
    Handle(ctx context.Context, uc models.UpdateContext) error
}
```

The `Dispatcher` iterates its ordered handler list and calls `Handle` on the **first** handler whose `CanHandle` returns `true`.

### UpdateContext

Every incoming Telegram update is wrapped in `models.UpdateContext`:

```go
type UpdateContext struct {
    Update tgbotapi.Update // raw tgbotapi update
    Type   UpdateType      // command | text | voice | audio | image | callback_query
    UserID int64
    ChatID int64
}
```

`models.NewUpdateContext(update)` populates `Type`, `UserID`, and `ChatID` automatically.

### Supported update types

| UpdateType | Handler | Triggered by |
|---|---|---|
| `command` | `CommandHandler` | `/start`, `/help`, any `/cmd` |
| `text` | `TextHandler` | Plain text messages |
| `voice` | `VoiceHandler` | Voice notes recorded in Telegram |
| `audio` | `AudioHandler` | Audio files (mp3, m4a, etc.) |
| `image` | `ImageHandler` | Photo messages |
| `callback_query` | `CallbackHandler` | Inline keyboard button taps |

### Registering a new handler

```go
// 1. Implement the Handler interface
type MyHandler struct{ tg handlers.TelegramAPI }

func (h *MyHandler) CanHandle(uc models.UpdateContext) bool {
    return uc.Type == models.UpdateTypeText &&
           strings.HasPrefix(uc.Update.Message.Text, "!magic")
}

func (h *MyHandler) Handle(ctx context.Context, uc models.UpdateContext) error {
    _, err := h.tg.SendMessage(ctx, uc.ChatID, "✨ Magic!")
    return err
}

// 2. Register — in bot.go, before dispatcher.Register(textHandler):
dispatcher.Register(&MyHandler{tg: tgClient})
```

Order matters: handlers are checked top-to-bottom. Register more specific handlers before catch-all ones.

### Adding custom bot commands

```go
cmdHandler := handlers.NewCommandHandler(tgClient, logger)

// Register any /command handler with a closure:
cmdHandler.Register("greet", func(ctx context.Context, uc models.UpdateContext) error {
    name := uc.Update.Message.From.FirstName
    _, err := tgClient.SendMessage(ctx, uc.ChatID, "Hello, "+name+"!")
    return err
})
```

### The TelegramAPI interface

All handlers depend on this interface (not on the concrete client), which makes them trivially testable with a stub:

```go
type TelegramAPI interface {
    SendMessage(ctx context.Context, chatID int64, text string) (int, error)
    SendMessageWithKeyboard(ctx context.Context, chatID int64, text string, kb models.InlineKeyboardMarkup) (int, error)
    SendVoice(ctx context.Context, chatID int64, fileID string) (int, error)
    SendPhoto(ctx context.Context, chatID int64, fileID string, caption string) (int, error)
    EditMessageText(ctx context.Context, chatID int64, messageID int, text string) error
    DeleteMessage(ctx context.Context, chatID int64, messageID int) error
    GetFile(ctx context.Context, fileID string) (string, error)   // returns file_path
    DownloadFile(ctx context.Context, filePath string) ([]byte, error)
    AnswerCallbackQuery(ctx context.Context, callbackID string) error
}
```

---

## 4. Flow / state-machine system

### Core types

```go
// internal/flows/state.go
type FlowName  string
type StateName string

type UserState struct {
    UserID  int64
    Flow    FlowName
    State   StateName
    Payload map[string]any  // arbitrary per-user context
}
```

### Storage interface

```go
// internal/flows/storage.go
type StateStorage interface {
    Get(ctx context.Context, userID int64) (*UserState, error)
    Set(ctx context.Context, state *UserState) error
    Delete(ctx context.Context, userID int64) error
}
```

The default implementation is `InMemoryStorage` (thread-safe, copy-on-read-write to prevent aliasing bugs). To swap in Redis, implement this three-method interface and pass it to `flows.NewEngine(yourRedisStorage)`.

### Engine / Manager interface

```go
// internal/flows/engine.go
type Manager interface {
    GetState(ctx context.Context, userID int64) (*UserState, error)
    SetState(ctx context.Context, state *UserState) error
    ClearState(ctx context.Context, userID int64) error
    IsInFlow(ctx context.Context, userID int64, flow FlowName) (bool, error)
    IsInState(ctx context.Context, userID int64, flow FlowName, state StateName) (bool, error)
}
```

### Defining a new flow

```go
// myfeature_flow.go
package flows

const (
    FlowMyFeature FlowName = "my_feature"

    StateMyFeatureStep1 StateName = "step1"
    StateMyFeatureStep2 StateName = "step2"

    PayloadMyFeatureData = "my_data"
)
```

Then in a handler:

```go
// Start the flow
st := flows.NewUserState(uc.UserID, flows.FlowMyFeature, flows.StateMyFeatureStep1)
st.Payload[flows.PayloadMyFeatureData] = someValue
engine.SetState(ctx, st)

// Check state in another handler
inStep2, _ := engine.IsInState(ctx, uc.UserID, flows.FlowMyFeature, flows.StateMyFeatureStep2)

// Transition
st.State = flows.StateMyFeatureStep2
engine.SetState(ctx, st)

// End the flow
engine.ClearState(ctx, uc.UserID)
```

Each user's flow state is completely independent. There is no shared mutable state between users.

---

## 5. Voice message flow (built-in example)

This flow is fully implemented and serves as the primary usage example for the flow engine.

### State diagram

```
User sends voice
       │
       ▼
[VoiceHandler]
  Download file (GetFile → DownloadFile)
  Transcribe (Groq API)
  Send draft message with inline keyboard
  SetState → voice / draft
       │
       ├─ user taps "Edit"
       │       │
       │  [CallbackHandler: voice:edit]
       │   SetState → voice / edit
       │   Send "Please type your correction:"
       │       │
       │  [TextHandler: in voice/edit state]
       │   Update draft text in payload
       │   Re-send draft message with keyboard
       │   SetState → voice / draft
       │
       ├─ user taps "Send as text"
       │       │
       │  [CallbackHandler: voice:send_text]
       │   SendMessage(draft text)
       │   ClearState
       │
       └─ user taps "Send as command"
               │
          [CallbackHandler: voice:send_command]
           Parse transcribed text as /<command>
           Re-dispatch synthetic command update
           ClearState
```

### Flow constants

```go
// internal/flows/voice_flow.go
FlowVoice        FlowName  = "voice"
StateVoiceDraft  StateName = "draft"
StateVoiceEdit   StateName = "edit"

PayloadDraftText  = "draft_text"   // transcribed string
PayloadDraftMsgID = "draft_msg_id" // message ID of the sent draft

CallbackVoiceEdit        = "voice:edit"
CallbackVoiceSendText    = "voice:send_text"
CallbackVoiceSendCommand = "voice:send_command"
```

---

## 6. Middleware pipeline

### Types

```go
type HandlerFunc func(ctx context.Context, uc models.UpdateContext) error
type Middleware  func(next HandlerFunc) HandlerFunc
```

### Provided middleware

| Middleware | What it does |
|---|---|
| `Recovery(logger)` | Catches any `panic` in the handler chain and converts it to an error. Logs the panic value and user ID. |
| `Timing(duration)` | Injects a `context.WithTimeout` into every update. Default: 45 s. |
| `Logging(logger)` | Logs update type, user ID, handler duration, and outcome (info on success, error on failure). |

### Chain order

Middleware is applied **outermost-first**. The registration order in `bot.go`:

```go
dispatcher.Use(
    middleware.Recovery(logger),  // outermost — catches panics from everything below
    middleware.Timing(45*time.Second),
    middleware.Logging(logger),   // innermost — sees the actual handler duration
)
```

Call sequence for a single update:

```
Recovery → Timing → Logging → Handler → Logging → Timing → Recovery
```

### Writing a custom middleware

```go
func RateLimit(maxPerSec int) middleware.Middleware {
    limiter := rate.NewLimiter(rate.Limit(maxPerSec), maxPerSec)
    return func(next middleware.HandlerFunc) middleware.HandlerFunc {
        return func(ctx context.Context, uc models.UpdateContext) error {
            if !limiter.Allow() {
                return fmt.Errorf("rate limit exceeded")
            }
            return next(ctx, uc)
        }
    }
}

// Register:
dispatcher.Use(RateLimit(10))
```

---

## 7. External services

### Telegram client (`internal/services/telegram`)

A plain HTTP client — no hidden SDK calls for outbound traffic.

**URL patterns:**

```
API calls:   https://api.telegram.org/bot{token}/{method}
File download: https://api.telegram.org/file/bot{token}/{file_path}
```

**Available methods:**

| Method | Signature |
|---|---|
| `SendMessage` | `(ctx, chatID, text) → (messageID, error)` |
| `SendMessageWithKeyboard` | `(ctx, chatID, text, InlineKeyboardMarkup) → (messageID, error)` |
| `SendVoice` | `(ctx, chatID, fileID) → (messageID, error)` |
| `SendPhoto` | `(ctx, chatID, fileID, caption) → (messageID, error)` |
| `EditMessageText` | `(ctx, chatID, messageID, text) → error` |
| `DeleteMessage` | `(ctx, chatID, messageID) → error` |
| `GetFile` | `(ctx, fileID) → (filePath, error)` |
| `DownloadFile` | `(ctx, filePath) → ([]byte, error)` |
| `AnswerCallbackQuery` | `(ctx, callbackID) → error` |

**Retry policy:** exponential backoff on HTTP 429 (rate limit) and 5xx responses. Defaults: 3 retries, 300 ms base delay (doubles each attempt: 300 ms → 600 ms → 1.2 s).

**Constructor options:**

```go
telegram.NewClient(token,
    telegram.WithBaseURL("https://api.telegram.org"),  // override for tests
    telegram.WithMaxRetries(5),
    telegram.WithRetryDelay(200 * time.Millisecond),
    telegram.WithHTTPClient(myHTTPClient),
)
```

### Groq client (`internal/services/groq`)

Sends audio bytes to the Groq Whisper API and returns the transcribed text.

**Endpoint:** `POST https://api.groq.com/openai/v1/audio/transcriptions`

**Request format:** `multipart/form-data` with fields:
- `file` — raw audio bytes (any format Telegram provides; `.ogg` for voice notes)
- `model` — default `whisper-large-v3-turbo`
- `response_format` — `json`

**Auth:** `Authorization: Bearer {GROQ_API_KEY}` header.

**Retry policy:** exponential backoff on network errors and HTTP 5xx. Defaults: 3 retries, 500 ms base delay. The context is respected between retry sleeps — cancellation stops immediately.

**Constructor options:**

```go
groq.NewClient(apiKey,
    groq.WithBaseURL("https://api.groq.com"),
    groq.WithModel("whisper-large-v3-turbo"),
    groq.WithMaxRetries(3),
    groq.WithRetryDelay(500 * time.Millisecond),
    groq.WithHTTPClient(myHTTPClient),
)
```

---

## 8. Configuration

All configuration is read from environment variables at startup. No config files.

| Variable | Required | Default | Description |
|---|---|---|---|
| `BOT_TOKEN` | yes | — | Telegram bot token from BotFather (e.g. `123456:ABC-DEF...`) |
| `GROQ_API_KEY` | yes | — | Groq API key from console.groq.com |
| `LOG_LEVEL` | no | `info` | Log verbosity: `debug` or `info` |
| `POLLING_MODE` | no | `long_polling` | Update source: `long_polling` or `webhook` |

### Webhook mode

When `POLLING_MODE=webhook`, the bot starts an HTTP server (default `:8080`) instead of polling. You must separately register the webhook URL with Telegram using the `setWebhook` API method. The listen address can be configured in `bot.New(...)`:

```go
bot.New(token, "webhook", tgClient, groqClient, logger,
    bot.WithWebhookAddr(":443"),
)
```

---

## 9. Testing

### Running tests

```bash
go test ./...
```

### Test packages

| Package | Tests |
|---|---|
| `internal/flows` | Engine state get/set/clear, `IsInFlow`, `IsInState`, storage copy isolation |
| `internal/handlers` | Dispatcher routing (table-driven), error propagation, command routing |
| `internal/middleware` | Chain ordering, Recovery (panic cases), Logging (propagation), Timing (deadline injection) |
| `internal/services/groq` | HTTP-level: success, retry on 5xx, exhausted retries, context cancellation, auth header |
| `internal/services/telegram` | HTTP-level: sendMessage, sendVoice (table), sendPhoto (table), getFile, downloadFile, answerCallbackQuery, retry on 5xx, error response parsing |

### HTTP-level integration tests

All external service tests use `net/http/httptest.NewServer` — no mocking frameworks, no interface substitution for the service layer. Tests make real `http.Client` calls to a local test server that validates request structure and returns realistic JSON payloads.

Example structure:

```go
func TestSendVoice(t *testing.T) {
    cases := []struct{ name string; chatID int64; fileID string; wantMsgID int }{
        {"standard send", 100, "voice-file-id-123", 55},
        {"different chat", 999, "voice-xyz",        56},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            mux := http.NewServeMux()
            mux.HandleFunc("/bottesttoken/sendVoice", func(w http.ResponseWriter, r *http.Request) {
                // validate r.Body, then:
                okJSON(t, w, map[string]any{"message_id": tc.wantMsgID, ...})
            })
            _, client := newTestClient(t, mux) // points client BaseURL at test server
            msgID, err := client.SendVoice(ctx, tc.chatID, tc.fileID)
            // assert msgID, err, request body content
        })
    }
}
```

### What each test validates

**Groq client tests:**
- `TestTranscribeSuccess` — correct endpoint, `Authorization` header, multipart `file` and `model` fields present, response text decoded correctly
- `TestTranscribeRetryOnServerError` — server returns 500 twice then succeeds; verifies exactly 3 attempts
- `TestTranscribeExhaustsRetries` — server always returns 500; verifies error returned after max retries
- `TestTranscribeRespectsContextCancellation` — slow server; context timeout cancels before response
- `TestTranscribeAuthHeader` — Bearer token value is correct

**Telegram client tests:**
- `TestSendMessage` — POST to `/sendMessage`, body contains text, message ID decoded
- `TestSendMessageWithKeyboard` — `reply_markup`/`inline_keyboard` present in body
- `TestSendVoice` — POST to `/sendVoice`, `voice` field contains file ID
- `TestSendPhoto` — POST to `/sendPhoto`, `photo` and `caption` fields present
- `TestGetFile` — POST to `/getFile`, `file_id` in body, `file_path` decoded from response
- `TestDownloadFile` — GET to `/file/bot{token}/{path}`, raw bytes returned
- `TestAnswerCallbackQuery` — `callback_query_id` in body
- `TestRetryOnServerError` — exactly 3 attempts on 500 responses
- `TestTelegramErrorResponse` — `ok: false` response returns descriptive error

---

## 10. Running with real Telegram — step by step

### Prerequisites

- Go 1.22 or later (`go version`)
- A Telegram account
- A Groq account (free tier is sufficient)

---

### Step 1 — Create a Telegram bot

1. Open Telegram and search for **@BotFather** (the official bot).
2. Send `/newbot`.
3. Follow the prompts: choose a display name, then a username ending in `bot` (e.g. `my_voice_bot`).
4. BotFather replies with your token:
   ```
   Done! Congratulations on your new bot. You will find it at t.me/my_voice_bot.
   Use this token to access the HTTP API:
   123456789:AAHdqTcvCH1vGWJxfSeofSs3yqD1IVpITG4
   ```
5. Copy the token — you will need it as `BOT_TOKEN`.

---

### Step 2 — Get a Groq API key

1. Sign up or log in at **console.groq.com**.
2. Go to **API Keys** in the left sidebar.
3. Click **Create API Key**, give it a name, and copy the key.
4. This becomes `GROQ_API_KEY`.

---

### Step 3 — Get the code and install dependencies

```bash
git clone <this-repo-url> go-telegram-template
cd go-telegram-template
go mod download
```

Or if you are already in the directory:

```bash
go mod download
```

---

### Step 4 — Set environment variables

**Option A — export in your shell (simplest for local dev):**

```bash
export BOT_TOKEN="123456789:AAHdqTcvCH1vGWJxfSeofSs3yqD1IVpITG4"
export GROQ_API_KEY="gsk_xxxxxxxxxxxxxxxxxxxxxxxxxxxx"
export LOG_LEVEL="debug"
export POLLING_MODE="long_polling"
```

**Option B — create a `.env` file and source it:**

```bash
# .env
BOT_TOKEN=123456789:AAHdqTcvCH1vGWJxfSeofSs3yqD1IVpITG4
GROQ_API_KEY=gsk_xxxxxxxxxxxxxxxxxxxxxxxxxxxx
LOG_LEVEL=debug
POLLING_MODE=long_polling
```

```bash
source .env
```

> Never commit `.env` to version control. Add it to `.gitignore`.

---

### Step 5 — Run the bot

```bash
go run ./cmd/bot
```

Expected output (JSON log lines):

```json
{"time":"2026-01-01T12:00:00Z","level":"INFO","msg":"starting in long-polling mode","username":"my_voice_bot"}
```

The bot is now polling Telegram for updates. Leave this terminal open.

---

### Step 6 — Test the bot in Telegram

Open Telegram and find your bot by its username (e.g. `@my_voice_bot`).

**Test commands:**
- Send `/start` → bot replies with a welcome message
- Send `/help` → bot replies with available commands
- Send any text → bot echoes it back

**Test voice transcription (the main feature):**
1. Tap the microphone icon in the Telegram chat and record a short voice note (e.g. say "Hello world")
2. Send it
3. The bot replies: `Transcribing…`
4. Seconds later the bot sends the transcribed text with three buttons: **Edit**, **Send as text**, **Send as command**
5. Tap **Edit** → bot asks for a correction; type a new version → bot updates the draft
6. Tap **Send as text** → bot sends the final text as a plain message
7. Tap **Send as command** → bot interprets the text as a `/command` and dispatches it

**Test audio files:**
- Send an MP3 or M4A file → bot acknowledges it and logs the file size

**Test photos:**
- Send a photo → bot acknowledges it

---

### Step 7 — Stop the bot

Press `Ctrl+C` in the terminal. The bot handles `SIGINT` and `SIGTERM` with graceful shutdown via `signal.NotifyContext`.

---

### Step 8 — Run in the background (production-like)

**Using `nohup`:**

```bash
nohup go run ./cmd/bot > bot.log 2>&1 &
echo $! > bot.pid
```

Stop it:

```bash
kill $(cat bot.pid)
```

**Using the compiled binary:**

```bash
go build -o bot ./cmd/bot
nohup ./bot > bot.log 2>&1 &
```

---

### Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `config error: BOT_TOKEN is required` | Env var not set | Re-run `export BOT_TOKEN=...` |
| `failed to create bot: Unauthorized` | Token is wrong or revoked | Re-generate via BotFather (`/revoke`) |
| `transcription failed after 4 attempts` | Invalid `GROQ_API_KEY` or quota exceeded | Check key at console.groq.com |
| Bot does not respond | Bot is not running, or another instance is consuming updates | Check `ps aux | grep bot` |
| `context deadline exceeded` during transcription | Groq is slow; default 45 s timeout hit | Increase `Timing` middleware timeout in `bot.go` |

---

## 11. Docker / docker-compose

### Build and run with Docker

```bash
docker build -t telegram-bot .

docker run -e BOT_TOKEN="..." \
           -e GROQ_API_KEY="..." \
           -e LOG_LEVEL="info" \
           telegram-bot
```

### Local integration environment (no external API calls)

`docker-compose.yml` starts the bot alongside two minimal HTTP mocks (socat-based) that simulate the Telegram API and Groq API with fixed responses. Useful for verifying bot startup without real credentials.

```bash
docker-compose up --build
```

The mock servers:
- `mock-telegram` on `:8081` — returns `{"ok":true,"result":[]}` for every request (empty update list)
- `mock-groq` on `:8082` — returns `{"text":"integration test transcription"}` for every transcription request

---

## 12. Extensibility guide

### Add a new handler

1. Create `internal/handlers/myhandler.go` implementing `Handler`.
2. Register it in `internal/bot/bot.go` with `dispatcher.Register(...)` — before the catch-all `textHandler`.

### Add a new flow

1. Create `internal/flows/myflow.go` with flow/state name constants.
2. Use `flows.Manager` in your handler to get/set/clear state.
3. No changes needed in the dispatcher or engine.

### Replace in-memory storage with Redis

```go
// Implement StateStorage:
type RedisStorage struct{ client *redis.Client }

func (r *RedisStorage) Get(ctx context.Context, userID int64) (*flows.UserState, error) { ... }
func (r *RedisStorage) Set(ctx context.Context, state *flows.UserState) error          { ... }
func (r *RedisStorage) Delete(ctx context.Context, userID int64) error                 { ... }

// In bot.go — one line change:
flowEngine := flows.NewEngine(NewRedisStorage(redisClient))
```

### Replace the Groq transcription service

Implement `handlers.Transcriber`:

```go
type MyTranscriber struct{}
func (t *MyTranscriber) Transcribe(ctx context.Context, data []byte, filename string) (string, error) {
    // call any STT API
}
```

Pass it to `handlers.NewVoiceHandler(tgClient, &MyTranscriber{}, flowEngine, logger)`.

### Add a middleware

```go
func MyMiddleware() middleware.Middleware {
    return func(next middleware.HandlerFunc) middleware.HandlerFunc {
        return func(ctx context.Context, uc models.UpdateContext) error {
            // pre-processing
            err := next(ctx, uc)
            // post-processing
            return err
        }
    }
}

// Register in bot.go:
dispatcher.Use(MyMiddleware())
```

### Change the module name

If you fork this template under your own module path:

```bash
find . -name "*.go" -exec sed -i 's|go-telegram-template|github.com/you/yourbot|g' {} +
# Update go.mod first line manually or:
go mod edit -module github.com/you/yourbot
```

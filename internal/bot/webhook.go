package bot

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"deutsch-helper/pkg/models"
)

// WebhookServer receives Telegram updates via HTTP POST.
type WebhookServer struct {
	dispatcher *Dispatcher
	logger     *slog.Logger
}

func newWebhookServer(dispatcher *Dispatcher, logger *slog.Logger) *WebhookServer {
	return &WebhookServer{dispatcher: dispatcher, logger: logger}
}

// ServeHTTP implements http.Handler; Telegram POSTs updates to this endpoint.
func (ws *WebhookServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var update tgbotapi.Update
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		ws.logger.ErrorContext(r.Context(), "decode webhook update", slog.String("error", err.Error()))
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	uc := models.NewUpdateContext(update)
	if err := ws.dispatcher.Dispatch(r.Context(), uc); err != nil {
		ws.logger.ErrorContext(r.Context(), "dispatch webhook update", slog.Any("error", err))
	}
	w.WriteHeader(http.StatusOK)
}

// runWebhook starts an HTTP server that receives Telegram webhook updates.
// addr is the listen address (e.g. ":8080"). The caller is responsible for
// configuring the webhook URL in Telegram (setWebhook API call).
func (b *Bot) runWebhook(ctx context.Context, addr string) error {
	handler := newWebhookServer(b.dispatcher, b.logger)
	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	b.logger.Info("webhook server starting", slog.String("addr", addr))

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	case err := <-errCh:
		return err
	}
}

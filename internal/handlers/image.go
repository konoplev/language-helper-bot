package handlers

import (
	"context"
	"log/slog"

	"go-telegram-template/pkg/models"
)

// ImageHandler handles photo messages.
type ImageHandler struct {
	tg     TelegramAPI
	logger *slog.Logger
}

func NewImageHandler(tg TelegramAPI, logger *slog.Logger) *ImageHandler {
	return &ImageHandler{tg: tg, logger: logger}
}

func (h *ImageHandler) CanHandle(uc models.UpdateContext) bool {
	return uc.Type == models.UpdateTypeImage
}

func (h *ImageHandler) Handle(ctx context.Context, uc models.UpdateContext) error {
	photos := uc.Update.Message.Photo
	if len(photos) == 0 {
		return nil
	}
	best := photos[len(photos)-1]

	h.logger.InfoContext(ctx, "received photo",
		slog.Int64("user_id", uc.UserID),
		slog.String("file_id", best.FileID),
	)

	_, err := h.tg.SendMessage(ctx, uc.ChatID, "📷 Got your photo! (processing not implemented)")
	return err
}

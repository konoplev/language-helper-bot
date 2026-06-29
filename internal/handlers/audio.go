package handlers

import (
	"context"
	"fmt"
	"log/slog"

	"go-telegram-template/pkg/models"
)

// AudioHandler handles audio file messages (mp3, m4a, etc. — distinct from voice notes).
type AudioHandler struct {
	tg     TelegramAPI
	logger *slog.Logger
}

func NewAudioHandler(tg TelegramAPI, logger *slog.Logger) *AudioHandler {
	return &AudioHandler{tg: tg, logger: logger}
}

func (h *AudioHandler) CanHandle(uc models.UpdateContext) bool {
	return uc.Type == models.UpdateTypeAudio
}

func (h *AudioHandler) Handle(ctx context.Context, uc models.UpdateContext) error {
	audio := uc.Update.Message.Audio

	h.logger.InfoContext(ctx, "received audio file",
		slog.Int64("user_id", uc.UserID),
		slog.String("file_id", audio.FileID),
		slog.Int("duration_sec", audio.Duration),
		slog.String("mime_type", audio.MimeType),
	)

	filePath, err := h.tg.GetFile(ctx, audio.FileID)
	if err != nil {
		return fmt.Errorf("get audio file: %w", err)
	}

	data, err := h.tg.DownloadFile(ctx, filePath)
	if err != nil {
		return fmt.Errorf("download audio file: %w", err)
	}

	h.logger.InfoContext(ctx, "audio downloaded",
		slog.Int64("user_id", uc.UserID),
		slog.Int("bytes", len(data)),
	)

	// Placeholder: extend here with audio processing logic.
	_, err = h.tg.SendMessage(ctx, uc.ChatID, fmt.Sprintf("🎵 Got your audio file (%d bytes). Processing not yet implemented.", len(data)))
	return err
}

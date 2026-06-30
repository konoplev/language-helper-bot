package middleware

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"deutsch-helper/pkg/models"
)

// MessageSender is the minimal interface needed to send a denial message.
type MessageSender interface {
	SendMessage(ctx context.Context, chatID int64, text string) (int, error)
}

// Allowlist rejects updates from users not in the allowed set.
type Allowlist struct {
	allowed map[int64]bool
	tg      MessageSender
	logger  *slog.Logger
}

// NewAllowlist creates an Allowlist from a set of allowed user IDs.
// If logger is nil, slog.Default() is used.
func NewAllowlist(allowedUsers []int64, tg MessageSender, logger *slog.Logger) *Allowlist {
	if logger == nil {
		logger = slog.Default()
	}
	m := make(map[int64]bool, len(allowedUsers))
	for _, id := range allowedUsers {
		m[id] = true
	}
	return &Allowlist{allowed: m, tg: tg, logger: logger}
}

// Middleware returns a Middleware function that gates access by user ID.
func (a *Allowlist) Middleware() Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, uc models.UpdateContext) error {
			if uc.UserID == 0 || !a.allowed[uc.UserID] {
				a.logger.InfoContext(ctx, "rejected unauthorized user",
					slog.Int64("user_id", uc.UserID),
				)
				if uc.ChatID != 0 {
					_, err := a.tg.SendMessage(ctx, uc.ChatID, "Access denied.")
					return err
				}
				return nil
			}
			return next(ctx, uc)
		}
	}
}

// LoadAllowedUsers reads user IDs from a file, one per line. Lines beginning
// with '#' and blank lines are ignored.
func LoadAllowedUsers(filename string) ([]int64, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filename, err)
	}
	defer f.Close()

	var ids []int64
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		id, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s line %d: invalid user ID %q", filename, lineNum, line)
		}
		ids = append(ids, id)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", filename, err)
	}
	return ids, nil
}

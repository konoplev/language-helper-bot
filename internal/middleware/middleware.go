package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"deutsch-helper/pkg/models"
)

type HandlerFunc func(ctx context.Context, uc models.UpdateContext) error

type Middleware func(next HandlerFunc) HandlerFunc

// Chain wraps handler with middlewares applied outermost-first.
func Chain(handler HandlerFunc, mws ...Middleware) HandlerFunc {
	for i := len(mws) - 1; i >= 0; i-- {
		handler = mws[i](handler)
	}
	return handler
}

// Logging logs each update's type, user, duration, and outcome.
func Logging(logger *slog.Logger) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, uc models.UpdateContext) error {
			start := time.Now()
			err := next(ctx, uc)
			level := slog.LevelInfo
			if err != nil {
				level = slog.LevelError
			}
			logger.Log(ctx, level, "update handled",
				slog.String("type", string(uc.Type)),
				slog.Int64("user_id", uc.UserID),
				slog.Duration("duration", time.Since(start)),
				slog.Any("error", err),
			)
			return err
		}
	}
}

// Recovery catches panics and converts them to errors.
func Recovery(logger *slog.Logger) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, uc models.UpdateContext) (err error) {
			defer func() {
				if r := recover(); r != nil {
					logger.ErrorContext(ctx, "panic recovered",
						slog.Any("panic", r),
						slog.Int64("user_id", uc.UserID),
					)
					err = fmt.Errorf("internal error")
				}
			}()
			return next(ctx, uc)
		}
	}
}

// Timing adds a deadline to the context for each update.
func Timing(timeout time.Duration) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, uc models.UpdateContext) error {
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			return next(ctx, uc)
		}
	}
}

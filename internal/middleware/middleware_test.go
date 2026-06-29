package middleware_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"go-telegram-template/internal/middleware"
	"go-telegram-template/pkg/models"
)

func noopUpdate() models.UpdateContext {
	return models.UpdateContext{Type: models.UpdateTypeText, UserID: 1, ChatID: 1}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestChainOrder verifies that middlewares are applied outermost-first and all run.
func TestChainOrder(t *testing.T) {
	cases := []struct {
		name      string
		mwCount   int
		wantOrder []int
	}{
		{"single middleware", 1, []int{1}},
		{"two middlewares", 2, []int{1, 2}},
		{"three middlewares", 3, []int{1, 2, 3}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var order []int
			var mws []middleware.Middleware
			for i := 1; i <= tc.mwCount; i++ {
				n := i
				mws = append(mws, func(next middleware.HandlerFunc) middleware.HandlerFunc {
					return func(ctx context.Context, uc models.UpdateContext) error {
						order = append(order, n)
						return next(ctx, uc)
					}
				})
			}

			noop := func(_ context.Context, _ models.UpdateContext) error { return nil }
			chained := middleware.Chain(noop, mws...)
			if err := chained(context.Background(), noopUpdate()); err != nil {
				t.Fatal(err)
			}

			if len(order) != len(tc.wantOrder) {
				t.Fatalf("order len: want %d, got %d", len(tc.wantOrder), len(order))
			}
			for i, v := range tc.wantOrder {
				if order[i] != v {
					t.Fatalf("order[%d]: want %d, got %d", i, v, order[i])
				}
			}
		})
	}
}

// TestRecoveryMiddleware verifies panic recovery and error passthrough.
func TestRecoveryMiddleware(t *testing.T) {
	cases := []struct {
		name      string
		handler   middleware.HandlerFunc
		wantErr   bool
		wantPanic bool
	}{
		{
			name:    "no panic, no error",
			handler: func(_ context.Context, _ models.UpdateContext) error { return nil },
			wantErr: false,
		},
		{
			name:    "handler returns error",
			handler: func(_ context.Context, _ models.UpdateContext) error { return errors.New("boom") },
			wantErr: true,
		},
		{
			name:      "handler panics",
			handler:   func(_ context.Context, _ models.UpdateContext) error { panic("oh no") },
			wantErr:   true,
			wantPanic: true,
		},
		{
			name:      "handler panics with non-string",
			handler:   func(_ context.Context, _ models.UpdateContext) error { panic(42) },
			wantErr:   true,
			wantPanic: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chained := middleware.Chain(tc.handler, middleware.Recovery(discardLogger()))
			err := chained(context.Background(), noopUpdate())
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestLoggingMiddleware verifies the handler is called and errors are propagated.
func TestLoggingMiddleware(t *testing.T) {
	sentinel := errors.New("logged error")

	cases := []struct {
		name    string
		handler middleware.HandlerFunc
		wantErr error
	}{
		{
			name:    "success",
			handler: func(_ context.Context, _ models.UpdateContext) error { return nil },
			wantErr: nil,
		},
		{
			name:    "error propagated",
			handler: func(_ context.Context, _ models.UpdateContext) error { return sentinel },
			wantErr: sentinel,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chained := middleware.Chain(tc.handler, middleware.Logging(discardLogger()))
			err := chained(context.Background(), noopUpdate())
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error: want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

// TestTimingMiddleware verifies context deadline injection and cancellation behaviour.
func TestTimingMiddleware(t *testing.T) {
	cases := []struct {
		name         string
		timeout      time.Duration
		handlerSleep time.Duration
		wantErrNil   bool // whether ctx.Err() inside the handler should be nil
	}{
		{
			name:         "deadline injected, completes within timeout",
			timeout:      200 * time.Millisecond,
			handlerSleep: 0,
			wantErrNil:   true,
		},
		{
			name:         "exceeds timeout — ctx cancelled inside handler",
			timeout:      10 * time.Millisecond,
			handlerSleep: 60 * time.Millisecond,
			wantErrNil:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handlerCalled := false
			var deadlineSet bool
			var ctxErrAfterSleep error

			handler := func(ctx context.Context, _ models.UpdateContext) error {
				handlerCalled = true
				_, deadlineSet = ctx.Deadline()
				if tc.handlerSleep > 0 {
					time.Sleep(tc.handlerSleep)
				}
				ctxErrAfterSleep = ctx.Err()
				return nil
			}

			chained := middleware.Chain(handler, middleware.Timing(tc.timeout))
			_ = chained(context.Background(), noopUpdate())

			if !handlerCalled {
				t.Fatal("handler was not called")
			}
			if !deadlineSet {
				t.Fatal("Timing middleware did not inject a deadline into ctx")
			}
			if tc.wantErrNil && ctxErrAfterSleep != nil {
				t.Fatalf("ctx.Err() inside handler: want nil, got %v", ctxErrAfterSleep)
			}
			if !tc.wantErrNil && ctxErrAfterSleep == nil {
				t.Fatal("ctx.Err() inside handler: want non-nil (deadline exceeded), got nil")
			}
		})
	}
}

// TestMiddlewareChainWithMultipleMiddlewares is a table-driven integration of Chain + all middleware types.
func TestMiddlewareChainWithMultipleMiddlewares(t *testing.T) {
	cases := []struct {
		name      string
		handler   middleware.HandlerFunc
		wantErr   bool
	}{
		{
			name:    "success path",
			handler: func(_ context.Context, _ models.UpdateContext) error { return nil },
			wantErr: false,
		},
		{
			name:    "error path",
			handler: func(_ context.Context, _ models.UpdateContext) error { return errors.New("err") },
			wantErr: true,
		},
		{
			name:    "panic path recovered",
			handler: func(_ context.Context, _ models.UpdateContext) error { panic("crash") },
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger := discardLogger()
			chained := middleware.Chain(tc.handler,
				middleware.Recovery(logger),
				middleware.Timing(5*time.Second),
				middleware.Logging(logger),
			)
			err := chained(context.Background(), noopUpdate())
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

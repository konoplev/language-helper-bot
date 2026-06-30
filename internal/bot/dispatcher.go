package bot

import (
	"context"
	"fmt"
	"log/slog"

	"deutsch-helper/internal/handlers"
	"deutsch-helper/internal/middleware"
	"deutsch-helper/pkg/models"
)

// Dispatcher routes incoming updates to the first matching Handler.
type Dispatcher struct {
	handlers   []handlers.Handler
	middleware []middleware.Middleware
	logger     *slog.Logger
}

func NewDispatcher(logger *slog.Logger) *Dispatcher {
	return &Dispatcher{logger: logger}
}

// Register appends a handler to the dispatch chain.
func (d *Dispatcher) Register(h handlers.Handler) {
	d.handlers = append(d.handlers, h)
}

// Use appends middleware to the pipeline (applied outermost-first).
func (d *Dispatcher) Use(mws ...middleware.Middleware) {
	d.middleware = append(d.middleware, mws...)
}

// Dispatch finds the first handler that can handle uc and runs it through middleware.
func (d *Dispatcher) Dispatch(ctx context.Context, uc models.UpdateContext) error {
	for _, h := range d.handlers {
		if !h.CanHandle(uc) {
			continue
		}
		chain := middleware.Chain(h.Handle, d.middleware...)
		return chain(ctx, uc)
	}
	d.logger.DebugContext(ctx, "no handler for update",
		slog.String("type", string(uc.Type)),
		slog.Int64("user_id", uc.UserID),
	)
	return nil
}

// DispatchCommand creates a synthetic command update and re-dispatches it.
// Used by CallbackHandler for "send as command".
func (d *Dispatcher) DispatchCommand(ctx context.Context, uc models.UpdateContext) error {
	return d.Dispatch(ctx, uc)
}

// ErrNoHandler is returned when no registered handler matches the update.
var ErrNoHandler = fmt.Errorf("no handler matched update")

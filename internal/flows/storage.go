package flows

import "context"

type StateStorage interface {
	Get(ctx context.Context, userID int64) (*UserState, error)
	Set(ctx context.Context, state *UserState) error
	Delete(ctx context.Context, userID int64) error
}

package flows

import "context"

// Manager is the interface handlers use to interact with flow state.
type Manager interface {
	GetState(ctx context.Context, userID int64) (*UserState, error)
	SetState(ctx context.Context, state *UserState) error
	ClearState(ctx context.Context, userID int64) error
	IsInFlow(ctx context.Context, userID int64, flow FlowName) (bool, error)
	IsInState(ctx context.Context, userID int64, flow FlowName, state StateName) (bool, error)
}

type Engine struct {
	storage StateStorage
}

func NewEngine(storage StateStorage) *Engine {
	return &Engine{storage: storage}
}

func (e *Engine) GetState(ctx context.Context, userID int64) (*UserState, error) {
	return e.storage.Get(ctx, userID)
}

func (e *Engine) SetState(ctx context.Context, state *UserState) error {
	return e.storage.Set(ctx, state)
}

func (e *Engine) ClearState(ctx context.Context, userID int64) error {
	return e.storage.Delete(ctx, userID)
}

func (e *Engine) IsInFlow(ctx context.Context, userID int64, flow FlowName) (bool, error) {
	st, err := e.storage.Get(ctx, userID)
	if err != nil || st == nil {
		return false, err
	}
	return st.Flow == flow, nil
}

func (e *Engine) IsInState(ctx context.Context, userID int64, flow FlowName, state StateName) (bool, error) {
	st, err := e.storage.Get(ctx, userID)
	if err != nil || st == nil {
		return false, err
	}
	return st.Flow == flow && st.State == state, nil
}

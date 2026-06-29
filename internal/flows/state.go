package flows

type FlowName string
type StateName string

type UserState struct {
	UserID  int64
	Flow    FlowName
	State   StateName
	Payload map[string]any
}

func NewUserState(userID int64, flow FlowName, state StateName) *UserState {
	return &UserState{
		UserID:  userID,
		Flow:    flow,
		State:   state,
		Payload: make(map[string]any),
	}
}

package flows

const (
	FlowVoice FlowName = "voice"

	StateVoiceDraft StateName = "draft"
	StateVoiceEdit  StateName = "edit"

	PayloadDraftText  = "draft_text"
	PayloadDraftMsgID = "draft_msg_id"
)

const (
	CallbackVoiceEdit        = "voice:edit"
	CallbackVoiceSendText    = "voice:send_text"
	CallbackVoiceSendCommand = "voice:send_command"
)

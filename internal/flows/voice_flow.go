package flows

const (
	FlowVoice FlowName = "voice"

	// StateVoicePending means voice was transcribed and sent back; waiting for user's final text.
	StateVoicePending StateName = "pending"

	// Kept for backward-compat with engine tests.
	StateVoiceDraft StateName = "draft"
	StateVoiceEdit  StateName = "edit"

	PayloadDraftText  = "draft_text"
	PayloadDraftMsgID = "draft_msg_id"

	PayloadPendingTranscription = "pending_transcription"
	PayloadActiveCommand        = "active_command"
)

const (
	CallbackVoiceEdit     = "voice:edit"
	CallbackVoiceSendText = "voice:send_text"
)

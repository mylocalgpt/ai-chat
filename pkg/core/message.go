package core

import "time"

// Message is a stored chat message, either inbound or outbound.
type Message struct {
	ID           int64
	Channel      string
	ChannelMsgID string
	SenderID     string
	WorkspaceID  *int64 // nullable, pointer for SQL NULL
	Content      string
	Direction    MessageDirection
	Status       MessageStatus
	CreatedAt    time.Time
}

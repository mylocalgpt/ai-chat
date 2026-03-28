package audit

import "time"

// Inbound creates an entry for an incoming message from a channel.
func Inbound(channel, sender, workspace, content string) AuditEntry {
	return AuditEntry{
		Timestamp: time.Now().UTC(),
		Type:      "inbound",
		Channel:   channel,
		Sender:    sender,
		Workspace: workspace,
		Content:   content,
	}
}

// Route creates an entry for a routing decision.
func Route(workspace, action, agent string, confidence float64) AuditEntry {
	return AuditEntry{
		Timestamp:  time.Now().UTC(),
		Type:       "route",
		Workspace:  workspace,
		Action:     action,
		Agent:      agent,
		Confidence: confidence,
	}
}

// AgentSend creates an entry for a message sent to an agent.
func AgentSend(workspace, agent, session, message string) AuditEntry {
	return AuditEntry{
		Timestamp: time.Now().UTC(),
		Type:      "agent_send",
		Workspace: workspace,
		Agent:     agent,
		Session:   session,
		Message:   message,
	}
}

// AgentResponse creates an entry for a response received from an agent.
func AgentResponse(workspace, agent, session string, length int, durationMs int64) AuditEntry {
	return AuditEntry{
		Timestamp: time.Now().UTC(),
		Type:      "agent_response",
		Workspace: workspace,
		Agent:     agent,
		Session:   session,
		Length:    length,
		Duration:  durationMs,
	}
}

// Outbound creates an entry for an outgoing message to a channel.
func Outbound(channel, recipient, workspace string, length int) AuditEntry {
	return AuditEntry{
		Timestamp: time.Now().UTC(),
		Type:      "outbound",
		Channel:   channel,
		Recipient: recipient,
		Workspace: workspace,
		Length:    length,
	}
}

// Error creates an entry for an error event.
func Error(workspace, errMsg string) AuditEntry {
	return AuditEntry{
		Timestamp: time.Now().UTC(),
		Type:      "error",
		Workspace: workspace,
		Error:     errMsg,
	}
}

// Health creates an entry for a health check event.
func Health(workspace, status string) AuditEntry {
	return AuditEntry{
		Timestamp: time.Now().UTC(),
		Type:      "health",
		Workspace: workspace,
		Status:    status,
	}
}

// Unauthorized creates an entry for an unauthorized access attempt.
func Unauthorized(channel, sender string) AuditEntry {
	return AuditEntry{
		Timestamp: time.Now().UTC(),
		Type:      "unauthorized",
		Channel:   channel,
		Sender:    sender,
	}
}

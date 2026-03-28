package router

import (
	"fmt"
	"strings"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

func FormatWorkspaceList(workspaces []core.Workspace, activeID int64) string {
	if len(workspaces) == 0 {
		return "No workspaces configured."
	}

	var b strings.Builder
	for i, w := range workspaces {
		if i > 0 {
			b.WriteString("\n")
		}
		if w.ID == activeID {
			b.WriteString("→ ")
		} else {
			b.WriteString("  ")
		}
		b.WriteString(w.Name)
		b.WriteString(": ")
		b.WriteString(w.Path)
	}
	return b.String()
}

func FormatSessionList(sessions []core.Session, activeID int64, previews map[int64]string) string {
	if len(sessions) == 0 {
		return "No sessions in current workspace."
	}

	var b strings.Builder
	for i, s := range sessions {
		if i > 0 {
			b.WriteString("\n")
		}
		if s.ID == activeID {
			b.WriteString("→ ")
		} else {
			b.WriteString("  ")
		}
		b.WriteString(s.Slug)
		b.WriteString(" [")
		b.WriteString(s.Agent)
		b.WriteString("] ")
		b.WriteString(string(s.Status))
	}
	return b.String()
}

func FormatStatus(info *StatusInfo) string {
	var b strings.Builder
	if info.Workspace != nil {
		b.WriteString("Workspace: ")
		b.WriteString(info.Workspace.Name)
		b.WriteString(" (")
		b.WriteString(info.Workspace.Path)
		b.WriteString(")\n")
	} else {
		b.WriteString("Workspace: none\n")
	}

	b.WriteString("Agent: ")
	if info.Agent != "" {
		b.WriteString(info.Agent)
	} else {
		b.WriteString("none")
	}
	b.WriteString("\n")

	if info.ActiveSession != nil {
		b.WriteString("Session: ")
		b.WriteString(info.ActiveSession.Name)
		b.WriteString("\n")
	}

	b.WriteString("Sessions: ")
	fmt.Fprintf(&b, "%d", info.SessionCount)

	return b.String()
}

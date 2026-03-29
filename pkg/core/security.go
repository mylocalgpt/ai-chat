package core

import "fmt"

type SecurityAction string

const (
	SecurityActionAllow   SecurityAction = "allow"
	SecurityActionBlock   SecurityAction = "block"
	SecurityActionConfirm SecurityAction = "confirm"
)

type SecurityDecision struct {
	Action    SecurityAction
	PendingID string
	Reason    string
}

type SecurityDecisionError struct {
	Decision SecurityDecision
}

func (e *SecurityDecisionError) Error() string {
	if e == nil {
		return "security decision required"
	}
	if e.Decision.Reason != "" {
		return fmt.Sprintf("security decision %s: %s", e.Decision.Action, e.Decision.Reason)
	}
	return fmt.Sprintf("security decision %s", e.Decision.Action)
}

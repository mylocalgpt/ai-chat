package audit

// RunScheduledCheck runs the audit check for the last 24 hours and returns a
// formatted summary, whether any patterns were flagged, and any error. This is
// the hook that the orchestrator calls on a schedule.
func RunScheduledCheck(logDir string) (summary string, flagged bool, err error) {
	result, err := RunAuditCheck(logDir, 1)
	if err != nil {
		return "", false, err
	}

	summary = result.String()
	for _, p := range result.Patterns {
		if p.Flagged {
			flagged = true
			break
		}
	}

	return summary, flagged, nil
}

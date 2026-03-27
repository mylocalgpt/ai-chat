package audit

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeUsageLog(t *testing.T, dir, date string, lines []string) {
	t.Helper()
	content := strings.Join(lines, "\n") + "\n"
	path := filepath.Join(dir, date+".jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
}

func TestUsageSummaryAggregation(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().UTC().Format("2006-01-02")

	lines := []string{
		`{"type":"agent_response","workspace":"lab","agent":"claude","duration_ms":30000}`,
		`{"type":"agent_response","workspace":"lab","agent":"claude","duration_ms":30000}`,
		`{"type":"agent_response","workspace":"lab","agent":"opencode","duration_ms":60000}`,
		`{"type":"inbound","workspace":"lab","channel":"telegram"}`,
		`{"type":"agent_response","workspace":"prod","agent":"claude","duration_ms":10000}`,
	}
	writeUsageLog(t, dir, today, lines)

	u, err := UsageSummary(dir, "lab", 1)
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}

	if u.Requests != 3 {
		t.Errorf("Requests = %d, want 3", u.Requests)
	}

	expectedDuration := 120 * time.Second // 30000 + 30000 + 60000 ms
	if u.TotalDuration != expectedDuration {
		t.Errorf("TotalDuration = %v, want %v", u.TotalDuration, expectedDuration)
	}

	if u.ByAgent["claude"] != 2 {
		t.Errorf("ByAgent[claude] = %d, want 2", u.ByAgent["claude"])
	}
	if u.ByAgent["opencode"] != 1 {
		t.Errorf("ByAgent[opencode] = %d, want 1", u.ByAgent["opencode"])
	}
}

func TestUsageSummaryCostEstimation(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().UTC().Format("2006-01-02")

	// claude with exactly 60000ms = 1 minute = $0.05
	lines := []string{
		`{"type":"agent_response","workspace":"lab","agent":"claude","duration_ms":60000}`,
	}
	writeUsageLog(t, dir, today, lines)

	u, err := UsageSummary(dir, "lab", 1)
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}

	if math.Abs(u.EstimatedCost-0.05) > 0.001 {
		t.Errorf("EstimatedCost = %.4f, want 0.05", u.EstimatedCost)
	}
}

func TestUsageSummaryUnknownAgent(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().UTC().Format("2006-01-02")

	lines := []string{
		`{"type":"agent_response","workspace":"lab","agent":"unknown-agent","duration_ms":120000}`,
	}
	writeUsageLog(t, dir, today, lines)

	u, err := UsageSummary(dir, "lab", 1)
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}

	if u.Requests != 1 {
		t.Errorf("Requests = %d, want 1", u.Requests)
	}
	if u.ByAgent["unknown-agent"] != 1 {
		t.Errorf("ByAgent[unknown-agent] = %d, want 1", u.ByAgent["unknown-agent"])
	}
	if u.EstimatedCost != 0 {
		t.Errorf("EstimatedCost = %.4f, want 0 for unknown agent", u.EstimatedCost)
	}
}

func TestAllUsageSummaries(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().UTC().Format("2006-01-02")

	lines := []string{
		`{"type":"agent_response","workspace":"lab","agent":"claude","duration_ms":10000}`,
		`{"type":"agent_response","workspace":"prod","agent":"claude","duration_ms":20000}`,
		`{"type":"agent_response","workspace":"lab","agent":"opencode","duration_ms":5000}`,
	}
	writeUsageLog(t, dir, today, lines)

	summaries, err := AllUsageSummaries(dir, 1)
	if err != nil {
		t.Fatalf("AllUsageSummaries: %v", err)
	}

	if len(summaries) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(summaries))
	}

	// Sorted alphabetically.
	if summaries[0].Workspace != "lab" {
		t.Errorf("first workspace = %q, want %q", summaries[0].Workspace, "lab")
	}
	if summaries[1].Workspace != "prod" {
		t.Errorf("second workspace = %q, want %q", summaries[1].Workspace, "prod")
	}

	if summaries[0].Requests != 2 {
		t.Errorf("lab requests = %d, want 2", summaries[0].Requests)
	}
	if summaries[1].Requests != 1 {
		t.Errorf("prod requests = %d, want 1", summaries[1].Requests)
	}
}

func TestUsageSummaryEmptyDir(t *testing.T) {
	dir := t.TempDir()

	u, err := UsageSummary(dir, "", 1)
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}
	if u.Requests != 0 {
		t.Errorf("Requests = %d, want 0", u.Requests)
	}

	summaries, err := AllUsageSummaries(dir, 1)
	if err != nil {
		t.Fatalf("AllUsageSummaries: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("expected 0 summaries, got %d", len(summaries))
	}
}

func TestUsageSummaryDateFiltering(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().UTC()
	oldDate := today.AddDate(0, 0, -10)

	// Today's file.
	writeUsageLog(t, dir, today.Format("2006-01-02"), []string{
		`{"type":"agent_response","workspace":"lab","agent":"claude","duration_ms":1000}`,
	})
	// Old file (10 days ago).
	writeUsageLog(t, dir, oldDate.Format("2006-01-02"), []string{
		`{"type":"agent_response","workspace":"lab","agent":"claude","duration_ms":5000}`,
	})

	// Only check last 1 day - should only see today's entry.
	u, err := UsageSummary(dir, "lab", 1)
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}
	if u.Requests != 1 {
		t.Errorf("Requests = %d, want 1 (only today)", u.Requests)
	}

	// Check last 15 days - should see both.
	u2, err := UsageSummary(dir, "lab", 15)
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}
	if u2.Requests != 2 {
		t.Errorf("Requests = %d, want 2 (both days)", u2.Requests)
	}
}

func TestUsageStringFormat(t *testing.T) {
	u := &Usage{
		Workspace:     "lab",
		Requests:      42,
		TotalDuration: 83 * time.Minute,
		EstimatedCost: 3.15,
		ByAgent: map[string]int{
			"claude":   30,
			"opencode": 12,
		},
	}

	output := u.String()
	if !strings.Contains(output, "Workspace: lab") {
		t.Errorf("missing workspace in output:\n%s", output)
	}
	if !strings.Contains(output, "Requests: 42") {
		t.Errorf("missing requests in output:\n%s", output)
	}
	if !strings.Contains(output, "1h23m") {
		t.Errorf("missing duration in output:\n%s", output)
	}
	if !strings.Contains(output, "$3.15") {
		t.Errorf("missing cost in output:\n%s", output)
	}
	if !strings.Contains(output, "claude: 30") {
		t.Errorf("missing agent breakdown in output:\n%s", output)
	}
}

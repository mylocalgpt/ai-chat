package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// Usage holds aggregated usage statistics for a single workspace.
type Usage struct {
	Workspace     string
	Requests      int
	TotalDuration time.Duration
	EstimatedCost float64
	ByAgent       map[string]int // request count per agent
}

// costPerMinute is a rough estimate of cost per minute of agent time.
// These are estimates only, not precision billing.
var costPerMinute = map[string]float64{
	"opencode": 0.02,
	"copilot":  0.01,
}

// usageEntry is a minimal struct for parsing only the fields we need.
type usageEntry struct {
	Type      string `json:"type"`
	Workspace string `json:"workspace"`
	Agent     string `json:"agent"`
	Duration  int64  `json:"duration_ms"`
}

// UsageSummary aggregates audit entries for a specific workspace (or all
// workspaces if workspace is empty) over the last `days` days.
func UsageSummary(logDir string, workspace string, days int) (*Usage, error) {
	files, err := logFilesInRange(logDir, days)
	if err != nil {
		return nil, err
	}

	u := &Usage{
		Workspace: workspace,
		ByAgent:   make(map[string]int),
	}

	agentDuration := make(map[string]int64) // agent -> total ms

	for _, path := range files {
		if err := scanUsageFile(path, workspace, u, agentDuration); err != nil {
			return nil, err
		}
	}

	// Calculate estimated cost from per-agent durations.
	for agent, totalMs := range agentDuration {
		minutes := float64(totalMs) / 60000.0
		if rate, ok := costPerMinute[agent]; ok {
			u.EstimatedCost += rate * minutes
		}
	}

	return u, nil
}

// AllUsageSummaries scans all entries and returns per-workspace summaries.
// Uses a single pass over the files.
func AllUsageSummaries(logDir string, days int) ([]*Usage, error) {
	files, err := logFilesInRange(logDir, days)
	if err != nil {
		return nil, err
	}

	byWorkspace := make(map[string]*Usage)
	agentDuration := make(map[string]map[string]int64) // workspace -> agent -> ms

	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("audit: open %s: %w", path, err)
		}

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var e usageEntry
			if err := json.Unmarshal(line, &e); err != nil {
				continue // skip malformed lines
			}

			if e.Type != "agent_response" {
				continue
			}

			ws := e.Workspace
			if ws == "" {
				ws = "(none)"
			}

			u, ok := byWorkspace[ws]
			if !ok {
				u = &Usage{
					Workspace: ws,
					ByAgent:   make(map[string]int),
				}
				byWorkspace[ws] = u
				agentDuration[ws] = make(map[string]int64)
			}

			u.Requests++
			u.TotalDuration += time.Duration(e.Duration) * time.Millisecond
			if e.Agent != "" {
				u.ByAgent[e.Agent]++
				agentDuration[ws][e.Agent] += e.Duration
			}
		}

		_ = f.Close()
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("audit: scan %s: %w", path, err)
		}
	}

	// Calculate costs.
	for ws, u := range byWorkspace {
		for agent, totalMs := range agentDuration[ws] {
			minutes := float64(totalMs) / 60000.0
			if rate, ok := costPerMinute[agent]; ok {
				u.EstimatedCost += rate * minutes
			}
		}
	}

	// Sort by workspace name for stable output.
	var result []*Usage
	for _, u := range byWorkspace {
		result = append(result, u)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Workspace < result[j].Workspace
	})

	return result, nil
}

// String formats a Usage for terminal output.
func (u *Usage) String() string {
	var b strings.Builder

	ws := u.Workspace
	if ws == "" {
		ws = "(all)"
	}

	fmt.Fprintf(&b, "Workspace: %s\n", ws)
	fmt.Fprintf(&b, "  Requests: %d\n", u.Requests)
	fmt.Fprintf(&b, "  Total duration: %s\n", formatDuration(u.TotalDuration))
	fmt.Fprintf(&b, "  Estimated cost: $%.2f\n", u.EstimatedCost)

	if len(u.ByAgent) > 0 {
		b.WriteString("  By agent:\n")
		// Sort agents for stable output.
		agents := make([]string, 0, len(u.ByAgent))
		for a := range u.ByAgent {
			agents = append(agents, a)
		}
		sort.Strings(agents)
		for _, a := range agents {
			fmt.Fprintf(&b, "    %s: %d\n", a, u.ByAgent[a])
		}
	}

	return b.String()
}

// formatDuration formats a duration as "1h23m" or "5m" or "0s".
func formatDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}

	h := int(d.Hours())
	m := int(d.Minutes()) % 60

	if h > 0 && m > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	if h > 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dm", m)
}

// scanUsageFile reads a single log file and accumulates usage data.
func scanUsageFile(path string, workspace string, u *Usage, agentDuration map[string]int64) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("audit: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var e usageEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue // skip malformed lines
		}

		if e.Type != "agent_response" {
			continue
		}

		// Filter by workspace if specified.
		if workspace != "" && e.Workspace != workspace {
			continue
		}

		u.Requests++
		u.TotalDuration += time.Duration(e.Duration) * time.Millisecond
		if e.Agent != "" {
			u.ByAgent[e.Agent]++
			agentDuration[e.Agent] += e.Duration
		}
	}

	return scanner.Err()
}

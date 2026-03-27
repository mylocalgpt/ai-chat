package audit

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CheckResult holds the results of an audit check across log files.
type CheckResult struct {
	Patterns     []PatternResult
	TimeRange    string // e.g. "last 24h"
	TotalEntries int
}

// PatternResult holds the match result for a single anomaly pattern.
type PatternResult struct {
	Name      string
	Pattern   string
	Count     int
	Threshold int
	Flagged   bool
	Details   []string // first few matching lines for flagged patterns
}

// anomalyPattern defines a pattern to search for in audit logs.
type anomalyPattern struct {
	Name      string
	Pattern   string
	Threshold int
}

var defaultPatterns = []anomalyPattern{
	{Name: "errors", Pattern: `"type":"error"`, Threshold: 5},
	{Name: "rate_limits", Pattern: `rate.limit`, Threshold: 3},
	{Name: "crashes", Pattern: `"status":"crashed"`, Threshold: 1},
	{Name: "failed_routes", Pattern: `"confidence":0\.[0-4]`, Threshold: 10},
	{Name: "unauthorized", Pattern: `"type":"unauthorized"`, Threshold: 0},
}

// RunAuditCheck scans log files from the last `days` days and checks for
// anomaly patterns. It tries rg first, then falls back to pure Go.
func RunAuditCheck(logDir string, days int) (*CheckResult, error) {
	return runAuditCheckInternal(logDir, days, false)
}

// runAuditCheckInternal is the shared implementation. When forceGoFallback is
// true, it skips rg even if available (used in tests).
func runAuditCheckInternal(logDir string, days int, forceGoFallback bool) (*CheckResult, error) {
	files, err := logFilesInRange(logDir, days)
	if err != nil {
		return nil, err
	}

	timeLabel := "last 24h"
	if days != 1 {
		timeLabel = fmt.Sprintf("last %dd", days)
	}

	result := &CheckResult{
		TimeRange: timeLabel,
	}

	// Count total entries across files.
	for _, f := range files {
		n, err := countLines(f)
		if err != nil {
			return nil, fmt.Errorf("audit: count lines in %s: %w", f, err)
		}
		result.TotalEntries += n
	}

	useRg := false
	if !forceGoFallback {
		if _, err := exec.LookPath("rg"); err == nil {
			useRg = true
		}
	}

	for _, p := range defaultPatterns {
		pr := PatternResult{
			Name:      p.Name,
			Pattern:   p.Pattern,
			Threshold: p.Threshold,
		}

		if len(files) == 0 {
			result.Patterns = append(result.Patterns, pr)
			continue
		}

		if useRg {
			pr.Count, pr.Details, err = matchWithRg(p.Pattern, files, p.Threshold)
		} else {
			pr.Count, pr.Details, err = matchWithGo(p.Pattern, files, p.Threshold)
		}
		if err != nil {
			return nil, fmt.Errorf("audit: check pattern %s: %w", p.Name, err)
		}

		pr.Flagged = pr.Count > pr.Threshold
		result.Patterns = append(result.Patterns, pr)
	}

	return result, nil
}

// String formats the check result for terminal output.
func (r *CheckResult) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "ai-chat audit check (%s):\n", r.TimeRange)
	for _, p := range r.Patterns {
		status := "OK"
		if p.Flagged {
			status = "!"
		}
		line := fmt.Sprintf("  %s: %d (%s)", p.Name, p.Count, status)
		if p.Flagged && len(p.Details) > 0 {
			line += " - " + p.Details[0]
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// logFilesInRange returns paths to log files within the last `days` days.
func logFilesInRange(logDir string, days int) ([]string, error) {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("audit: read log dir: %w", err)
	}

	cutoff := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -days)
	var files []string

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		stem := strings.TrimSuffix(name, ".jsonl")
		fileDate, err := time.Parse("2006-01-02", stem)
		if err != nil {
			continue
		}
		if !fileDate.Before(cutoff) {
			files = append(files, logDir+"/"+name)
		}
	}

	return files, nil
}

// countLines counts non-empty lines in a file.
func countLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if len(scanner.Bytes()) > 0 {
			count++
		}
	}
	return count, scanner.Err()
}

// matchWithRg uses ripgrep to count matches and extract details.
func matchWithRg(pattern string, files []string, threshold int) (int, []string, error) {
	// Count matches.
	args := append([]string{"-c", "--no-filename", pattern}, files...)
	cmd := exec.Command("rg", args...)
	out, err := cmd.Output()
	if err != nil {
		// rg exits 1 when no matches found.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return 0, nil, nil
		}
		return 0, nil, err
	}

	total := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil {
			continue
		}
		total += n
	}

	// Get details if flagged.
	var details []string
	if total > threshold {
		detailArgs := append([]string{"--no-filename", "-m", "3", pattern}, files...)
		cmd := exec.Command("rg", detailArgs...)
		out, err := cmd.Output()
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			for i, l := range lines {
				if i >= 3 {
					break
				}
				if l != "" {
					details = append(details, strings.TrimSpace(l))
				}
			}
		}
	}

	return total, details, nil
}

// matchWithGo uses pure Go regex matching as a fallback when rg is unavailable.
func matchWithGo(pattern string, files []string, threshold int) (int, []string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return 0, nil, fmt.Errorf("compile pattern %q: %w", pattern, err)
	}

	total := 0
	var details []string

	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			return 0, nil, err
		}

		scanner := bufio.NewScanner(f)
		// Increase buffer for potentially long lines.
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			if re.Match(line) {
				total++
				if len(details) < 3 {
					details = append(details, string(bytes.TrimSpace(line)))
				}
			}
		}

		f.Close()
		if err := scanner.Err(); err != nil {
			return total, details, err
		}
	}

	// Only return details if flagged.
	if total <= threshold {
		details = nil
	}

	return total, details, nil
}

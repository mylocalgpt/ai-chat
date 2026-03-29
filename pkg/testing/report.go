package testing

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type ScenarioResult struct {
	Name     string
	Passed   bool
	Duration time.Duration
	Error    string
}

type TestReport struct {
	Results []ScenarioResult
	Started time.Time
	Ended   time.Time
	Verbose bool
	output  io.Writer
}

func NewTestReport(verbose bool) *TestReport {
	return &TestReport{
		Results: []ScenarioResult{},
		Started: time.Now(),
		Verbose: verbose,
		output:  os.Stdout,
	}
}

func (r *TestReport) Add(name string, passed bool, dur time.Duration, err string) {
	r.Results = append(r.Results, ScenarioResult{
		Name:     name,
		Passed:   passed,
		Duration: dur,
		Error:    err,
	})
}

func (r *TestReport) End() {
	r.Ended = time.Now()
}

func (r *TestReport) Print() {
	_, _ = fmt.Fprintln(r.output, "ai-chat test results:")

	for _, res := range r.Results {
		status := "PASS"
		if !res.Passed {
			status = "FAIL"
		}
		_, _ = fmt.Fprintf(r.output, "  %-18s %s (%.1fs)\n", res.Name+":", status, res.Duration.Seconds())

		if r.Verbose && res.Error != "" {
			_, _ = fmt.Fprintf(r.output, "    Error: %s\n", res.Error)
		}
	}

	_, _ = fmt.Fprintln(r.output)
	_, _ = fmt.Fprintf(r.output, "%s\n", r.Summary())
}

func (r *TestReport) Summary() string {
	passed := 0
	for _, res := range r.Results {
		if res.Passed {
			passed++
		}
	}

	total := r.Ended.Sub(r.Started)
	return fmt.Sprintf("%d/%d passed (%.1fs total)", passed, len(r.Results), total.Seconds())
}

func (r *TestReport) ExitCode() int {
	for _, res := range r.Results {
		if !res.Passed {
			return 1
		}
	}
	return 0
}

func (r *TestReport) Failed() []ScenarioResult {
	var failed []ScenarioResult
	for _, res := range r.Results {
		if !res.Passed {
			failed = append(failed, res)
		}
	}
	return failed
}

func (r *TestReport) String() string {
	var b strings.Builder
	b.WriteString("ai-chat test results:\n")

	for _, res := range r.Results {
		status := "PASS"
		if !res.Passed {
			status = "FAIL"
		}
		fmt.Fprintf(&b, "  %-18s %s (%.1fs)\n", res.Name+":", status, res.Duration.Seconds())
	}

	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", r.Summary())
	return b.String()
}

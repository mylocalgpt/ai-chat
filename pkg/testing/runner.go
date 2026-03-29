package testing

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type TestRunner struct {
	Verbose  bool
	Scenario string
}

func NewTestRunner(verbose bool, scenario string) *TestRunner {
	return &TestRunner{
		Verbose:  verbose,
		Scenario: scenario,
	}
}

func (r *TestRunner) Run() *TestReport {
	report := NewTestReport(r.Verbose)

	scenarios := DefaultScenarios()
	if r.Scenario != "" {
		scenarios = nil
		for _, s := range Scenarios {
			if strings.EqualFold(s.Name, r.Scenario) {
				scenarios = []Scenario{s}
				break
			}
		}
		if len(scenarios) == 0 {
			report.Add(r.Scenario, false, 0, fmt.Sprintf("unknown scenario: %s", r.Scenario))
			report.End()
			return report
		}
	}

	for _, s := range scenarios {
		start := time.Now()
		err := r.runScenario(s)
		dur := time.Since(start)

		errStr := ""
		passed := err == nil
		if err != nil {
			errStr = err.Error()
		}

		report.Add(s.Name, passed, dur, errStr)
	}

	report.End()
	return report
}

func (r *TestRunner) runScenario(s Scenario) error {
	harness, cleanup, err := newHarness(HarnessConfig{})
	if err != nil {
		return fmt.Errorf("creating harness: %w", err)
	}
	defer cleanup()

	var scenarioErr error
	t := &scenarioT{verbose: r.Verbose}

	defer func() {
		if rec := recover(); rec != nil {
			if errStr, ok := rec.(string); ok && errStr == "test failed" {
			} else {
				scenarioErr = fmt.Errorf("panic: %v", rec)
			}
		}
	}()

	s.Run(t, harness)
	if t.failed {
		scenarioErr = fmt.Errorf("%s", t.errorMsg)
	}

	return scenarioErr
}

type scenarioT struct {
	failed   bool
	errorMsg string
	verbose  bool
}

func (t *scenarioT) Fatalf(format string, args ...any) {
	t.failed = true
	t.errorMsg = fmt.Sprintf(format, args...)
	panic("test failed")
}

func (t *scenarioT) Errorf(format string, args ...any) {
	t.failed = true
	t.errorMsg = fmt.Sprintf(format, args...)
}

func (t *scenarioT) Error(args ...any) {
	t.failed = true
	t.errorMsg = fmt.Sprint(args...)
}

func (t *scenarioT) Fatal(args ...any) {
	t.failed = true
	t.errorMsg = fmt.Sprint(args...)
	panic("test failed")
}

func (t *scenarioT) Fail() {
	t.failed = true
}

func (t *scenarioT) FailNow() {
	t.failed = true
	panic("test failed")
}

func (t *scenarioT) Failed() bool {
	return t.failed
}

func (t *scenarioT) Log(args ...any) {
	if t.verbose {
		fmt.Println(args...)
	}
}

func (t *scenarioT) Logf(format string, args ...any) {
	if t.verbose {
		fmt.Printf(format+"\n", args...)
	}
}

func (t *scenarioT) TempDir() string {
	dir, err := os.MkdirTemp("", "ai-chat-test-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	return dir
}

func (t *scenarioT) Run(name string, f func(T)) bool {
	if t.failed {
		return false
	}

	subT := &scenarioT{verbose: t.verbose}
	defer func() {
		if rec := recover(); rec != nil {
			if rec != "test failed" {
				t.failed = true
				t.errorMsg = fmt.Sprintf("panic in subtest %s: %v", name, rec)
			}
		}
	}()

	f(subT)
	if subT.failed {
		t.failed = true
		t.errorMsg = subT.errorMsg
		return false
	}
	return true
}

type T interface {
	Fatalf(format string, args ...any)
	Fatal(args ...any)
	Errorf(format string, args ...any)
	Error(args ...any)
	Fail()
	FailNow()
	Failed() bool
	Log(args ...any)
	Logf(format string, args ...any)
	TempDir() string
	Run(name string, f func(T)) bool
}

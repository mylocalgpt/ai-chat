package executor

import "errors"

var (
	ErrSpawnTimeout    = errors.New("agent spawn timed out waiting for prompt")
	ErrResponseTimeout = errors.New("agent response timed out")
)

func findDivergence(snapshot, current []string) int {
	n := min(len(snapshot), len(current))
	for i := range n {
		if snapshot[i] != current[i] {
			return i
		}
	}
	return n
}

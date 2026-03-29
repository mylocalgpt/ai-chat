package testing

import (
	"testing"
)

func TestScenariosWithTestingT(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Cleanup()

	for _, s := range Scenarios {
		t.Run(s.Name, func(t *testing.T) {
			s.Run(WrapTestingT(t), h)
		})
	}
}

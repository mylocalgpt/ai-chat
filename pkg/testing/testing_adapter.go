package testing

import (
	"testing"
)

type testingT struct {
	*testing.T
}

func WrapTestingT(t *testing.T) T {
	return &testingT{T: t}
}

func (t *testingT) Run(name string, f func(T)) bool {
	return t.T.Run(name, func(tt *testing.T) {
		f(WrapTestingT(tt))
	})
}

package router

import (
	"strings"
)

type Command struct {
	Name string
	Args []string
	Raw  string
}

func Parse(text string) (*Command, bool) {
	if text == "" || text[0] != '/' {
		return nil, false
	}

	if len(text) == 1 {
		return nil, false
	}

	rest := text[1:]

	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return nil, false
	}

	name := parts[0]
	if idx := strings.Index(name, "@"); idx > 0 {
		name = name[:idx]
	}

	return &Command{
		Name: name,
		Args: parts[1:],
		Raw:  text,
	}, true
}

package testing

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
)

type MockResponse struct {
	Content string
	Delay   time.Duration
}

type MockAdapter struct {
	name      string
	responses map[string]MockResponse
	fallback  MockResponse
}

func NewMockAdapter(name string) *MockAdapter {
	m := &MockAdapter{
		name:      name,
		responses: make(map[string]MockResponse),
		fallback:  MockResponse{Content: "", Delay: 0},
	}
	m.addDefaultResponses()
	return m
}

func (m *MockAdapter) addDefaultResponses() {
	m.responses["ping"] = MockResponse{Content: "pong", Delay: 0}
	m.responses["long"] = MockResponse{Content: strings.Repeat("Lorem ipsum dolor sit amet, consectetur adipiscing elit. ", 125), Delay: 0}
	m.responses["markdown"] = MockResponse{
		Content: `# Header

This is **bold** and *italic* text.

## Subheader

- Item 1
- Item 2
- Item 3

` + "```python" + `
def hello():
    print("Hello, World!")
` + "```" + `

> A blockquote

[Link](https://example.com)
`,
		Delay: 0,
	}
	m.responses["code"] = MockResponse{
		Content: `Here are some code examples:

` + "```python" + `
def fibonacci(n):
    if n <= 1:
        return n
    return fibonacci(n-1) + fibonacci(n-2)
` + "```" + `

` + "```javascript" + `
const fetch = async (url) => {
    const response = await fetch(url);
    return response.json();
};
` + "```" + `

` + "```go" + `
func main() {
    fmt.Println("Hello, World!")
}
` + "```" + `
`,
		Delay: 0,
	}
	m.responses["slow"] = MockResponse{Content: "delayed response", Delay: 2 * time.Second}
}

func (m *MockAdapter) AddResponse(input string, resp MockResponse) {
	m.responses[input] = resp
}

func (m *MockAdapter) Name() string {
	return m.name
}

func (m *MockAdapter) Spawn(ctx context.Context, session core.SessionInfo) error {
	if session.ResponseFile != "" {
		_, err := executor.NewResponseFile(filepath.Dir(session.ResponseFile), session)
		return err
	}
	return nil
}

func (m *MockAdapter) Send(ctx context.Context, session core.SessionInfo, message string) error {
	resp, ok := m.responses[message]
	if !ok {
		resp = MockResponse{Content: "Echo: " + message, Delay: 0}
	}

	if resp.Delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(resp.Delay):
		}
	}

	agentMsg := executor.ResponseMessage{
		Role:      "agent",
		Content:   resp.Content,
		Timestamp: time.Now().UTC(),
	}
	if err := executor.AppendMessage(session.ResponseFile, agentMsg); err != nil {
		return fmt.Errorf("appending agent message: %w", err)
	}

	return nil
}

func (m *MockAdapter) IsAlive(session core.SessionInfo) bool {
	return true
}

func (m *MockAdapter) Stop(ctx context.Context, session core.SessionInfo) error {
	return nil
}

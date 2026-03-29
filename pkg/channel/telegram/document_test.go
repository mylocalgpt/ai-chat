package telegram

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func TestTruncateCaption(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "ascii under limit",
			input:  "short caption",
			maxLen: 1024,
			want:   "short caption",
		},
		{
			name:   "empty string",
			input:  "",
			maxLen: 1024,
			want:   "",
		},
		{
			name:   "ascii at exact limit",
			input:  strings.Repeat("a", 1024),
			maxLen: 1024,
			want:   strings.Repeat("a", 1024),
		},
		{
			name:   "ascii over limit",
			input:  strings.Repeat("a", 1030),
			maxLen: 1024,
			want:   strings.Repeat("a", 1021) + "...",
		},
		{
			name:   "unicode under limit",
			input:  "Hello, world!",
			maxLen: 1024,
			want:   "Hello, world!",
		},
		{
			name:   "unicode over limit truncates at rune boundary",
			input:  strings.Repeat("\u00e9", 1030), // e-acute, 2 bytes per rune
			maxLen: 1024,
			want:   strings.Repeat("\u00e9", 1021) + "...",
		},
		{
			name:   "cjk characters truncated correctly",
			input:  strings.Repeat("\u4e16", 20), // 20 CJK chars
			maxLen: 10,
			want:   strings.Repeat("\u4e16", 7) + "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateCaption(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateCaption(%d runes, max %d) = %d runes, want %d runes",
					len([]rune(tt.input)), tt.maxLen, len([]rune(got)), len([]rune(tt.want)))
			}
		})
	}
}

func TestDocumentFilename(t *testing.T) {
	tests := []struct {
		session string
		idx     int
		want    string
	}{
		{"ai-chat-lab-a3f2", 1, "response-ai-chat-lab-a3f2-1.md"},
		{"eager-canyon", 0, "response-eager-canyon-0.md"},
		{"x", 99, "response-x-99.md"},
	}

	for _, tt := range tests {
		got := documentFilename(tt.session, tt.idx)
		if got != tt.want {
			t.Errorf("documentFilename(%q, %d) = %q, want %q", tt.session, tt.idx, got, tt.want)
		}
	}
}

// mockDocumentBot records SendDocument calls for verification.
type mockDocumentBot struct {
	sentDocuments []*bot.SendDocumentParams
	// Store the data read from the uploaded file for verification.
	uploadedData []string
	sendErr      error
}

func (b *mockDocumentBot) GetMe(context.Context) (*models.User, error) {
	return &models.User{ID: 1, IsBot: true}, nil
}
func (b *mockDocumentBot) Start(context.Context) {}
func (b *mockDocumentBot) SendMessage(_ context.Context, params *bot.SendMessageParams) (*models.Message, error) {
	return &models.Message{ID: 1, Chat: models.Chat{ID: params.ChatID.(int64)}}, nil
}
func (b *mockDocumentBot) SendChatAction(context.Context, *bot.SendChatActionParams) (bool, error) {
	return true, nil
}
func (b *mockDocumentBot) SetMyCommands(context.Context, *bot.SetMyCommandsParams) (bool, error) {
	return true, nil
}
func (b *mockDocumentBot) DeleteMessage(context.Context, *bot.DeleteMessageParams) (bool, error) {
	return true, nil
}
func (b *mockDocumentBot) EditMessageText(_ context.Context, params *bot.EditMessageTextParams) (*models.Message, error) {
	return &models.Message{ID: params.MessageID, Chat: models.Chat{ID: params.ChatID.(int64)}}, nil
}
func (b *mockDocumentBot) SendDocument(_ context.Context, params *bot.SendDocumentParams) (*models.Message, error) {
	if b.sendErr != nil {
		return nil, b.sendErr
	}
	b.sentDocuments = append(b.sentDocuments, params)

	// Read the uploaded data from the io.Reader for later verification.
	if upload, ok := params.Document.(*models.InputFileUpload); ok && upload.Data != nil {
		data, err := io.ReadAll(upload.Data)
		if err == nil {
			b.uploadedData = append(b.uploadedData, string(data))
		}
	}

	return &models.Message{ID: 1, Chat: models.Chat{ID: params.ChatID.(int64)}}, nil
}

func TestSendDocumentAttachment(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mb := &mockDocumentBot{}
		content := "# Full Response\n\nThis is a long response that was too big for a message."
		filename := "response-eager-canyon-1.md"
		caption := "Full response output"

		err := SendDocumentAttachment(context.Background(), mb, 123, content, filename, caption)
		if err != nil {
			t.Fatalf("SendDocumentAttachment() error: %v", err)
		}

		if len(mb.sentDocuments) != 1 {
			t.Fatalf("expected 1 SendDocument call, got %d", len(mb.sentDocuments))
		}

		params := mb.sentDocuments[0]
		if params.ChatID != int64(123) {
			t.Errorf("ChatID = %v, want 123", params.ChatID)
		}

		upload, ok := params.Document.(*models.InputFileUpload)
		if !ok {
			t.Fatal("Document is not *models.InputFileUpload")
		}
		if upload.Filename != filename {
			t.Errorf("Filename = %q, want %q", upload.Filename, filename)
		}

		if params.Caption != caption {
			t.Errorf("Caption = %q, want %q", params.Caption, caption)
		}
		if params.ParseMode != models.ParseModeHTML {
			t.Errorf("ParseMode = %v, want HTML", params.ParseMode)
		}

		// Verify uploaded data matches input content.
		if len(mb.uploadedData) != 1 {
			t.Fatalf("expected 1 uploaded data entry, got %d", len(mb.uploadedData))
		}
		if mb.uploadedData[0] != content {
			t.Errorf("uploaded data = %q, want %q", mb.uploadedData[0], content)
		}
	})

	t.Run("caption truncated to 1024", func(t *testing.T) {
		mb := &mockDocumentBot{}
		longCaption := strings.Repeat("x", 1030)

		err := SendDocumentAttachment(context.Background(), mb, 456, "content", "file.md", longCaption)
		if err != nil {
			t.Fatalf("SendDocumentAttachment() error: %v", err)
		}

		if len(mb.sentDocuments) != 1 {
			t.Fatalf("expected 1 SendDocument call, got %d", len(mb.sentDocuments))
		}

		caption := mb.sentDocuments[0].Caption
		if len([]rune(caption)) > 1024 {
			t.Errorf("caption length = %d runes, want <= 1024", len([]rune(caption)))
		}
		if !strings.HasSuffix(caption, "...") {
			t.Errorf("truncated caption should end with '...', got suffix %q", caption[len(caption)-3:])
		}
	})

	t.Run("temp file cleaned up", func(t *testing.T) {
		mb := &mockDocumentBot{}

		// Count temp files before.
		beforeFiles := countTempFiles(t)

		err := SendDocumentAttachment(context.Background(), mb, 789, "cleanup test", "file.md", "cap")
		if err != nil {
			t.Fatalf("SendDocumentAttachment() error: %v", err)
		}

		// Count temp files after - should not have grown.
		afterFiles := countTempFiles(t)
		if afterFiles > beforeFiles {
			t.Errorf("temp files grew from %d to %d; cleanup may have failed", beforeFiles, afterFiles)
		}
	})

	t.Run("send error propagated", func(t *testing.T) {
		mb := &mockDocumentBot{sendErr: io.ErrUnexpectedEOF}

		err := SendDocumentAttachment(context.Background(), mb, 123, "content", "file.md", "cap")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "sending document") {
			t.Errorf("error = %q, want it to contain 'sending document'", err.Error())
		}
	})
}

// countTempFiles counts files in the OS temp dir matching the ai-chat pattern.
func countTempFiles(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatalf("reading temp dir: %v", err)
	}
	count := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "ai-chat-") && strings.HasSuffix(e.Name(), ".md") {
			count++
		}
	}
	return count
}

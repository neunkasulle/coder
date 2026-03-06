package chatd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputerUseSentinel_Value(t *testing.T) {
	t.Parallel()

	// Ensure the sentinel string hasn't been accidentally changed.
	// Other components (e.g. runChat) rely on this exact value to
	// detect computer-use subagents.
	assert.Equal(t, "[COMPUTER_USE_AGENT]", computerUseSentinel)
}

func TestComputerUseSentinel_Detection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "PrefixedInSystemPrompt",
			content: "[COMPUTER_USE_AGENT]\n\nUse the desktop to open Firefox",
			want:    true,
		},
		{
			name:    "AbsentFromNormalPrompt",
			content: "Just a normal system prompt",
			want:    false,
		},
		{
			name:    "EmbeddedInText",
			content: "The agent has [COMPUTER_USE_AGENT] capabilities",
			want:    true,
		},
		{
			name:    "EmptyString",
			content: "",
			want:    false,
		},
		{
			name:    "SentinelOnly",
			content: "[COMPUTER_USE_AGENT]",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := strings.Contains(tt.content, computerUseSentinel)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSubagentFallbackChatTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "EmptyPrompt",
			input: "",
			want:  "New Chat",
		},
		{
			name:  "ShortPrompt",
			input: "Open Firefox",
			want:  "Open Firefox",
		},
		{
			name:  "LongPrompt",
			input: "Please open the Firefox browser and navigate to the settings page",
			want:  "Please open the Firefox browser and...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := subagentFallbackChatTitle(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

package chatd_test

import (
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/coderd/chatd"
	"github.com/coder/coder/v2/coderd/database/dbtestutil"
	"github.com/coder/coder/v2/testutil"
)

func TestSpawnComputerUseAgent_CreatesChildWithSentinel(t *testing.T) {
	t.Parallel()

	db, ps := dbtestutil.NewDB(t)
	server := newTestServer(t, db, ps, uuid.New())
	ctx := testutil.Context(t, testutil.WaitLong)
	user, model := seedChatDependencies(ctx, t, db)

	// Create a parent chat.
	parent, err := server.CreateChat(ctx, chatd.CreateOptions{
		OwnerID:            user.ID,
		Title:              "parent",
		ModelConfigID:      model.ID,
		InitialUserContent: []fantasy.Content{fantasy.TextContent{Text: "hello"}},
	})
	require.NoError(t, err)

	// Simulate what spawn_computer_use_agent does: build the
	// system prompt with the sentinel and create a child chat.
	prompt := "Use the desktop to open Firefox"
	systemPrompt := "[COMPUTER_USE_AGENT]" + "\n\n" + prompt

	child, err := server.CreateChat(ctx, chatd.CreateOptions{
		OwnerID: parent.OwnerID,
		ParentChatID: uuid.NullUUID{
			UUID:  parent.ID,
			Valid: true,
		},
		RootChatID: uuid.NullUUID{
			UUID:  parent.ID,
			Valid: true,
		},
		ModelConfigID:      model.ID,
		Title:              "computer-use",
		SystemPrompt:       systemPrompt,
		InitialUserContent: []fantasy.Content{fantasy.TextContent{Text: prompt}},
	})
	require.NoError(t, err)

	// Verify parent-child relationship.
	require.True(t, child.ParentChatID.Valid)
	require.Equal(t, parent.ID, child.ParentChatID.UUID)

	// Use GetChatMessagesForPromptByChatID because system
	// messages have visibility=model and the regular
	// GetChatMessagesByChatID filters those out.
	messages, err := db.GetChatMessagesForPromptByChatID(ctx, child.ID)
	require.NoError(t, err)

	foundSentinel := false
	for _, msg := range messages {
		if msg.Role != "system" {
			continue
		}
		if msg.Content.Valid && strings.Contains(string(msg.Content.RawMessage), "[COMPUTER_USE_AGENT]") {
			foundSentinel = true
			break
		}
	}
	require.True(t, foundSentinel,
		"child chat system message must contain the computer use sentinel")
}

func TestSpawnComputerUseAgent_SystemPromptFormat(t *testing.T) {
	t.Parallel()

	db, ps := dbtestutil.NewDB(t)
	server := newTestServer(t, db, ps, uuid.New())
	ctx := testutil.Context(t, testutil.WaitLong)
	user, model := seedChatDependencies(ctx, t, db)

	parent, err := server.CreateChat(ctx, chatd.CreateOptions{
		OwnerID:            user.ID,
		Title:              "parent",
		ModelConfigID:      model.ID,
		InitialUserContent: []fantasy.Content{fantasy.TextContent{Text: "hello"}},
	})
	require.NoError(t, err)

	prompt := "Navigate to settings page"
	systemPrompt := "[COMPUTER_USE_AGENT]" + "\n\n" + prompt

	child, err := server.CreateChat(ctx, chatd.CreateOptions{
		OwnerID: parent.OwnerID,
		ParentChatID: uuid.NullUUID{
			UUID:  parent.ID,
			Valid: true,
		},
		RootChatID: uuid.NullUUID{
			UUID:  parent.ID,
			Valid: true,
		},
		ModelConfigID:      model.ID,
		Title:              "computer-use-format",
		SystemPrompt:       systemPrompt,
		InitialUserContent: []fantasy.Content{fantasy.TextContent{Text: prompt}},
	})
	require.NoError(t, err)

	messages, err := db.GetChatMessagesForPromptByChatID(ctx, child.ID)
	require.NoError(t, err)

	// The system message raw content is a JSON-encoded string.
	// It should contain the sentinel followed by the user prompt.
	var rawSystemContent string
	for _, msg := range messages {
		if msg.Role != "system" {
			continue
		}
		if msg.Content.Valid {
			rawSystemContent = string(msg.Content.RawMessage)
			break
		}
	}

	assert.Contains(t, rawSystemContent, "[COMPUTER_USE_AGENT]",
		"system prompt raw content should contain sentinel")
	assert.Contains(t, rawSystemContent, prompt,
		"system prompt raw content should contain the user prompt")
}

func TestSpawnComputerUseAgent_ChildIsListedUnderParent(t *testing.T) {
	t.Parallel()

	db, ps := dbtestutil.NewDB(t)
	server := newTestServer(t, db, ps, uuid.New())
	ctx := testutil.Context(t, testutil.WaitLong)
	user, model := seedChatDependencies(ctx, t, db)

	parent, err := server.CreateChat(ctx, chatd.CreateOptions{
		OwnerID:            user.ID,
		Title:              "parent",
		ModelConfigID:      model.ID,
		InitialUserContent: []fantasy.Content{fantasy.TextContent{Text: "hello"}},
	})
	require.NoError(t, err)

	prompt := "Check the UI layout"
	systemPrompt := "[COMPUTER_USE_AGENT]" + "\n\n" + prompt

	child, err := server.CreateChat(ctx, chatd.CreateOptions{
		OwnerID: parent.OwnerID,
		ParentChatID: uuid.NullUUID{
			UUID:  parent.ID,
			Valid: true,
		},
		RootChatID: uuid.NullUUID{
			UUID:  parent.ID,
			Valid: true,
		},
		ModelConfigID:      model.ID,
		Title:              "computer-use-child",
		SystemPrompt:       systemPrompt,
		InitialUserContent: []fantasy.Content{fantasy.TextContent{Text: prompt}},
	})
	require.NoError(t, err)

	// The child should appear in the parent's children list.
	children, err := db.ListChildChatsByParentID(ctx, parent.ID)
	require.NoError(t, err)
	require.Len(t, children, 1)
	assert.Equal(t, child.ID, children[0].ID)
}

func TestSpawnComputerUseAgent_RootChatIDPropagation(t *testing.T) {
	t.Parallel()

	db, ps := dbtestutil.NewDB(t)
	server := newTestServer(t, db, ps, uuid.New())
	ctx := testutil.Context(t, testutil.WaitLong)
	user, model := seedChatDependencies(ctx, t, db)

	// Create a root parent chat (no parent of its own).
	parent, err := server.CreateChat(ctx, chatd.CreateOptions{
		OwnerID:            user.ID,
		Title:              "root-parent",
		ModelConfigID:      model.ID,
		InitialUserContent: []fantasy.Content{fantasy.TextContent{Text: "hello"}},
	})
	require.NoError(t, err)

	prompt := "Take a screenshot"
	systemPrompt := "[COMPUTER_USE_AGENT]" + "\n\n" + prompt

	child, err := server.CreateChat(ctx, chatd.CreateOptions{
		OwnerID: parent.OwnerID,
		ParentChatID: uuid.NullUUID{
			UUID:  parent.ID,
			Valid: true,
		},
		RootChatID: uuid.NullUUID{
			UUID:  parent.ID,
			Valid: true,
		},
		ModelConfigID:      model.ID,
		Title:              "computer-use-root-test",
		SystemPrompt:       systemPrompt,
		InitialUserContent: []fantasy.Content{fantasy.TextContent{Text: prompt}},
	})
	require.NoError(t, err)

	// When the parent has no RootChatID, the child's RootChatID
	// should point to the parent.
	require.True(t, child.RootChatID.Valid)
	assert.Equal(t, parent.ID, child.RootChatID.UUID)

	// Verify chat was retrieved correctly from the DB.
	got, err := db.GetChatByID(ctx, child.ID)
	require.NoError(t, err)
	assert.True(t, got.RootChatID.Valid)
	assert.Equal(t, parent.ID, got.RootChatID.UUID)
}

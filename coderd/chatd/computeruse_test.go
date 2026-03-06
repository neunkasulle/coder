package chatd

import (
	"context"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/xerrors"

	"github.com/coder/coder/v2/codersdk/workspacesdk"
	"github.com/coder/coder/v2/codersdk/workspacesdk/agentconnmock"
)

func TestComputeScaledScreenshotSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		width, height int
		wantW, wantH  int
	}{
		{
			name:   "1920x1080_scales_down",
			width:  1920,
			height: 1080,
			wantW:  1429,
			wantH:  804,
		},
		{
			name:   "1280x800_no_scaling",
			width:  1280,
			height: 800,
			wantW:  1280,
			wantH:  800,
		},
		{
			name:   "3840x2160_large_display",
			width:  3840,
			height: 2160,
			wantW:  1429,
			wantH:  804,
		},
		{
			name:   "1568x1000_pixel_cap_applies",
			width:  1568,
			height: 1000,
			wantW:  1342,
			wantH:  856,
		},
		{
			name:   "100x100_small_display",
			width:  100,
			height: 100,
			wantW:  100,
			wantH:  100,
		},
		{
			name:   "4000x3000_stays_within_limits",
			width:  4000,
			height: 3000,
			// Both constraints apply. The function should keep
			// the result within maxLongEdge=1568 and
			// totalPixels<=1,150,000.
			wantW: 1238,
			wantH: 928,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotW, gotH := computeScaledScreenshotSize(tt.width, tt.height)
			assert.Equal(t, tt.wantW, gotW)
			assert.Equal(t, tt.wantH, gotH)

			// Invariant: results must respect Anthropic constraints.
			const maxLongEdge = 1568
			const maxTotalPixels = 1_150_000
			longEdge := max(gotW, gotH)
			assert.LessOrEqual(t, longEdge, maxLongEdge,
				"long edge %d exceeds max %d", longEdge, maxLongEdge)
			assert.LessOrEqual(t, gotW*gotH, maxTotalPixels,
				"total pixels %d exceeds max %d", gotW*gotH, maxTotalPixels)
		})
	}
}

func TestComputerUseTool_Info(t *testing.T) {
	t.Parallel()

	tool := NewComputerUseTool(1920, 1080, nil)
	info := tool.Info()
	assert.Equal(t, "computer", info.Name)
	assert.NotEmpty(t, info.Description)
}

func TestComputerUseTool_ToolDefinition(t *testing.T) {
	t.Parallel()

	tool := NewComputerUseTool(1920, 1080, nil)
	definer, ok := tool.(interface{ ToolDefinition() fantasy.Tool })
	require.True(t, ok, "computerUseTool must implement ToolDefiner")

	def := definer.ToolDefinition()
	pdt, ok := def.(fantasy.ProviderDefinedTool)
	require.True(t, ok, "ToolDefinition should return a ProviderDefinedTool")
	assert.Contains(t, pdt.ID, "computer")
	assert.Equal(t, "computer", pdt.Name)
	// Verify display dimensions are passed through.
	assert.Equal(t, int64(1920), pdt.Args["display_width_px"])
	assert.Equal(t, int64(1080), pdt.Args["display_height_px"])
}

func TestComputerUseTool_Run_Screenshot(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockConn := agentconnmock.NewMockAgentConn(ctrl)

	mockConn.EXPECT().Screenshot(
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
	).Return(workspacesdk.ScreenshotResponse{
		Data:   "base64png",
		Width:  1024,
		Height: 768,
	}, nil)

	tool := NewComputerUseTool(1920, 1080, func(_ context.Context) (workspacesdk.AgentConn, error) {
		return mockConn, nil
	})

	call := fantasy.ToolCall{
		ID:    "test-1",
		Name:  "computer",
		Input: `{"action":"screenshot"}`,
	}

	resp, err := tool.Run(context.Background(), call)
	require.NoError(t, err)
	assert.Equal(t, "image", resp.Type)
	assert.Equal(t, "image/png", resp.MediaType)
	assert.Equal(t, []byte("base64png"), resp.Data)
	assert.False(t, resp.IsError)
}

func TestComputerUseTool_Run_LeftClick(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockConn := agentconnmock.NewMockAgentConn(ctrl)

	// Expect the action call first.
	mockConn.EXPECT().ComputerAction(
		gomock.Any(),
		gomock.Any(),
	).Return(workspacesdk.ComputerActionResponse{
		Output: "left_click performed",
	}, nil)

	// Then expect a screenshot (auto-screenshot after action).
	mockConn.EXPECT().Screenshot(
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
	).Return(workspacesdk.ScreenshotResponse{
		Data:   "after-click",
		Width:  1024,
		Height: 768,
	}, nil)

	tool := NewComputerUseTool(1920, 1080, func(_ context.Context) (workspacesdk.AgentConn, error) {
		return mockConn, nil
	})

	call := fantasy.ToolCall{
		ID:    "test-2",
		Name:  "computer",
		Input: `{"action":"left_click","coordinate":[100,200]}`,
	}

	resp, err := tool.Run(context.Background(), call)
	require.NoError(t, err)
	assert.Equal(t, "image", resp.Type)
	assert.Equal(t, []byte("after-click"), resp.Data)
}

func TestComputerUseTool_Run_Wait(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockConn := agentconnmock.NewMockAgentConn(ctrl)
	// No expectations on mockConn — wait should not call
	// Screenshot or ComputerAction.

	tool := NewComputerUseTool(1920, 1080, func(_ context.Context) (workspacesdk.AgentConn, error) {
		return mockConn, nil
	})

	call := fantasy.ToolCall{
		ID:    "test-3",
		Name:  "computer",
		Input: `{"action":"wait","duration":10}`,
	}

	resp, err := tool.Run(context.Background(), call)
	require.NoError(t, err)
	assert.Equal(t, "text", resp.Type)
	assert.Contains(t, resp.Content, "waited")
	assert.False(t, resp.IsError)
}

func TestComputerUseTool_Run_ConnError(t *testing.T) {
	t.Parallel()

	tool := NewComputerUseTool(1920, 1080, func(_ context.Context) (workspacesdk.AgentConn, error) {
		return nil, xerrors.New("workspace not available")
	})

	call := fantasy.ToolCall{
		ID:    "test-4",
		Name:  "computer",
		Input: `{"action":"screenshot"}`,
	}

	resp, err := tool.Run(context.Background(), call)
	require.NoError(t, err)
	assert.True(t, resp.IsError)
	assert.Contains(t, resp.Content, "workspace not available")
}

func TestComputerUseTool_Run_InvalidInput(t *testing.T) {
	t.Parallel()

	tool := NewComputerUseTool(1920, 1080, func(_ context.Context) (workspacesdk.AgentConn, error) {
		return nil, xerrors.New("should not be called")
	})

	call := fantasy.ToolCall{
		ID:    "test-5",
		Name:  "computer",
		Input: `{invalid json`,
	}

	resp, err := tool.Run(context.Background(), call)
	require.NoError(t, err)
	assert.True(t, resp.IsError)
	assert.Contains(t, resp.Content, "invalid computer use input")
}

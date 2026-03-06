package chatd

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"charm.land/fantasy"
	fantasyanthropic "charm.land/fantasy/providers/anthropic"

	"github.com/coder/coder/v2/codersdk/workspacesdk"
)

const (
	// ComputerUseModelProvider is the provider for the computer
	// use model.
	ComputerUseModelProvider = "anthropic"
	// ComputerUseModelName is the model used for computer use
	// subagents.
	ComputerUseModelName = "claude-opus-4-6"
)

// computerUseTool implements fantasy.AgentTool and
// chatloop.ToolDefiner for Anthropic computer use.
type computerUseTool struct {
	displayWidth     int
	displayHeight    int
	getWorkspaceConn func(ctx context.Context) (workspacesdk.AgentConn, error)
	providerOptions  fantasy.ProviderOptions
}

// NewComputerUseTool creates a computer use AgentTool that
// delegates to the agent's desktop endpoints.
func NewComputerUseTool(
	displayWidth, displayHeight int,
	getWorkspaceConn func(ctx context.Context) (workspacesdk.AgentConn, error),
) fantasy.AgentTool {
	return &computerUseTool{
		displayWidth:     displayWidth,
		displayHeight:    displayHeight,
		getWorkspaceConn: getWorkspaceConn,
	}
}

func (t *computerUseTool) Info() fantasy.ToolInfo {
	return fantasy.ToolInfo{
		Name:        "computer",
		Description: "Control the desktop: take screenshots, move the mouse, click, type, and scroll.",
		Parameters:  map[string]any{},
		Required:    []string{},
	}
}

func (t *computerUseTool) ToolDefinition() fantasy.Tool {
	return fantasyanthropic.NewComputerUseTool(
		fantasyanthropic.ComputerUseToolOptions{
			DisplayWidthPx:  int64(t.displayWidth),
			DisplayHeightPx: int64(t.displayHeight),
			ToolVersion:     fantasyanthropic.ComputerUse20251124,
		},
	)
}

func (t *computerUseTool) ProviderOptions() fantasy.ProviderOptions {
	return t.providerOptions
}

func (t *computerUseTool) SetProviderOptions(opts fantasy.ProviderOptions) {
	t.providerOptions = opts
}

func (t *computerUseTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	// Parse the tool input from Anthropic's computer use format.
	var input struct {
		Action          string `json:"action"`
		Coordinate      []int  `json:"coordinate,omitempty"`
		StartCoordinate []int  `json:"start_coordinate,omitempty"`
		Text            string `json:"text,omitempty"`
		Duration        int    `json:"duration,omitempty"`
		Scroll          int    `json:"scroll_amount,omitempty"`
		ScrollDir       string `json:"scroll_direction,omitempty"`
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return fantasy.NewTextErrorResponse(
			fmt.Sprintf("invalid computer use input: %v", err),
		), nil
	}

	conn, err := t.getWorkspaceConn(ctx)
	if err != nil {
		return fantasy.NewTextErrorResponse(
			fmt.Sprintf("failed to connect to workspace: %v", err),
		), nil
	}

	// For wait actions, just sleep and return text.
	if input.Action == "wait" {
		d := input.Duration
		if d <= 0 {
			d = 1000
		}
		//nolint:gocritic // time.Sleep is intentional for wait actions.
		time.Sleep(time.Duration(d) * time.Millisecond)
		return fantasy.NewTextResponse(
			fmt.Sprintf("waited %dms", d),
		), nil
	}

	// Compute scaled screenshot size for Anthropic constraints.
	scaledW, scaledH := computeScaledScreenshotSize(
		t.displayWidth, t.displayHeight,
	)

	// For screenshot action, just take a screenshot.
	if input.Action == "screenshot" {
		return t.takeScreenshot(ctx, conn, scaledW, scaledH)
	}

	// Build the action request.
	action := workspacesdk.ComputerAction{
		Action:       input.Action,
		ScaledWidth:  &scaledW,
		ScaledHeight: &scaledH,
	}
	if len(input.Coordinate) == 2 {
		coord := [2]int{input.Coordinate[0], input.Coordinate[1]}
		action.Coordinate = &coord
	}
	if len(input.StartCoordinate) == 2 {
		coord := [2]int{input.StartCoordinate[0], input.StartCoordinate[1]}
		action.StartCoordinate = &coord
	}
	if input.Text != "" {
		action.Text = &input.Text
	}
	if input.Duration > 0 {
		action.Duration = &input.Duration
	}
	if input.Scroll > 0 {
		action.ScrollAmount = &input.Scroll
	}
	if input.ScrollDir != "" {
		action.ScrollDirection = &input.ScrollDir
	}

	// Execute the action.
	_, err = conn.ComputerAction(ctx, action)
	if err != nil {
		return fantasy.NewTextErrorResponse(
			fmt.Sprintf("action %q failed: %v", input.Action, err),
		), nil
	}

	// Take a screenshot after every action (Anthropic pattern).
	return t.takeScreenshot(ctx, conn, scaledW, scaledH)
}

func (t *computerUseTool) takeScreenshot(
	ctx context.Context,
	conn workspacesdk.AgentConn,
	targetWidth, targetHeight int,
) (fantasy.ToolResponse, error) {
	result, err := conn.Screenshot(ctx, targetWidth, targetHeight)
	if err != nil {
		return fantasy.NewTextErrorResponse(
			fmt.Sprintf("screenshot failed: %v", err),
		), nil
	}

	return fantasy.NewImageResponse(
		[]byte(result.Data), "image/png",
	), nil
}

// computeScaledScreenshotSize computes the target screenshot
// dimensions to fit within Anthropic's constraints.
func computeScaledScreenshotSize(width, height int) (int, int) {
	const maxLongEdge = 1568
	const maxTotalPixels = 1_150_000

	longEdge := max(width, height)
	totalPixels := width * height
	longEdgeScale := float64(maxLongEdge) / float64(longEdge)
	totalPixelsScale := math.Sqrt(
		float64(maxTotalPixels) / float64(totalPixels),
	)
	scale := min(1.0, longEdgeScale, totalPixelsScale)

	if scale >= 1.0 {
		return width, height
	}
	return max(1, int(float64(width)*scale)),
		max(1, int(float64(height)*scale))
}

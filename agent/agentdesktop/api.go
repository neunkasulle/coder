package agentdesktop

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"cdr.dev/slog/v3"
	"github.com/coder/coder/v2/agent/agentssh"
	"github.com/coder/coder/v2/coderd/httpapi"
	"github.com/coder/coder/v2/codersdk"
	"github.com/coder/websocket"
)

// ComputerAction is the request body for the desktop action endpoint.
type ComputerAction struct {
	Action          string  `json:"action"`
	Coordinate      *[2]int `json:"coordinate,omitempty"`
	StartCoordinate *[2]int `json:"start_coordinate,omitempty"`
	Text            *string `json:"text,omitempty"`
	Duration        *int    `json:"duration,omitempty"`
	ScrollAmount    *int    `json:"scroll_amount,omitempty"`
	ScrollDirection *string `json:"scroll_direction,omitempty"`
	// ScaledWidth and ScaledHeight are the coordinate space the
	// model is using. When provided, coordinates are linearly
	// mapped from scaled → native before dispatching.
	ScaledWidth  *int `json:"scaled_width,omitempty"`
	ScaledHeight *int `json:"scaled_height,omitempty"`
}

// ComputerActionResponse is the response from the desktop action
// endpoint.
type ComputerActionResponse struct {
	Output string            `json:"output,omitempty"`
	Image  *ScreenshotResult `json:"image,omitempty"`
}

// API exposes the desktop streaming HTTP routes for the agent.
type API struct {
	logger  slog.Logger
	desktop Desktop
}

// NewAPI creates a new desktop streaming API.
func NewAPI(logger slog.Logger, desktop Desktop) *API {
	return &API{
		logger:  logger,
		desktop: desktop,
	}
}

// Routes returns the chi router for mounting at /api/v0/desktop.
func (a *API) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", a.handleDesktop)
	r.Get("/screenshot", a.handleScreenshot)
	r.Post("/action", a.handleAction)
	return r
}

func (a *API) handleDesktop(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Start the desktop session (idempotent).
	_, err := a.desktop.Start(ctx)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
			Message: "Failed to start desktop session.",
			Detail:  err.Error(),
		})
		return
	}

	// Get a VNC connection.
	vncConn, err := a.desktop.VNCConn(ctx)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
			Message: "Failed to connect to VNC server.",
			Detail:  err.Error(),
		})
		return
	}
	defer vncConn.Close()

	// Accept WebSocket from coderd.
	conn, err := websocket.Accept(rw, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		a.logger.Error(ctx, "failed to accept websocket", slog.Error(err))
		return
	}

	// No read limit — RFB framebuffer updates can be large.
	conn.SetReadLimit(-1)

	wsCtx, wsNetConn := codersdk.WebsocketNetConn(ctx, conn, websocket.MessageBinary)
	defer wsNetConn.Close()

	// Bicopy raw bytes between WebSocket and VNC TCP.
	agentssh.Bicopy(wsCtx, wsNetConn, vncConn)
}

func (a *API) handleScreenshot(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Ensure the desktop is running.
	_, err := a.desktop.Start(ctx)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
			Message: "Failed to start desktop session.",
			Detail:  err.Error(),
		})
		return
	}

	// Parse optional query params.
	var opts ScreenshotOptions
	if w := r.URL.Query().Get("target_width"); w != "" {
		if v, err := strconv.Atoi(w); err == nil {
			opts.TargetWidth = v
		}
	}
	if h := r.URL.Query().Get("target_height"); h != "" {
		if v, err := strconv.Atoi(h); err == nil {
			opts.TargetHeight = v
		}
	}

	result, err := a.desktop.Screenshot(ctx, opts)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
			Message: "Failed to capture screenshot.",
			Detail:  err.Error(),
		})
		return
	}

	httpapi.Write(ctx, rw, http.StatusOK, result)
}

func (a *API) handleAction(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Ensure the desktop is running and grab native dimensions.
	cfg, err := a.desktop.Start(ctx)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
			Message: "Failed to start desktop session.",
			Detail:  err.Error(),
		})
		return
	}

	var action ComputerAction
	if err := json.NewDecoder(r.Body).Decode(&action); err != nil {
		httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
			Message: "Failed to decode request body.",
			Detail:  err.Error(),
		})
		return
	}

	// Helper to scale a coordinate pair from the model's space to
	// native display pixels.
	scaleXY := func(x, y int) (int, int) {
		if action.ScaledWidth != nil && *action.ScaledWidth > 0 {
			x = scaleCoordinate(x, *action.ScaledWidth, cfg.Width)
		}
		if action.ScaledHeight != nil && *action.ScaledHeight > 0 {
			y = scaleCoordinate(y, *action.ScaledHeight, cfg.Height)
		}
		return x, y
	}

	var resp ComputerActionResponse

	switch action.Action {
	case "key":
		if action.Text == nil {
			httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
				Message: "Missing \"text\" for key action.",
			})
			return
		}
		if err := a.desktop.KeyPress(ctx, *action.Text); err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Key press failed.",
				Detail:  err.Error(),
			})
			return
		}
		resp.Output = "key action performed"

	case "type":
		if action.Text == nil {
			httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
				Message: "Missing \"text\" for type action.",
			})
			return
		}
		if err := a.desktop.Type(ctx, *action.Text); err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Type action failed.",
				Detail:  err.Error(),
			})
			return
		}
		resp.Output = "type action performed"

	case "cursor_position":
		x, y, err := a.desktop.CursorPosition(ctx)
		if err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Cursor position failed.",
				Detail:  err.Error(),
			})
			return
		}
		resp.Output = "x=" + strconv.Itoa(x) + ",y=" + strconv.Itoa(y)

	case "mouse_move":
		x, y, err := coordFromAction(action)
		if err != nil {
			httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
				Message: err.Error(),
			})
			return
		}
		x, y = scaleXY(x, y)
		if err := a.desktop.Move(ctx, x, y); err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Mouse move failed.",
				Detail:  err.Error(),
			})
			return
		}
		resp.Output = "mouse_move action performed"

	case "left_click":
		x, y, err := coordFromAction(action)
		if err != nil {
			httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
				Message: err.Error(),
			})
			return
		}
		x, y = scaleXY(x, y)
		if err := a.desktop.Move(ctx, x, y); err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Mouse move failed.",
				Detail:  err.Error(),
			})
			return
		}
		if err := a.desktop.Click(ctx, x, y, MouseButtonLeft); err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Left click failed.",
				Detail:  err.Error(),
			})
			return
		}
		resp.Output = "left_click action performed"

	case "left_click_drag":
		if action.Coordinate == nil || action.StartCoordinate == nil {
			httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
				Message: "Missing \"coordinate\" or \"start_coordinate\" for left_click_drag.",
			})
			return
		}
		sx, sy := scaleXY(action.StartCoordinate[0], action.StartCoordinate[1])
		ex, ey := scaleXY(action.Coordinate[0], action.Coordinate[1])
		if err := a.desktop.Drag(ctx, sx, sy, ex, ey); err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Left click drag failed.",
				Detail:  err.Error(),
			})
			return
		}
		resp.Output = "left_click_drag action performed"

	case "left_mouse_down":
		if err := a.desktop.ButtonDown(ctx, MouseButtonLeft); err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Left mouse down failed.",
				Detail:  err.Error(),
			})
			return
		}
		resp.Output = "left_mouse_down action performed"

	case "left_mouse_up":
		if err := a.desktop.ButtonUp(ctx, MouseButtonLeft); err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Left mouse up failed.",
				Detail:  err.Error(),
			})
			return
		}
		resp.Output = "left_mouse_up action performed"

	case "right_click":
		x, y, err := coordFromAction(action)
		if err != nil {
			httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
				Message: err.Error(),
			})
			return
		}
		x, y = scaleXY(x, y)
		if err := a.desktop.Move(ctx, x, y); err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Mouse move failed.",
				Detail:  err.Error(),
			})
			return
		}
		if err := a.desktop.Click(ctx, x, y, MouseButtonRight); err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Right click failed.",
				Detail:  err.Error(),
			})
			return
		}
		resp.Output = "right_click action performed"

	case "middle_click":
		x, y, err := coordFromAction(action)
		if err != nil {
			httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
				Message: err.Error(),
			})
			return
		}
		x, y = scaleXY(x, y)
		if err := a.desktop.Move(ctx, x, y); err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Mouse move failed.",
				Detail:  err.Error(),
			})
			return
		}
		if err := a.desktop.Click(ctx, x, y, MouseButtonMiddle); err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Middle click failed.",
				Detail:  err.Error(),
			})
			return
		}
		resp.Output = "middle_click action performed"

	case "double_click":
		x, y, err := coordFromAction(action)
		if err != nil {
			httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
				Message: err.Error(),
			})
			return
		}
		x, y = scaleXY(x, y)
		if err := a.desktop.Move(ctx, x, y); err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Mouse move failed.",
				Detail:  err.Error(),
			})
			return
		}
		if err := a.desktop.DoubleClick(ctx, x, y, MouseButtonLeft); err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Double click failed.",
				Detail:  err.Error(),
			})
			return
		}
		resp.Output = "double_click action performed"

	case "triple_click":
		x, y, err := coordFromAction(action)
		if err != nil {
			httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
				Message: err.Error(),
			})
			return
		}
		x, y = scaleXY(x, y)
		if err := a.desktop.Move(ctx, x, y); err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Mouse move failed.",
				Detail:  err.Error(),
			})
			return
		}
		for range 3 {
			if err := a.desktop.Click(ctx, x, y, MouseButtonLeft); err != nil {
				httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
					Message: "Triple click failed.",
					Detail:  err.Error(),
				})
				return
			}
		}
		resp.Output = "triple_click action performed"

	case "scroll":
		x, y, err := coordFromAction(action)
		if err != nil {
			httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
				Message: err.Error(),
			})
			return
		}
		x, y = scaleXY(x, y)

		amount := 3
		if action.ScrollAmount != nil {
			amount = *action.ScrollAmount
		}
		direction := "down"
		if action.ScrollDirection != nil {
			direction = *action.ScrollDirection
		}

		var dx, dy int
		switch direction {
		case "up":
			dy = -amount
		case "down":
			dy = amount
		case "left":
			dx = -amount
		case "right":
			dx = amount
		default:
			httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
				Message: "Invalid scroll direction: " + direction,
			})
			return
		}

		if err := a.desktop.Move(ctx, x, y); err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Mouse move failed.",
				Detail:  err.Error(),
			})
			return
		}
		if err := a.desktop.Scroll(ctx, x, y, dx, dy); err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Scroll failed.",
				Detail:  err.Error(),
			})
			return
		}
		resp.Output = "scroll action performed"

	case "screenshot":
		var opts ScreenshotOptions
		result, err := a.desktop.Screenshot(ctx, opts)
		if err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Screenshot failed.",
				Detail:  err.Error(),
			})
			return
		}
		resp.Image = &result

	case "wait":
		dur := 1000
		if action.Duration != nil {
			dur = *action.Duration
		}
		//nolint:gocritic // wait action intentionally sleeps.
		time.Sleep(time.Duration(dur) * time.Millisecond)
		resp.Output = "wait action performed"

	default:
		httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
			Message: "Unknown action: " + action.Action,
		})
		return
	}

	httpapi.Write(ctx, rw, http.StatusOK, resp)
}

// Close shuts down the desktop session if one is running.
func (a *API) Close() error {
	return a.desktop.Close()
}

// coordFromAction extracts the coordinate pair from a ComputerAction,
// returning an error if the coordinate field is missing.
func coordFromAction(action ComputerAction) (x, y int, err error) {
	if action.Coordinate == nil {
		return 0, 0, &missingFieldError{field: "coordinate", action: action.Action}
	}
	return action.Coordinate[0], action.Coordinate[1], nil
}

// missingFieldError is returned when a required field is absent from
// a ComputerAction.
type missingFieldError struct {
	field  string
	action string
}

func (e *missingFieldError) Error() string {
	return "Missing \"" + e.field + "\" for " + e.action + " action."
}

// scaleCoordinate maps a coordinate from scaled → native space.
func scaleCoordinate(scaled, scaledDim, nativeDim int) int {
	if scaledDim == 0 || scaledDim == nativeDim {
		return scaled
	}
	native := (float64(scaled)+0.5)*float64(nativeDim)/float64(scaledDim) - 0.5
	// Clamp to valid range.
	native = math.Max(native, 0)
	native = math.Min(native, float64(nativeDim-1))
	return int(native)
}

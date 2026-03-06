package agentdesktop_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	"cdr.dev/slog/v3/sloggers/slogtest"
	"github.com/coder/coder/v2/agent/agentdesktop"
	"github.com/coder/coder/v2/codersdk"
)

// Ensure fakeDesktop satisfies the Desktop interface at compile time.
var _ agentdesktop.Desktop = (*fakeDesktop)(nil)

// fakeDesktop is a minimal Desktop implementation for unit tests.
type fakeDesktop struct {
	startErr      error
	startCfg      agentdesktop.DisplayConfig
	vncConnErr    error
	screenshotErr error
	screenshotRes agentdesktop.ScreenshotResult
	closed        bool

	// Track calls for assertions.
	lastMove   [2]int
	lastClick  [3]int // x, y, button
	lastScroll [4]int // x, y, dx, dy
	lastKey    string
	lastTyped  string
}

func (f *fakeDesktop) Start(context.Context) (agentdesktop.DisplayConfig, error) {
	return f.startCfg, f.startErr
}

func (f *fakeDesktop) VNCConn(context.Context) (net.Conn, error) {
	return nil, f.vncConnErr
}

func (f *fakeDesktop) Screenshot(_ context.Context, _ agentdesktop.ScreenshotOptions) (agentdesktop.ScreenshotResult, error) {
	return f.screenshotRes, f.screenshotErr
}

func (f *fakeDesktop) Move(_ context.Context, x, y int) error {
	f.lastMove = [2]int{x, y}
	return nil
}

func (f *fakeDesktop) Click(_ context.Context, x, y int, _ agentdesktop.MouseButton) error {
	f.lastClick = [3]int{x, y, 1}
	return nil
}

func (f *fakeDesktop) DoubleClick(_ context.Context, x, y int, _ agentdesktop.MouseButton) error {
	f.lastClick = [3]int{x, y, 2}
	return nil
}

func (f *fakeDesktop) ButtonDown(context.Context, agentdesktop.MouseButton) error { return nil }
func (f *fakeDesktop) ButtonUp(context.Context, agentdesktop.MouseButton) error   { return nil }

func (f *fakeDesktop) Scroll(_ context.Context, x, y, dx, dy int) error {
	f.lastScroll = [4]int{x, y, dx, dy}
	return nil
}

func (f *fakeDesktop) Drag(context.Context, int, int, int, int) error { return nil }

func (f *fakeDesktop) KeyPress(_ context.Context, key string) error {
	f.lastKey = key
	return nil
}

func (f *fakeDesktop) KeyDown(context.Context, string) error { return nil }
func (f *fakeDesktop) KeyUp(context.Context, string) error   { return nil }

func (f *fakeDesktop) Type(_ context.Context, text string) error {
	f.lastTyped = text
	return nil
}

func (f *fakeDesktop) CursorPosition(context.Context) (int, int, error) {
	return 10, 20, nil
}

func (f *fakeDesktop) Close() error {
	f.closed = true
	return nil
}

func TestHandleDesktop_StartError(t *testing.T) {
	t.Parallel()

	logger := slogtest.Make(t, nil)
	fake := &fakeDesktop{startErr: xerrors.New("no desktop")}
	api := agentdesktop.NewAPI(logger, fake)
	defer api.Close()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	handler := api.Routes()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)

	var resp codersdk.Response
	err := json.NewDecoder(rr.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "Failed to start desktop session.", resp.Message)
}

func TestHandleScreenshot_Success(t *testing.T) {
	t.Parallel()

	logger := slogtest.Make(t, nil)
	fake := &fakeDesktop{
		startCfg:      agentdesktop.DisplayConfig{Width: 1024, Height: 768},
		screenshotRes: agentdesktop.ScreenshotResult{Width: 1024, Height: 768},
	}
	api := agentdesktop.NewAPI(logger, fake)
	defer api.Close()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/screenshot", nil)

	handler := api.Routes()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var result agentdesktop.ScreenshotResult
	err := json.NewDecoder(rr.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, 1024, result.Width)
	assert.Equal(t, 768, result.Height)
}

func TestHandleAction_LeftClick(t *testing.T) {
	t.Parallel()

	logger := slogtest.Make(t, nil)
	fake := &fakeDesktop{
		startCfg: agentdesktop.DisplayConfig{Width: 1920, Height: 1080},
	}
	api := agentdesktop.NewAPI(logger, fake)
	defer api.Close()

	body := agentdesktop.ComputerAction{
		Action:     "left_click",
		Coordinate: &[2]int{100, 200},
	}
	b, err := json.Marshal(body)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/action", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")

	handler := api.Routes()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp agentdesktop.ComputerActionResponse
	err = json.NewDecoder(rr.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "left_click action performed", resp.Output)
	assert.Equal(t, [2]int{100, 200}, fake.lastMove)
}

func TestHandleAction_UnknownAction(t *testing.T) {
	t.Parallel()

	logger := slogtest.Make(t, nil)
	fake := &fakeDesktop{
		startCfg: agentdesktop.DisplayConfig{Width: 1920, Height: 1080},
	}
	api := agentdesktop.NewAPI(logger, fake)
	defer api.Close()

	body := agentdesktop.ComputerAction{Action: "explode"}
	b, err := json.Marshal(body)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/action", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")

	handler := api.Routes()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandleAction_KeyAction(t *testing.T) {
	t.Parallel()

	logger := slogtest.Make(t, nil)
	fake := &fakeDesktop{
		startCfg: agentdesktop.DisplayConfig{Width: 1920, Height: 1080},
	}
	api := agentdesktop.NewAPI(logger, fake)
	defer api.Close()

	text := "Return"
	body := agentdesktop.ComputerAction{
		Action: "key",
		Text:   &text,
	}
	b, err := json.Marshal(body)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/action", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")

	handler := api.Routes()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "Return", fake.lastKey)
}

func TestHandleAction_TypeAction(t *testing.T) {
	t.Parallel()

	logger := slogtest.Make(t, nil)
	fake := &fakeDesktop{
		startCfg: agentdesktop.DisplayConfig{Width: 1920, Height: 1080},
	}
	api := agentdesktop.NewAPI(logger, fake)
	defer api.Close()

	text := "hello world"
	body := agentdesktop.ComputerAction{
		Action: "type",
		Text:   &text,
	}
	b, err := json.Marshal(body)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/action", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")

	handler := api.Routes()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "hello world", fake.lastTyped)
}

func TestHandleAction_ScrollDown(t *testing.T) {
	t.Parallel()

	logger := slogtest.Make(t, nil)
	fake := &fakeDesktop{
		startCfg: agentdesktop.DisplayConfig{Width: 1920, Height: 1080},
	}
	api := agentdesktop.NewAPI(logger, fake)
	defer api.Close()

	dir := "down"
	amount := 5
	body := agentdesktop.ComputerAction{
		Action:          "scroll",
		Coordinate:      &[2]int{500, 400},
		ScrollDirection: &dir,
		ScrollAmount:    &amount,
	}
	b, err := json.Marshal(body)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/action", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")

	handler := api.Routes()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	// dy should be positive 5 for "down".
	assert.Equal(t, [4]int{500, 400, 0, 5}, fake.lastScroll)
}

func TestHandleAction_CoordinateScaling(t *testing.T) {
	t.Parallel()

	logger := slogtest.Make(t, nil)
	fake := &fakeDesktop{
		// Native display is 1920x1080.
		startCfg: agentdesktop.DisplayConfig{Width: 1920, Height: 1080},
	}
	api := agentdesktop.NewAPI(logger, fake)
	defer api.Close()

	// Model is working in a 1280x720 coordinate space.
	sw := 1280
	sh := 720
	body := agentdesktop.ComputerAction{
		Action:       "mouse_move",
		Coordinate:   &[2]int{640, 360},
		ScaledWidth:  &sw,
		ScaledHeight: &sh,
	}
	b, err := json.Marshal(body)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/action", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")

	handler := api.Routes()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	// 640 in 1280-space → 960 in 1920-space (midpoint maps to
	// midpoint).
	assert.Equal(t, 960, fake.lastMove[0])
	assert.Equal(t, 540, fake.lastMove[1])
}

func TestClose_DelegatesToDesktop(t *testing.T) {
	t.Parallel()

	logger := slogtest.Make(t, nil)
	fake := &fakeDesktop{}
	api := agentdesktop.NewAPI(logger, fake)

	err := api.Close()
	require.NoError(t, err)
	assert.True(t, fake.closed)
}

func TestClose_PreventsNewSessions(t *testing.T) {
	t.Parallel()

	logger := slogtest.Make(t, nil)
	// After Close(), Start() will return an error because the
	// underlying Desktop is closed.
	fake := &fakeDesktop{}
	api := agentdesktop.NewAPI(logger, fake)

	err := api.Close()
	require.NoError(t, err)

	// Simulate the closed desktop returning an error on Start().
	fake.startErr = xerrors.New("desktop is closed")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	handler := api.Routes()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

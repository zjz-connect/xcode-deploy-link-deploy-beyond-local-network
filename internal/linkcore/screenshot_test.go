package linkcore

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func samplePNG(t *testing.T) []byte {
	t.Helper()
	i := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	i.SetNRGBA(0, 0, color.NRGBA{R: 23, G: 50, B: 91, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, i); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestScreenshotPreservesPNGAndExistingOutput(t *testing.T) {
	raw := samplePNG(t)
	path := filepath.Join(t.TempDir(), "screen.png")
	w, h, err := SaveScreenshot(path, raw)
	if err != nil || w != 2 || h != 3 {
		t.Fatalf("save: %dx%d %v", w, h, err)
	}
	saved, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(saved, raw) {
		t.Fatal("PNG bytes changed")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("output is not owner-only")
	}
	if _, _, err := SaveScreenshot(path, raw); err == nil {
		t.Fatal("existing output was overwritten")
	}
	saved, _ = os.ReadFile(path)
	if !bytes.Equal(saved, raw) {
		t.Fatal("existing output changed")
	}
}

func TestScreenshotRejectsCorruptPNG(t *testing.T) {
	raw := samplePNG(t)
	for _, data := range [][]byte{nil, []byte("not a PNG"), raw[:len(raw)/2]} {
		path := filepath.Join(t.TempDir(), "screen.png")
		if _, _, err := SaveScreenshot(path, data); err == nil {
			t.Fatal("invalid image accepted")
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid image created an output")
		}
	}
}

func TestScreenshotDoesNotReopenOrCloseWarmSession(t *testing.T) {
	d := testDaemon(t)
	raw := samplePNG(t)
	calls := 0
	session := &fakeSession{done: make(chan error), screenshot: func(context.Context) ([]byte, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("developer service unavailable")
		}
		return raw, nil
	}}
	d.session = session
	d.state = StateActive
	d.generation = 7
	d.openSession = func(context.Context, Config) (sessionHandle, error) {
		t.Fatal("capture attempted to create or pair a new tunnel")
		return nil, nil
	}
	if _, err := d.screenshot(context.Background()); err == nil {
		t.Fatal("capture error was lost")
	}
	if d.Snapshot().State != StateActive || d.Snapshot().Generation != 7 || session.closed.Load() != 0 {
		t.Fatal("capture failure changed the warm session")
	}
	got, err := d.screenshot(context.Background())
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatal("subsequent capture did not use the existing session")
	}
	if session.closed.Load() != 0 || d.currentSession() != session {
		t.Fatal("capture closed or replaced the session")
	}
}

func TestScreenshotRefusesBusySession(t *testing.T) {
	d := testDaemon(t)
	d.session = &fakeSession{screenshot: func(context.Context) ([]byte, error) {
		t.Fatal("capture ran during another operation")
		return nil, nil
	}}
	d.operationMu.Lock()
	_, err := d.screenshot(context.Background())
	d.operationMu.Unlock()
	if errorCode(err) != "session_busy" {
		t.Fatalf("error=%v", err)
	}
}

func TestScreenshotControlSocketPreservesImage(t *testing.T) {
	d := testDaemon(t)
	raw := samplePNG(t)
	session := &fakeSession{screenshot: func(context.Context) ([]byte, error) { return raw, nil }}
	d.session, d.state, d.generation = session, StateActive, 7
	listener, err := prepareUnixListener(d.socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	handled := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			d.handleConnection(ctx, connection)
		}
		handled <- err
	}()
	var result Response
	if err := Call(ctx, d.profilePath, Request{Command: "screenshot"}, func(r Response) { result = r }); err != nil {
		t.Fatal(err)
	}
	if err := <-handled; err != nil {
		t.Fatal(err)
	}
	if !result.OK || !result.Final || result.Event != "screenshot" || result.Generation != 7 || !bytes.Equal(result.ScreenshotPNG, raw) {
		t.Fatal("screenshot response lost image or session identity")
	}
	path := filepath.Join(t.TempDir(), "received.png")
	if _, _, err := SaveScreenshot(path, result.ScreenshotPNG); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(saved, raw) {
		t.Fatal("control round trip changed the image")
	}
	if session.closed.Load() != 0 || d.Snapshot().State != StateActive {
		t.Fatal("control round trip affected the tunnel")
	}
}

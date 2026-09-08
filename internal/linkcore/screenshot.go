package linkcore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"sync"
	"time"

	"github.com/danielpaulus/go-ios/ios/instruments"
)

const maxScreenshotBytes = 64 << 20
const maxScreenshotPixels = 16 << 20

// Screenshot opens an inner developer service on the already verified session.
// It never creates, closes or re-pairs the outer RemotePairing tunnel.
func (s *Session) Screenshot(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.device.Rsd.GetPort("com.apple.instruments.dtservicehub") == 0 {
		return nil, coded("capture_service_unavailable", "the active device does not advertise the Instruments developer service", nil)
	}
	service, err := instruments.NewScreenshotService(s.device)
	if err != nil {
		return nil, coded("capture_service_unavailable", "could not open the screenshot service on the current tunnel", err)
	}
	closeService := sync.OnceFunc(service.Close)
	stop := context.AfterFunc(ctx, closeService)
	defer func() { stop(); closeService() }()
	data, err := service.TakeScreenshot()
	if ctx.Err() != nil {
		return nil, coded("capture_timeout", "screenshot request was cancelled or timed out", ctx.Err())
	}
	if err != nil {
		return nil, coded("capture_failed", "the device screenshot request failed", err)
	}
	return data, nil
}

func (d *Daemon) screenshot(ctx context.Context) (Response, error) {
	if !d.operationMu.TryLock() {
		return Response{}, coded("session_busy", "another operation owns the device session", nil)
	}
	defer d.operationMu.Unlock()
	session := d.currentSession()
	if session == nil {
		return Response{}, coded("session_not_active", "no existing RemotePairing session is available", nil)
	}
	captureCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// A capture error does not change installation readiness or close the tunnel.
	data, err := session.Screenshot(captureCtx)
	if err != nil {
		return Response{}, err
	}
	if len(data) == 0 || len(data) > maxScreenshotBytes {
		return Response{}, coded("capture_invalid_image", "screenshot payload has an invalid size", nil)
	}
	snapshot := d.Snapshot()
	return Response{OK: true, Final: true, Event: "screenshot", State: snapshot.State,
		Generation: snapshot.Generation, ImageBytes: len(data), ScreenshotPNG: data}, nil
}

// SaveScreenshot preserves the returned PNG bytes and never replaces an output.
func SaveScreenshot(path string, data []byte) (int, int, error) {
	if len(data) == 0 || len(data) > maxScreenshotBytes {
		return 0, 0, coded("capture_invalid_image", "screenshot payload has an invalid size", nil)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > maxScreenshotPixels {
		return 0, 0, coded("capture_invalid_image", "device response is not a valid bounded PNG", err)
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		return 0, 0, coded("capture_invalid_image", "device PNG is incomplete or corrupt", err)
	}
	output, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, 0, coded("capture_output_failed", "could not create a new screenshot output", err)
	}
	written, writeErr := output.Write(data)
	if written != len(data) {
		writeErr = errors.Join(writeErr, fmt.Errorf("incomplete screenshot write"))
	}
	err = errors.Join(writeErr, output.Close())
	if err != nil {
		_ = os.Remove(path)
		return 0, 0, coded("capture_output_failed", "could not finish writing the screenshot", err)
	}
	return config.Width, config.Height, nil
}

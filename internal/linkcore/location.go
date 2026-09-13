package linkcore

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/danielpaulus/go-ios/ios/instruments"
)

type locationDriver interface {
	StartSimulateLocation(float64, float64) error
	StopSimulateLocation() error
	Close()
}

type LocationState struct {
	Phase        string   `json:"phase"`
	Latitude     *float64 `json:"latitude,omitempty"`
	Longitude    *float64 `json:"longitude,omitempty"`
	CommandTime  float64  `json:"commandTime,omitempty"`
	TunnelActive bool     `json:"tunnelActive"`
	Error        string   `json:"error,omitempty"`
}

type locationSession struct {
	mu     sync.Mutex
	driver locationDriver
	state  LocationState
	open   func(context.Context) (locationDriver, error)
}

func validLocation(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsNaN(lon) && !math.IsInf(lat, 0) && !math.IsInf(lon, 0) && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}

func (s *Session) OpenLocation(ctx context.Context) (locationDriver, error) {
	device, err := s.captureDevice(ctx)
	if err != nil {
		return nil, err
	}
	type opened struct {
		driver locationDriver
		err    error
	}
	result := make(chan opened)
	go func() {
		driver, err := instruments.NewLocationSimulationService(device)
		select {
		case result <- opened{driver, err}:
		case <-ctx.Done():
			if driver != nil {
				driver.Close()
			}
		}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-result:
		return result.driver, result.err
	}
}

func locationCall(ctx context.Context, driver locationDriver, call func() error) error {
	result := make(chan error, 1)
	go func() { result <- call() }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		driver.Close()
		return ctx.Err()
	}
}

func (s *locationSession) snapshot(tunnelActive bool) LocationState {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.state
	if state.Phase == "" {
		state.Phase = "idle"
	}
	state.TunnelActive = tunnelActive
	return state
}
func (s *locationSession) fail(message string) {
	if s.driver != nil {
		s.driver.Close()
		s.driver = nil
	}
	s.state.Phase = "lost"
	s.state.Error = message
}
func (s *locationSession) lost() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.driver != nil || s.state.Phase == "active" {
		s.fail("定位连接已中断，请重新连接主机后恢复定位。")
	}
}
func (s *locationSession) set(ctx context.Context, lat, lon float64) error {
	if !validLocation(lat, lon) {
		return errors.New("坐标无效。")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.driver == nil {
		driver, err := s.open(ctx)
		if err != nil {
			s.fail("无法打开手机定位服务。")
			return errors.New(s.state.Error)
		}
		s.driver = driver
	}
	started := float64(time.Now().UnixMilli()) / 1000
	s.state = LocationState{Phase: "applying", Latitude: &lat, Longitude: &lon, CommandTime: started}
	driver := s.driver
	err := locationCall(ctx, driver, func() error { return driver.StartSimulateLocation(lat, lon) })
	if err != nil {
		s.fail("定位命令未确认，请恢复真实定位。")
		return errors.New(s.state.Error)
	}
	s.state = LocationState{Phase: "active", Latitude: &lat, Longitude: &lon, CommandTime: started}
	return nil
}
func (s *locationSession) clear(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.driver == nil {
		driver, err := s.open(ctx)
		if err != nil {
			s.fail("无法连接手机，恢复命令尚未发送。")
			return errors.New(s.state.Error)
		}
		s.driver = driver
	}
	err := locationCall(ctx, s.driver, s.driver.StopSimulateLocation)
	if err != nil {
		s.fail("恢复命令未确认，请检查手机定位。")
		return errors.New(s.state.Error)
	}
	s.driver = nil
	s.state = LocationState{Phase: "idle"}
	return nil
}
func (s *locationSession) pulse(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			if s.driver != nil && s.state.Phase == "active" {
				callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
				driver := s.driver
				lat, lon := *s.state.Latitude, *s.state.Longitude
				err := locationCall(callCtx, driver, func() error { return driver.StartSimulateLocation(lat, lon) })
				cancel()
				if err != nil {
					s.fail("定位连接已中断，请检查手机定位。")
				}
			}
			s.mu.Unlock()
		}
	}
}

package linkcore

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeLocation struct {
	sets, stops, closes atomic.Int32
	failSet, failStop   bool
}

func (f *fakeLocation) StartSimulateLocation(float64, float64) error {
	f.sets.Add(1)
	if f.failSet {
		return errors.New("lost")
	}
	return nil
}
func (f *fakeLocation) StopSimulateLocation() error {
	f.stops.Add(1)
	if f.failStop {
		return errors.New("not confirmed")
	}
	f.Close()
	return nil
}
func (f *fakeLocation) Close() { f.closes.Add(1) }
func TestLocationSetUpdateClearAndLostState(t *testing.T) {
	driver := &fakeLocation{}
	session := &locationSession{open: func(context.Context) (locationDriver, error) { return driver, nil }}
	for _, point := range [][2]float64{{37.3349, -122.009}, {51.5, -.12}} {
		if err := session.set(context.Background(), point[0], point[1]); err != nil {
			t.Fatal(err)
		}
		state := session.snapshot(true)
		if state.Phase != "active" || *state.Latitude != point[0] || *state.Longitude != point[1] || state.CommandTime == 0 {
			t.Fatal(state)
		}
	}
	if err := session.clear(context.Background()); err != nil {
		t.Fatal(err)
	}
	if driver.sets.Load() != 2 || driver.stops.Load() != 1 || session.snapshot(true).Phase != "idle" {
		t.Fatal("set/update/clear lifecycle")
	}
	driver.failSet = true
	if err := session.set(context.Background(), 1, 2); err == nil {
		t.Fatal("failed set accepted")
	}
	if session.snapshot(true).Phase != "lost" || session.snapshot(true).Latitude == nil {
		t.Fatal("failure reported active")
	}
	driver.failSet = false
	if err := session.set(context.Background(), 1, 2); err != nil {
		t.Fatal(err)
	}
	session.lost()
	if session.snapshot(false).Phase != "lost" || session.driver != nil {
		t.Fatal("lost tunnel retained an active location driver")
	}
}
func TestLocationClearFailureIsNotRestoration(t *testing.T) {
	driver := &fakeLocation{failStop: true}
	session := &locationSession{open: func(context.Context) (locationDriver, error) { return driver, nil }}
	if err := session.clear(context.Background()); err == nil {
		t.Fatal("failed clear accepted")
	}
	if session.snapshot(true).Phase == "idle" {
		t.Fatal("clear failure reported idle")
	}
}
func TestLocationRejectsInvalidCoordinates(t *testing.T) {
	for _, point := range [][2]float64{{math.NaN(), 0}, {0, math.Inf(1)}, {90.01, 0}, {0, -180.01}} {
		if validLocation(point[0], point[1]) {
			t.Fatal(point)
		}
	}
	if !validLocation(-90, 180) {
		t.Fatal("valid boundary rejected")
	}
}
func TestLocationHTTPAuthorizationScopeAndRequestLifetime(t *testing.T) {
	driver := &fakeLocation{}
	session := &locationSession{open: func(context.Context) (locationDriver, error) { return driver, nil }}
	handler := locationHandler(context.Background(), "owner-secret", session, func() bool { return true })
	cases := []struct {
		method, path, token, body string
		want                      int
	}{
		{"PUT", "/v1/location", "", "{\"latitude\":1,\"longitude\":2}", 401},
		{"POST", "/install", "Bearer owner-secret", "", 404},
		{"PUT", "/v1/location", "Bearer owner-secret", "{\"latitude\":1}", 400},
		{"PUT", "/v1/location", "Bearer owner-secret", "{\"latitude\":1,\"longitude\":181}", 400},
		{"PUT", "/v1/location", "Bearer owner-secret", "{\"latitude\":1,\"longitude\":2,\"command\":\"install\"}", 400},
		{"PUT", "/v1/location", "Bearer owner-secret", "{\"latitude\":1,\"longitude\":2} {}", 400},
		{"GET", "/v1/location?token=owner-secret", "Bearer owner-secret", "", 404},
	}
	for _, test := range cases {
		r := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		r.Header.Set("Authorization", test.token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != test.want {
			t.Fatalf("%s %s: %d", test.method, test.path, w.Code)
		}
	}
	if driver.sets.Load() != 0 {
		t.Fatal("unauthorized or invalid mutation reached device")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("PUT", "/v1/location", strings.NewReader(`{"latitude":1,"longitude":2}`)).WithContext(cancelled)
	r.Header.Set("Authorization", "Bearer owner-secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK || session.snapshot(true).Phase != "active" {
		t.Fatal("accepted set incorrectly owned by HTTP connection")
	}
	if driver.stops.Load() != 0 {
		t.Fatal("HTTP completion cleared location")
	}
}
func TestLocationCallDeadlineClosesTheDeveloperConnection(t *testing.T) {
	driver := &fakeLocation{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	finish := make(chan struct{})
	if err := locationCall(ctx, driver, func() error { <-finish; return nil }); err == nil {
		t.Fatal("deadline ignored")
	}
	close(finish)
	if driver.closes.Load() != 1 {
		t.Fatal("deadline did not close connection")
	}
}

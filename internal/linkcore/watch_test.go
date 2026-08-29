package linkcore

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestWatchEmitsColdOnlyAfterObservedWarmSessionLoss(t *testing.T) {
	responses := []Response{
		{State: StateWaiting},
		{State: StateActive, Generation: 1},
		{State: StateRecovering, Generation: 1, LastErrorCode: "service_unresponsive"},
		{State: StateWaiting, Generation: 1, SessionLosses: 1, LastErrorCode: "remote_pairing_cold"},
	}
	var mu sync.Mutex
	index := 0
	read := func(context.Context) (Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if index >= len(responses) {
			return responses[len(responses)-1], nil
		}
		response := responses[index]
		index++
		return response, nil
	}
	var emitted []Response
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := watchStatus(ctx, time.Millisecond, read, func(response Response) {
		emitted = append(emitted, response)
	}); err != nil {
		t.Fatal(err)
	}
	if len(emitted) != 4 {
		t.Fatalf("emitted %d events, want 4: %#v", len(emitted), emitted)
	}
	if emitted[2].Event != "state_changed" || emitted[2].State != StateRecovering {
		t.Fatalf("recovering event = %#v", emitted[2])
	}
	if emitted[3].Event != "cold" || !emitted[3].Final || emitted[3].SessionLosses != 1 {
		t.Fatalf("cold event = %#v", emitted[3])
	}
}

func TestWatchDoesNotCallRecoveringCold(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	read := func(context.Context) (Response, error) {
		calls++
		if calls == 1 {
			return Response{State: StateActive, Generation: 4, SessionLosses: 2}, nil
		}
		if calls == 2 {
			return Response{State: StateRecovering, Generation: 4, SessionLosses: 2, LastErrorCode: "service_unresponsive"}, nil
		}
		cancel()
		return Response{State: StateRecovering, Generation: 4, SessionLosses: 2}, nil
	}
	var emitted []Response
	if err := watchStatus(ctx, time.Millisecond, read, func(response Response) {
		emitted = append(emitted, response)
	}); err != nil {
		t.Fatal(err)
	}
	for _, response := range emitted {
		if response.Event == "cold" {
			t.Fatalf("recovering emitted cold: %#v", emitted)
		}
	}
}

package linkcore

import (
	"context"
	"time"
)

type statusReader func(context.Context) (Response, error)

type watchSignature struct {
	state          string
	generation     uint64
	sessionLosses  uint64
	installCount   uint64
	uninstallCount uint64
	lastErrorCode  string
	errorCode      string
}

func isWarmState(response Response) bool {
	if response.Generation == 0 {
		return false
	}
	switch response.State {
	case StateActive, StateRecovering, StateInstalling, StateUninstalling:
		return true
	default:
		return false
	}
}

func watchStatus(ctx context.Context, interval time.Duration, read statusReader, emit func(Response)) error {
	if interval <= 0 {
		return coded("watch_invalid", "watch interval must be positive", nil)
	}

	var previous watchSignature
	havePrevious := false
	warmObserved := false
	var lossBaseline uint64
	haveBaseline := false
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}

		response, err := read(ctx)
		if err != nil {
			code, message := ErrorDetails(err)
			signature := watchSignature{errorCode: code}
			if !havePrevious || signature != previous {
				emit(Response{
					OK:         false,
					Final:      false,
					Event:      "watch_error",
					ErrorCode:  code,
					Error:      message,
					ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
				})
				previous = signature
				havePrevious = true
			}
			timer.Reset(interval)
			continue
		}

		if !haveBaseline {
			lossBaseline = response.SessionLosses
			haveBaseline = true
		}
		if isWarmState(response) {
			warmObserved = true
		}
		response.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if warmObserved && response.SessionLosses > lossBaseline {
			response.OK = true
			response.Final = true
			response.Event = "cold"
			response.LastErrorCode = "session_lost"
			emit(response)
			return nil
		}

		signature := watchSignature{
			state:          response.State,
			generation:     response.Generation,
			sessionLosses:  response.SessionLosses,
			installCount:   response.InstallCount,
			uninstallCount: response.UninstallCount,
			lastErrorCode:  response.LastErrorCode,
		}
		if !havePrevious || signature != previous {
			response.OK = true
			response.Final = false
			if !havePrevious {
				response.Event = "watching"
			} else {
				response.Event = "state_changed"
			}
			emit(response)
			previous = signature
			havePrevious = true
		}
		timer.Reset(interval)
	}
}

// Watch reports status transitions and exits after a warm generation records
// an outer userspace tunnel loss. It never invokes an installation service.
func Watch(ctx context.Context, profilePath string, interval time.Duration, emit func(Response)) error {
	read := func(callContext context.Context) (Response, error) {
		var status Response
		err := Call(callContext, profilePath, Request{Command: "status"}, func(response Response) {
			status = response
		})
		return status, err
	}
	return watchStatus(ctx, interval, read, emit)
}

package linkcore

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

type Request struct {
	Command string `json:"command"`
	AppPath string `json:"app_path,omitempty"`
}

type Response struct {
	OK            bool   `json:"ok"`
	Final         bool   `json:"final"`
	Event         string `json:"event,omitempty"`
	State         string `json:"state,omitempty"`
	Generation    uint64 `json:"generation,omitempty"`
	SessionLosses uint64 `json:"session_loss_count,omitempty"`
	InstallCount  uint64 `json:"install_count,omitempty"`
	Percent       int    `json:"percent,omitempty"`
	Status        string `json:"status,omitempty"`
	ErrorCode     string `json:"error_code,omitempty"`
	Error         string `json:"error,omitempty"`
	LastErrorCode string `json:"last_error_code,omitempty"`
	ObservedAt    string `json:"observed_at,omitempty"`
}

type Snapshot struct {
	State         string
	Generation    uint64
	SessionLosses uint64
	InstallCount  uint64
	LastErrorCode string
}

func snapshotResponse(snapshot Snapshot) Response {
	return Response{
		OK:            true,
		Final:         true,
		Event:         "status",
		State:         snapshot.State,
		Generation:    snapshot.Generation,
		SessionLosses: snapshot.SessionLosses,
		InstallCount:  snapshot.InstallCount,
		LastErrorCode: snapshot.LastErrorCode,
	}
}

func errorResponse(err error) Response {
	return Response{
		OK:        false,
		Final:     true,
		Event:     "error",
		ErrorCode: errorCode(err),
		Error:     publicError(err),
	}
}

func prepareUnixListener(socketPath string) (net.Listener, error) {
	if _, err := os.Lstat(socketPath); err == nil {
		connection, dialErr := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
		if dialErr == nil {
			_ = connection.Close()
			return nil, coded("deploy_link_already_running", "another Deploy Link process owns the control socket", nil)
		}
		if err := os.Remove(socketPath); err != nil {
			return nil, coded("control_socket_failed", "could not remove a stale control socket", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, coded("control_socket_failed", "could not inspect the control socket", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, coded("control_socket_failed", "could not bind the control socket", err)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(socketPath)
		return nil, coded("control_socket_failed", "could not protect the control socket", err)
	}
	return listener, nil
}

func readRequest(connection net.Conn) (Request, error) {
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	decoder := json.NewDecoder(io.LimitReader(connection, 1<<20))
	decoder.DisallowUnknownFields()
	var request Request
	if err := decoder.Decode(&request); err != nil {
		return Request{}, coded("control_request_invalid", "could not decode control request", err)
	}
	_ = connection.SetReadDeadline(time.Time{})
	return request, nil
}

func Call(ctx context.Context, profilePath string, request Request, onResponse func(Response)) error {
	socketPath, err := SocketPath(profilePath)
	if err != nil {
		return err
	}
	dialer := net.Dialer{}
	connection, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return coded("deploy_link_not_running", "Deploy Link does not answer on the profile control socket", err)
	}
	defer connection.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return coded("control_request_failed", "could not send control request", err)
	}
	decoder := json.NewDecoder(bufio.NewReader(connection))
	for {
		var response Response
		if err := decoder.Decode(&response); err != nil {
			return coded("control_response_failed", "daemon response ended before a final result", err)
		}
		if onResponse != nil {
			onResponse(response)
		}
		if response.Final {
			if !response.OK {
				return coded(response.ErrorCode, response.Error, nil)
			}
			return nil
		}
	}
}

type safeEncoder struct {
	mu      sync.Mutex
	encoder *json.Encoder
}

func (e *safeEncoder) Encode(response Response) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.encoder.Encode(response)
}

func writeOneResponse(writer io.Writer, response Response) error {
	return json.NewEncoder(writer).Encode(response)
}

func validateCommand(request Request) error {
	switch request.Command {
	case "status", "stop":
		if request.AppPath != "" {
			return coded("control_request_invalid", fmt.Sprintf("%s does not accept app_path", request.Command), nil)
		}
	case "install":
		if request.AppPath == "" {
			return coded("control_request_invalid", "install requires app_path", nil)
		}
	default:
		return coded("control_request_invalid", "unknown command", nil)
	}
	return nil
}

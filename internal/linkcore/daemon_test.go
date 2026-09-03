package linkcore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type fakeSession struct {
	closed    atomic.Int32
	done      chan error
	install   func(context.Context, AppBundle, func(int, string)) error
	uninstall func(context.Context, string) error
}

func (s *fakeSession) Close() error {
	s.closed.Add(1)
	return nil
}

func (s *fakeSession) Done() <-chan error {
	return s.done
}

func (s *fakeSession) Install(ctx context.Context, app AppBundle, progress func(int, string)) error {
	if s.install != nil {
		return s.install(ctx, app, progress)
	}
	return nil
}

func (s *fakeSession) Uninstall(ctx context.Context, bundleIdentifier string) error {
	if s.uninstall != nil {
		return s.uninstall(ctx, bundleIdentifier)
	}
	return nil
}

func testDaemon(t *testing.T) *Daemon {
	t.Helper()
	config, profilePath := testConfig(t)
	daemon, err := NewDaemon(config, profilePath)
	if err != nil {
		t.Fatal(err)
	}
	return daemon
}

func TestDaemonEstablishesAndDropsOnlyOuterFailedSession(t *testing.T) {
	daemon := testDaemon(t)
	session := &fakeSession{done: make(chan error, 1)}
	var opens atomic.Int32
	daemon.openSession = func(context.Context, Config) (sessionHandle, error) {
		if opens.Add(1) > 1 {
			return nil, coded("remote_pairing_cold", "listener closed", nil)
		}
		return session, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		daemon.connectionLoop(ctx)
		close(done)
	}()
	deadline := time.Now().Add(time.Second)
	for daemon.Snapshot().State != StateActive {
		if time.Now().After(deadline) {
			t.Fatal("daemon did not establish session")
		}
		time.Sleep(time.Millisecond)
	}
	session.done <- errors.New("transport closed")
	for daemon.Snapshot().SessionLosses != 1 {
		if time.Now().After(deadline) {
			t.Fatal("daemon did not record outer session loss")
		}
		time.Sleep(time.Millisecond)
	}
	if got := daemon.Snapshot(); got.Generation != 1 {
		t.Fatalf("lost snapshot = %#v", got)
	}
	if got := session.closed.Load(); got != 1 {
		t.Fatalf("close count = %d, want 1", got)
	}
	cancel()
	<-done
}

func TestDaemonClassifiesColdAcquisition(t *testing.T) {
	daemon := testDaemon(t)
	daemon.openSession = func(context.Context, Config) (sessionHandle, error) {
		return nil, coded("remote_pairing_cold", "listener closed", nil)
	}
	daemon.establish(context.Background())
	if got := daemon.Snapshot(); got.State != StateWaiting || got.LastErrorCode != "remote_pairing_cold" || got.Generation != 0 {
		t.Fatalf("snapshot = %#v", got)
	}
}

func TestDaemonSerializesInstalls(t *testing.T) {
	daemon := testDaemon(t)
	daemon.validateApp = func(context.Context, string) (AppBundle, error) {
		return AppBundle{Path: "/tmp/Test.app", BundleIdentifier: "one.example.test"}, nil
	}
	var running atomic.Int32
	var maximum atomic.Int32
	session := &fakeSession{install: func(_ context.Context, _ AppBundle, progress func(int, string)) error {
		current := running.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		progress(50, "Installing")
		time.Sleep(20 * time.Millisecond)
		running.Add(-1)
		return nil
	}}
	daemon.session = session
	daemon.state = StateActive

	results := make(chan error, 2)
	for range 2 {
		go func() {
			results <- daemon.install(context.Background(), "/tmp/Test.app", func(int, string) {})
		}()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if got := maximum.Load(); got != 1 {
		t.Fatalf("maximum concurrent installs = %d, want 1", got)
	}
	if got := daemon.Snapshot(); got.State != StateActive || got.InstallCount != 2 {
		t.Fatalf("snapshot = %#v", got)
	}
}

func TestDaemonSerializesInstallAndUninstall(t *testing.T) {
	daemon := testDaemon(t)
	daemon.validateApp = func(context.Context, string) (AppBundle, error) {
		return AppBundle{Path: "/tmp/Test.app", BundleIdentifier: "one.example.test"}, nil
	}
	var running atomic.Int32
	var maximum atomic.Int32
	operation := func() {
		current := running.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		running.Add(-1)
	}
	session := &fakeSession{
		install: func(context.Context, AppBundle, func(int, string)) error {
			operation()
			return nil
		},
		uninstall: func(context.Context, string) error {
			operation()
			return nil
		},
	}
	daemon.session = session
	daemon.state = StateActive

	results := make(chan error, 2)
	go func() {
		results <- daemon.install(context.Background(), "/tmp/Test.app", func(int, string) {})
	}()
	go func() {
		results <- daemon.uninstall(context.Background(), "one.example.test")
	}()
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if got := maximum.Load(); got != 1 {
		t.Fatalf("maximum concurrent lifecycle operations = %d, want 1", got)
	}
	if got := daemon.Snapshot(); got.State != StateActive || got.InstallCount != 1 || got.UninstallCount != 1 {
		t.Fatalf("snapshot = %#v", got)
	}
}

func TestDaemonRetainsSessionWhenInstallServiceFails(t *testing.T) {
	daemon := testDaemon(t)
	daemon.validateApp = func(context.Context, string) (AppBundle, error) {
		return AppBundle{Path: "/tmp/Test.app", BundleIdentifier: "one.example.test"}, nil
	}
	session := &fakeSession{
		install: func(context.Context, AppBundle, func(int, string)) error {
			return coded("install_failed", "transfer stalled", context.DeadlineExceeded)
		},
	}
	daemon.session = session
	daemon.state = StateActive
	daemon.generation = 1

	if err := daemon.install(context.Background(), "/tmp/Test.app", func(int, string) {}); errorCode(err) != "install_failed" {
		t.Fatalf("install error = %v, want install_failed", err)
	}
	if got := daemon.Snapshot(); got.State != StateRecovering || got.LastErrorCode != "service_unresponsive" || got.Generation != 1 {
		t.Fatalf("recovering snapshot = %#v", got)
	}
	if got := session.closed.Load(); got != 0 {
		t.Fatalf("close count after install timeout = %d, want 0", got)
	}
	if daemon.currentSession() != session {
		t.Fatal("failed install discarded the warm outer session")
	}
}

func TestDaemonRetainsSessionWhenUninstallServiceFails(t *testing.T) {
	daemon := testDaemon(t)
	session := &fakeSession{
		uninstall: func(context.Context, string) error {
			return coded("uninstall_failed", "removal stalled", context.DeadlineExceeded)
		},
	}
	daemon.session = session
	daemon.state = StateActive
	daemon.generation = 1

	if err := daemon.uninstall(context.Background(), "one.example.test"); errorCode(err) != "uninstall_failed" {
		t.Fatalf("uninstall error = %v, want uninstall_failed", err)
	}
	if got := daemon.Snapshot(); got.State != StateRecovering || got.LastErrorCode != "service_unresponsive" || got.Generation != 1 {
		t.Fatalf("recovering snapshot = %#v", got)
	}
	if got := session.closed.Load(); got != 0 {
		t.Fatalf("close count after uninstall timeout = %d, want 0", got)
	}
	if daemon.currentSession() != session {
		t.Fatal("failed uninstall discarded the warm outer session")
	}
}

func TestDaemonServeControlSocketLifecycle(t *testing.T) {
	daemon := testDaemon(t)
	session := &fakeSession{}
	daemon.openSession = func(context.Context, Config) (sessionHandle, error) {
		return session, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- daemon.Serve(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if info, err := os.Stat(daemon.socketPath); err == nil {
			if got := info.Mode().Perm(); got != 0o600 {
				t.Fatalf("socket mode = %#o, want 0600", got)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("control socket did not appear")
		}
		time.Sleep(5 * time.Millisecond)
	}
	for daemon.Snapshot().State != StateActive {
		if time.Now().After(deadline) {
			t.Fatal("daemon did not establish its test session")
		}
		time.Sleep(5 * time.Millisecond)
	}

	var responses []Response
	callCtx, callCancel := context.WithTimeout(context.Background(), time.Second)
	defer callCancel()
	if err := Call(callCtx, daemon.profilePath, Request{Command: "status"}, func(response Response) {
		responses = append(responses, response)
	}); err != nil {
		t.Fatal(err)
	}
	if len(responses) != 1 || !responses[0].Final || responses[0].Event != "status" {
		t.Fatalf("responses = %#v", responses)
	}
	responses = nil
	if err := Call(callCtx, daemon.profilePath, Request{
		Command:          "uninstall",
		BundleIdentifier: "one.example.test",
	}, func(response Response) {
		responses = append(responses, response)
	}); err != nil {
		t.Fatal(err)
	}
	if len(responses) != 2 || responses[0].Event != "accepted" ||
		!responses[1].Final || responses[1].Event != "uninstalled" ||
		responses[1].UninstallCount != 1 {
		t.Fatalf("uninstall responses = %#v", responses)
	}
	if err := Call(callCtx, daemon.profilePath, Request{Command: "stop"}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-served:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not stop")
	}
	if _, err := os.Stat(daemon.socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket still exists: %v", err)
	}
	if got := session.closed.Load(); got != 1 {
		t.Fatalf("close count = %d, want 1", got)
	}
}

func TestPrepareUnixListenerRejectsLiveOwner(t *testing.T) {
	directory, err := os.MkdirTemp("/tmp", "nodus-remote-deploy-listener-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	socketPath := filepath.Join(directory, "nodus-remote-deploy.sock")
	listener, err := prepareUnixListener(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if _, err := prepareUnixListener(socketPath); errorCode(err) != "nodus_remote_deploy_already_running" {
		t.Fatalf("error = %v, want nodus_remote_deploy_already_running", err)
	}
}

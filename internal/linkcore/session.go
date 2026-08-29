package linkcore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/installationproxy"
	"github.com/danielpaulus/go-ios/ios/tunnel"
	"github.com/danielpaulus/go-ios/ios/zipconduit"
)

type Session struct {
	device    ios.DeviceEntry
	tunnel    tunnel.Tunnel
	closeOnce sync.Once
}

func classifyTunnelError(err error) error {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "connection refused"),
		strings.Contains(message, "dial timed out"),
		strings.Contains(message, "i/o timeout"),
		strings.Contains(message, "no route to host"):
		return coded("remote_pairing_cold", "Tailnet is reachable but iOS exposes no usable RemotePairing listener", err)
	case strings.Contains(message, "pair verification failed"), strings.Contains(message, "saved identity was rejected"):
		return coded("pair_verify_failed", "the endpoint rejected the configured pairing record", err)
	default:
		return coded("tunnel_failed", "could not establish the trusted userspace tunnel", err)
	}
}

func OpenSession(ctx context.Context, config Config) (*Session, error) {
	pairRecords, err := tunnel.NewPairRecordManagerFromPymobiledevice3(config.PairRecordPath)
	if err != nil {
		return nil, coded("pair_record_invalid", "could not load the RemotePairing identity", err)
	}
	device := ios.DeviceEntry{Properties: ios.DeviceProperties{
		SerialNumber:   config.RemoteIdentifier,
		ConnectionType: "Network",
	}}
	tun, err := tunnel.ConnectRemotePairingTCPUserspace(
		ctx,
		device,
		config.TargetTailnetIP,
		config.RemotePairingPort,
		config.LocalForwardPort,
		pairRecords,
	)
	if err != nil {
		return nil, classifyTunnelError(err)
	}

	device.Address = tun.Address
	device.UserspaceTUN = true
	device.UserspaceTUNHost = "127.0.0.1"
	device.UserspaceTUNPort = config.LocalForwardPort
	rsdService, err := ios.NewWithAddrPortDevice(tun.Address, tun.RsdPort, device)
	if err != nil {
		_ = tun.Close()
		return nil, coded("tunnel_failed", "could not connect to RSD", err)
	}
	type handshakeResult struct {
		response ios.RsdHandshakeResponse
		err      error
	}
	handshake := make(chan handshakeResult, 1)
	go func() {
		response, handshakeErr := rsdService.Handshake()
		handshake <- handshakeResult{response: response, err: handshakeErr}
	}()
	var rsd ios.RsdHandshakeResponse
	select {
	case <-ctx.Done():
		_ = rsdService.Close()
		_ = tun.Close()
		return nil, coded("tunnel_failed", "RSD handshake timed out", ctx.Err())
	case result := <-handshake:
		_ = rsdService.Close()
		if result.err != nil {
			_ = tun.Close()
			return nil, coded("tunnel_failed", "RSD handshake failed", result.err)
		}
		rsd = result.response
	}
	if rsd.Udid != config.RemoteIdentifier {
		_ = tun.Close()
		return nil, coded("rsd_identity_mismatch", "RSD identity differs from the configured device", nil)
	}
	device.Rsd = rsd
	session := &Session{device: device, tunnel: tun}
	if _, err := session.browseUserApps(ctx); err != nil {
		_ = session.Close()
		return nil, coded("tunnel_failed", "InstallationProxy readiness check failed", err)
	}
	return session, nil
}

func (s *Session) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		closeErr = s.tunnel.Close()
	})
	return closeErr
}

func (s *Session) Done() <-chan error {
	return s.tunnel.Done()
}

func (s *Session) browseUserApps(ctx context.Context) ([]installationproxy.AppInfo, error) {
	proxy, err := installationproxy.New(s.device)
	if err != nil {
		return nil, err
	}
	type browseResult struct {
		apps []installationproxy.AppInfo
		err  error
	}
	result := make(chan browseResult, 1)
	go func() {
		apps, browseErr := proxy.BrowseUserApps()
		result <- browseResult{apps: apps, err: browseErr}
	}()
	select {
	case <-ctx.Done():
		proxy.Close()
		return nil, ctx.Err()
	case response := <-result:
		proxy.Close()
		return response.apps, response.err
	}
}

func (s *Session) Install(ctx context.Context, app AppBundle, progress func(int, string)) error {
	conduit, err := zipconduit.New(s.device)
	if err != nil {
		return coded("install_failed", "could not connect to streaming zip conduit", err)
	}
	installResult := make(chan error, 1)
	go func() {
		installResult <- conduit.SendFileWithProgress(app.Path, progress)
	}()
	select {
	case <-ctx.Done():
		_ = conduit.Close()
		return coded("install_failed", "installation timed out", ctx.Err())
	case err := <-installResult:
		_ = conduit.Close()
		if err != nil {
			return coded("install_failed", "iOS rejected or interrupted the app transfer", err)
		}
	}

	apps, err := s.browseUserApps(ctx)
	if err != nil {
		return coded("install_failed", "post-install InstallationProxy readback failed", err)
	}
	for _, installed := range apps {
		if installed.CFBundleIdentifier() == app.BundleIdentifier {
			return nil
		}
	}
	return coded("install_failed", "installed bundle was absent from InstallationProxy readback", errors.New(app.BundleIdentifier))
}

func (s *Session) String() string {
	return fmt.Sprintf("session<userspace=%t>", s.device.UserspaceTUN)
}

package linkcore

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/tunnel"
	"github.com/danielpaulus/go-ios/ios/xpc"
	"golang.org/x/net/http2"
)

const discoveryPeer = "fixture-phone"
const discoveryAddress = "fd00::42"
const discoveryPort = 62001

type discoveryReply struct {
	peer     string
	services map[string]uint32
	stopAt   string
}

// This local forwarder speaks the pinned HTTP2/XPC codecs. It never connects
// to a phone and validates the exact endpoint requested in the TUN envelope.
func discoveryFixture(t *testing.T, replies ...discoveryReply) (*Session, <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	reached := make(chan string, len(replies))
	finished := make(chan error, len(replies))
	go func() {
		for _, reply := range replies {
			conn, err := listener.Accept()
			if err != nil {
				finished <- err
				continue
			}
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			finished <- serveDiscovery(conn, reply, reached)
			_ = conn.Close()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		for range replies {
			if err := <-finished; err != nil {
				t.Errorf("discovery fixture: %v", err)
			}
		}
	})
	base := ios.DeviceEntry{
		Properties: ios.DeviceProperties{SerialNumber: discoveryPeer, ConnectionType: "Network"},
		Address:    discoveryAddress, UserspaceTUN: true,
		UserspaceTUNHost: "127.0.0.1", UserspaceTUNPort: listener.Addr().(*net.TCPAddr).Port,
		Rsd: ios.RsdHandshakeResponse{Udid: discoveryPeer, OSVersion: "26.0", Services: map[string]ios.RsdServiceEntry{
			"cached-only": {Port: 62002},
		}},
	}
	return &Session{device: base, tunnel: tunnel.Tunnel{Address: discoveryAddress, RsdPort: discoveryPort}}, reached
}

func serveDiscovery(conn net.Conn, reply discoveryReply, reached chan<- string) error {
	var envelope [20]byte
	if _, err := io.ReadFull(conn, envelope[:]); err != nil {
		return err
	}
	if !net.IP(envelope[:16]).Equal(net.ParseIP(discoveryAddress)) || binary.LittleEndian.Uint32(envelope[16:]) != discoveryPort {
		return fmt.Errorf("discovery changed the verified TUN endpoint")
	}
	preface := make([]byte, len(http2.ClientPreface))
	if _, err := io.ReadFull(conn, preface); err != nil {
		return err
	}
	if string(preface) != http2.ClientPreface {
		return fmt.Errorf("invalid HTTP2 preface")
	}
	stop := func(phase string) (bool, error) {
		if reply.stopAt != phase {
			return false, nil
		}
		reached <- phase
		return true, waitDiscoveryClose(conn)
	}
	if stopped, err := stop("http2"); stopped {
		return err
	}
	framer := http2.NewFramer(conn, conn)
	if err := framer.WriteSettings(); err != nil {
		return err
	}
	for _, stream := range []uint32{1, 3, 1} {
		if _, err := xpc.DecodeMessage(&discoveryStreamReader{framer: framer, stream: stream}); err != nil {
			return err
		}
		if stopped, err := stop("xpc"); stopped {
			return err
		}
		if reply.stopAt == "malformed-xpc" {
			if err := framer.WriteData(stream, false, []byte("invalid XPC")); err != nil {
				return err
			}
			return waitDiscoveryClose(conn)
		}
		if err := writeDiscoveryXPC(framer, stream, nil); err != nil {
			return err
		}
	}
	if stopped, err := stop("handshake"); stopped {
		return err
	}
	services := make(map[string]interface{}, len(reply.services))
	for name, port := range reply.services {
		services[name] = map[string]interface{}{"Port": strconv.FormatUint(uint64(port), 10)}
	}
	if err := writeDiscoveryXPC(framer, 1, map[string]interface{}{
		"MessageType": "Handshake", "Services": services,
		"Properties": map[string]interface{}{"UniqueDeviceID": reply.peer, "OSVersion": "27.0"},
	}); err != nil {
		return err
	}
	return waitDiscoveryClose(conn)
}

func waitDiscoveryClose(conn net.Conn) error {
	_, err := io.Copy(io.Discard, conn)
	// Closing with unread startup bytes can send RST rather than FIN; both
	// prove closure. A deadline or any other error must still fail the fixture.
	if errors.Is(err, syscall.ECONNRESET) {
		return nil
	}
	return err
}

type discoveryStreamReader struct {
	framer  *http2.Framer
	stream  uint32
	pending []byte
}

func (r *discoveryStreamReader) Read(p []byte) (int, error) {
	for len(r.pending) == 0 {
		frame, err := r.framer.ReadFrame()
		if err != nil {
			return 0, err
		}
		if data, ok := frame.(*http2.DataFrame); ok {
			if data.StreamID != r.stream {
				return 0, fmt.Errorf("unexpected XPC startup stream %d", data.StreamID)
			}
			r.pending = data.Data()
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func writeDiscoveryXPC(framer *http2.Framer, stream uint32, body map[string]interface{}) error {
	var buffer bytes.Buffer
	if err := xpc.EncodeMessage(&buffer, xpc.Message{Flags: xpc.AlwaysSetFlag | xpc.DataFlag, Body: body}); err != nil {
		return err
	}
	return framer.WriteData(stream, false, buffer.Bytes())
}

func TestCaptureDiscoveryRefreshesImmutableDevice(t *testing.T) {
	const service = "com.apple.dt.testmanagerd.remote"
	session, _ := discoveryFixture(t,
		discoveryReply{peer: discoveryPeer, services: map[string]uint32{service: 62010}},
		discoveryReply{peer: discoveryPeer, services: map[string]uint32{service: 62011}},
		discoveryReply{peer: discoveryPeer},
	)
	base := session.device
	var previous ios.DeviceEntry
	for _, port := range []int{62010, 62011, 0} {
		device, err := session.captureDevice(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if device.Rsd.GetPort(service) != port || device.Rsd.GetPort("cached-only") != 0 {
			t.Fatal("operation reused or merged the acquisition map")
		}
		if device.Rsd.(ios.RsdHandshakeResponse).OSVersion != "27.0" {
			t.Fatal("operation retained a stale OS version")
		}
		if device.Address != base.Address || device.Properties != base.Properties || device.UserspaceTUNPort != base.UserspaceTUNPort || !device.UserspaceTUN {
			t.Fatal("fresh discovery changed the device/transport identity")
		}
		if previous.Rsd != nil && previous.Rsd.GetPort(service) != 62010 {
			t.Fatal("subsequent discovery mutated an earlier operation map")
		}
		if previous.Rsd == nil {
			previous = device
		}
	}
	if !reflect.DeepEqual(session.device, base) || base.Rsd.GetPort("cached-only") != 62002 || base.Rsd.GetPort(service) != 0 {
		t.Fatal("discovery mutated the acquisition-time base")
	}
}

func TestCaptureOperationsUseFreshMissingServices(t *testing.T) {
	for _, operation := range []string{"screenshot", "tests"} {
		t.Run(operation, func(t *testing.T) {
			session, _ := discoveryFixture(t, discoveryReply{peer: discoveryPeer})
			session.device.Rsd = ios.RsdHandshakeResponse{Services: map[string]ios.RsdServiceEntry{
				"com.apple.instruments.dtservicehub":   {Port: 62020},
				"com.apple.dt.testmanagerd.remote":     {Port: 62021},
				"com.apple.coredevice.appservice":      {Port: 62022},
				"com.apple.coredevice.openstdiosocket": {Port: 62023},
			}}
			var err error
			want := "capture_service_unavailable"
			if operation == "screenshot" {
				_, err = session.Screenshot(context.Background())
			} else {
				_, err = session.RunTests(context.Background(), TestRunRequest{}, io.Discard, t.TempDir())
				want = "test_service_unavailable"
			}
			if errorCode(err) != want {
				t.Fatalf("fresh missing service did not stop the operation: %v", err)
			}
		})
	}
}

func TestCaptureDiscoveryRejectsDifferentPeerWithoutReplacingSession(t *testing.T) {
	session, _ := discoveryFixture(t, discoveryReply{peer: "other-phone"}, discoveryReply{peer: discoveryPeer})
	base := session.device
	if _, err := session.captureDevice(context.Background()); errorCode(err) != "rsd_identity_mismatch" {
		t.Fatalf("mismatched peer accepted: %v", err)
	}
	if _, err := session.captureDevice(context.Background()); err != nil {
		t.Fatalf("identity failure damaged the existing forwarder: %v", err)
	}
	if !reflect.DeepEqual(session.device, base) {
		t.Fatal("identity failure replaced base device")
	}
}

func TestCaptureDiscoveryCancellationClosesStartupAndHandshake(t *testing.T) {
	for _, phase := range []string{"http2", "xpc", "handshake"} {
		t.Run(phase, func(t *testing.T) {
			session, reached := discoveryFixture(t, discoveryReply{stopAt: phase})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			returned := make(chan error, 1)
			go func() { _, err := session.captureDevice(ctx); returned <- err }()
			select {
			case <-reached:
			case <-time.After(time.Second):
				t.Fatal("discovery did not reach the intended blocking phase")
			}
			cancel()
			select {
			case err := <-returned:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation was lost: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("cancelled discovery remained blocked")
			}
		})
	}
}

func TestCaptureDiscoveryConstructorFailureClosesSocket(t *testing.T) {
	session, _ := discoveryFixture(t, discoveryReply{stopAt: "malformed-xpc"})
	if _, err := session.captureDevice(context.Background()); errorCode(err) != "rsd_discovery_failed" {
		t.Fatalf("invalid constructor response accepted: %v", err)
	}
}

func TestCaptureDiscoveryPreCancelledDoesNotDial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discoverDevice(ctx, ios.DeviceEntry{}, "invalid endpoint", 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled discovery did not stop before dial: %v", err)
	}
}

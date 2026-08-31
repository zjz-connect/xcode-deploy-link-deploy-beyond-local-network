package linkcore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testConfig(t *testing.T) (Config, string) {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "nodus-remote-deploy-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	pairRecord := filepath.Join(directory, "remote.plist")
	if err := os.WriteFile(pairRecord, []byte("pair record"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.DeviceLabel = "development iphone"
	config.RemoteIdentifier = "00008140-001A2D3602A0801C"
	config.TargetTailnetIP = "100.91.2.3"
	config.PairRecordPath = pairRecord
	return config, filepath.Join(directory, "iphone.json")
}

func TestConfigRoundTripIsOwnerOnly(t *testing.T) {
	config, profilePath := testConfig(t)
	if err := WriteConfig(profilePath, config); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("profile mode = %#o, want 0600", got)
	}
	loaded, err := LoadConfig(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != config {
		t.Fatalf("loaded config = %#v, want %#v", loaded, config)
	}
}

func TestConfigRejectsNonTailnetAddress(t *testing.T) {
	config, _ := testConfig(t)
	config.TargetTailnetIP = "203.0.113.10"
	if err := config.Validate(); errorCode(err) != "profile_invalid" {
		t.Fatalf("error = %v, want profile_invalid", err)
	}
}

func TestConfigRejectsGroupReadablePairRecord(t *testing.T) {
	config, _ := testConfig(t)
	if err := os.Chmod(config.PairRecordPath, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePairRecord(config); errorCode(err) != "pair_record_permissions" {
		t.Fatalf("error = %v, want pair_record_permissions", err)
	}
}

func TestLoadConfigRejectsUnknownAndTrailingJSON(t *testing.T) {
	config, profilePath := testConfig(t)
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	unknown := strings.TrimSuffix(string(data), "}") + `,"unexpected":true}`
	if err := os.WriteFile(profilePath, []byte(unknown), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(profilePath); errorCode(err) != "profile_invalid" {
		t.Fatalf("unknown field error = %v, want profile_invalid", err)
	}
	if err := os.WriteFile(profilePath, append(data, []byte("\n{}\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(profilePath); errorCode(err) != "profile_invalid" {
		t.Fatalf("trailing object error = %v, want profile_invalid", err)
	}
}

func TestSocketPathIsProfileScoped(t *testing.T) {
	path, err := SocketPath("/tmp/iphone.json")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/tmp/iphone.sock" {
		t.Fatalf("socket path = %q", path)
	}
}

func TestIsTailnetAddress(t *testing.T) {
	for _, address := range []string{"100.64.0.1", "100.127.255.254", "fd7a:115c:a1e0::1234"} {
		if !IsTailnetAddress(address) {
			t.Fatalf("%s should be accepted", address)
		}
	}
	for _, address := range []string{"100.128.0.1", "192.168.1.1", "invalid"} {
		if IsTailnetAddress(address) {
			t.Fatalf("%s should be rejected", address)
		}
	}
}

func TestValidateBuildMetadata(t *testing.T) {
	valid := BuildMetadata{
		Version:        ModuleVersion,
		LinkCoreCommit: PinnedLinkCoreCommit,
		PatchSHA256:    strings.Repeat("a", 64),
	}
	if err := validateBuildMetadata(valid); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.Version = "dev"
	if err := validateBuildMetadata(invalid); errorCode(err) != "build_metadata_invalid" {
		t.Fatalf("error = %v, want build_metadata_invalid", err)
	}
	invalid = valid
	invalid.PatchSHA256 = strings.Repeat("g", 64)
	if err := validateBuildMetadata(invalid); errorCode(err) != "build_metadata_invalid" {
		t.Fatalf("error = %v, want build_metadata_invalid", err)
	}
}

func TestTailnetPingAcceptsDERPReachability(t *testing.T) {
	arguments := tailnetPingArguments("100.91.2.3")
	joined := strings.Join(arguments, " ")
	if !strings.Contains(joined, "--until-direct=false") {
		t.Fatalf("ping arguments must treat DERP as reachable: %q", joined)
	}
}

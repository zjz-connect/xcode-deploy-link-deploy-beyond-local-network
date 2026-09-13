package main

import (
	"encoding/json"
	"github.com/zjz-connect/ios-ota/internal/linkcore"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigureLocationCommandCreatesPrivateConnection(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(root, "iphone.json")
	output := filepath.Join(root, "connection.json")
	config := linkcore.DefaultConfig()
	config.DeviceLabel = "test phone"
	config.RemoteIdentifier = "test-remote-identifier"
	config.TargetTailnetIP = "100.64.0.2"
	config.PairRecordPath = filepath.Join(root, "pair.plist")
	if err := linkcore.WriteConfig(profile, config); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"configure-location", "--profile", profile, "--listen", "100.64.0.1:61443", "--output", output}); err != nil {
		t.Fatal(err)
	}
	configured, err := linkcore.LoadConfig(profile)
	if err != nil || configured.Location == nil {
		t.Fatal("location configuration missing", err)
	}
	for _, path := range []string{output, configured.Location.Credentials} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("connection must be owner-only")
		}
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var connection linkcore.LocationConnection
	if err := json.Unmarshal(data, &connection); err != nil {
		t.Fatal(err)
	}
	if connection.URL != "https://100.64.0.1:61443" || len(connection.Token) != 43 || len(connection.CertificateSHA256) != 64 {
		t.Fatal("invalid connection document")
	}
	if err := run([]string{"configure-location", "--profile", profile, "--listen", "100.64.0.1:61443", "--output", output}); err == nil {
		t.Fatal("existing credential overwritten")
	}
}

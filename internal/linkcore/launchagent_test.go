package linkcore

import (
	"path/filepath"
	"reflect"
	"testing"

	"howett.net/plist"
)

func TestLaunchAgentCanonicalIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	binary := "/Users/test/.local/bin/lyo-nodus-ios-ota"
	profile := "/Users/test/Library/Application Support/Owner & Device/iphone.json"
	data, err := launchAgentContents(binary, profile)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if _, err := plist.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if ServiceName != "Lyo Nodus iOS OTA" || ServiceLabel != "lyo-nodus-ios-ota" || fields["Label"] != ServiceLabel {
		t.Fatalf("noncanonical service identity: %v", fields["Label"])
	}
	if filepath.Base(LaunchAgentPath()) != ServiceLabel+".plist" {
		t.Fatal(LaunchAgentPath())
	}
	if !reflect.DeepEqual(fields["ProgramArguments"], []any{binary, "serve", "--profile", profile}) {
		t.Fatalf("profile or executable changed: %v", fields["ProgramArguments"])
	}
	logs := filepath.Join(filepath.Dir(profile), "logs")
	if fields["StandardOutPath"] != filepath.Join(logs, ServiceLabel+".log") || fields["StandardErrorPath"] != filepath.Join(logs, ServiceLabel+".error.log") {
		t.Fatal("log names must use the canonical slug")
	}
	if fields["RunAtLoad"] != true || fields["KeepAlive"] != true || fields["ProcessType"] != "Interactive" {
		t.Fatal("naming must preserve the daemon's existing scheduling")
	}
	for _, key := range []string{"DisplayName", "AssociatedBundleIdentifiers", "BundleProgram"} {
		if _, ok := fields[key]; ok {
			t.Fatalf("unexpected app wrapper metadata: %s", key)
		}
	}
}

func TestLaunchAgentRejectsRelativePaths(t *testing.T) {
	for _, paths := range [][2]string{{"relative", "/tmp/phone.json"}, {"/tmp/agent", "phone.json"}} {
		if _, err := launchAgentContents(paths[0], paths[1]); errorCode(err) != "launch_agent_invalid" {
			t.Fatalf("relative path accepted: %v", paths)
		}
	}
}

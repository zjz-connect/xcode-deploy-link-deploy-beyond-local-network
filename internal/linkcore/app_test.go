package linkcore

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidateAppAcceptsSignedBundle(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("codesign bundle validation is macOS-only")
	}
	directory := t.TempDir()
	appPath := filepath.Join(directory, "DeployLinkTest.app")
	if err := os.Mkdir(appPath, 0o755); err != nil {
		t.Fatal(err)
	}
	infoPlist := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>DeployLinkTest</string>
<key>CFBundleIdentifier</key><string>one.zjz.deploy-link-test</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleVersion</key><string>1</string>
</dict></plist>`)
	if err := os.WriteFile(filepath.Join(appPath, "Info.plist"), infoPlist, 0o644); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(appPath, "DeployLinkTest")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", appPath).CombinedOutput(); err != nil {
		t.Fatalf("codesign: %v: %s", err, output)
	}
	bundle, err := ValidateApp(context.Background(), appPath)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.BundleIdentifier != "one.zjz.deploy-link-test" || bundle.Path != appPath {
		t.Fatalf("bundle = %#v", bundle)
	}
}

func TestValidateAppRejectsUnsignedBundle(t *testing.T) {
	appPath := filepath.Join(t.TempDir(), "Unsigned.app")
	if err := os.Mkdir(appPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appPath, "Info.plist"), []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>one.zjz.unsigned</string></dict></plist>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateApp(context.Background(), appPath); errorCode(err) != "app_invalid" {
		t.Fatalf("error = %v, want app_invalid", err)
	}
}

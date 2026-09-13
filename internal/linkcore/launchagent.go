package linkcore

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

const launchAgentLabel = "ios-ota"

func LaunchAgentPath() string {
	return filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", launchAgentLabel+".plist")
}

func xmlEscape(value string) string {
	var buffer bytes.Buffer
	_ = xml.EscapeText(&buffer, []byte(value))
	return buffer.String()
}

func launchTarget() string {
	return "gui/" + strconv.Itoa(os.Getuid())
}

func launchServiceTarget() string {
	return launchTarget() + "/" + launchAgentLabel
}

func InstallLaunchAgent(binaryPath string, profilePath string) error {
	if !filepath.IsAbs(binaryPath) || !filepath.IsAbs(profilePath) {
		return coded("launch_agent_invalid", "binary and profile paths must be absolute", nil)
	}
	logsDirectory := filepath.Join(filepath.Dir(profilePath), "logs")
	if err := os.MkdirAll(logsDirectory, 0o700); err != nil {
		return coded("launch_agent_failed", "could not create log directory", err)
	}
	contents := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>serve</string>
    <string>--profile</string>
    <string>%s</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ThrottleInterval</key>
  <integer>10</integer>
  <key>ProcessType</key>
  <string>Interactive</string>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, launchAgentLabel, xmlEscape(binaryPath), xmlEscape(profilePath),
		xmlEscape(filepath.Join(logsDirectory, "ios-ota.log")),
		xmlEscape(filepath.Join(logsDirectory, "ios-ota.error.log")))
	path := LaunchAgentPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return coded("launch_agent_failed", "could not create LaunchAgents directory", err)
	}
	if err := writeAtomic(path, []byte(contents), 0o644); err != nil {
		return coded("launch_agent_failed", "could not write LaunchAgent", err)
	}
	_ = exec.Command("/bin/launchctl", "bootout", launchServiceTarget()).Run()
	if output, err := exec.Command("/bin/launchctl", "bootstrap", launchTarget(), path).CombinedOutput(); err != nil {
		return coded("launch_agent_failed", fmt.Sprintf("launchctl bootstrap failed (%d diagnostic bytes)", len(output)), err)
	}
	if output, err := exec.Command("/bin/launchctl", "kickstart", "-k", launchServiceTarget()).CombinedOutput(); err != nil {
		return coded("launch_agent_failed", fmt.Sprintf("launchctl kickstart failed (%d diagnostic bytes)", len(output)), err)
	}
	return nil
}

func RemoveLaunchAgent() error {
	path := LaunchAgentPath()
	_ = exec.Command("/bin/launchctl", "bootout", launchServiceTarget()).Run()
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return coded("launch_agent_failed", "could not remove LaunchAgent plist", err)
	}
	return nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	parent := filepath.Dir(path)
	temporary, err := os.CreateTemp(parent, ".launch-agent-*.plist")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	committed = true
	return nil
}

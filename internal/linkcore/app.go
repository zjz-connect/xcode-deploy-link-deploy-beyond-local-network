package linkcore

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"howett.net/plist"
)

type AppBundle struct {
	Path             string
	BundleIdentifier string
}

type appInfoPlist struct {
	BundleIdentifier string `plist:"CFBundleIdentifier"`
}

func ValidateApp(ctx context.Context, path string) (AppBundle, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return AppBundle{}, coded("app_invalid", "could not resolve app path", err)
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() || !strings.HasSuffix(strings.ToLower(absolute), ".app") {
		return AppBundle{}, coded("app_invalid", "path must be an existing .app directory", err)
	}
	infoData, err := os.ReadFile(filepath.Join(absolute, "Info.plist"))
	if err != nil {
		return AppBundle{}, coded("app_invalid", "Info.plist is missing", err)
	}
	var appInfo appInfoPlist
	if _, err := plist.Unmarshal(infoData, &appInfo); err != nil || appInfo.BundleIdentifier == "" {
		return AppBundle{}, coded("app_invalid", "Info.plist has no bundle identifier", err)
	}
	command := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--deep", "--strict", absolute)
	if output, err := command.CombinedOutput(); err != nil {
		return AppBundle{}, coded("app_invalid", fmt.Sprintf("code signature verification failed (%d bytes of diagnostic output)", len(output)), err)
	}
	return AppBundle{Path: absolute, BundleIdentifier: appInfo.BundleIdentifier}, nil
}

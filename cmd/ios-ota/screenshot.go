package main

import (
	"context"
	"errors"
	"flag"
	"path/filepath"
	"time"

	"github.com/zjz-connect/ios-ota/internal/linkcore"
)

func screenshot(arguments []string) error {
	flags := flag.NewFlagSet("screenshot", flag.ContinueOnError)
	profile := flags.String("profile", "", "existing owner-only profile path")
	output := flags.String("output", "", "new PNG output file")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("screenshot does not accept positional arguments")
	}
	if err := required(*profile, "--profile"); err != nil {
		return err
	}
	if err := required(*output, "--output"); err != nil {
		return err
	}
	if _, err := linkcore.LoadConfig(*profile); err != nil {
		return err
	}
	absolute, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	var result linkcore.Response
	err = linkcore.Call(ctx, *profile, linkcore.Request{Command: "screenshot"}, func(response linkcore.Response) { result = response })
	if err != nil {
		return err
	}
	width, height, err := linkcore.SaveScreenshot(absolute, result.ScreenshotPNG)
	if err != nil {
		return err
	}
	emit(map[string]any{"ok": true, "event": "screenshot_saved", "path": absolute, "width": width, "height": height, "generation": result.Generation})
	return nil
}

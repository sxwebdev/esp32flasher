//go:build !darwin && !windows

package main

import "errors"

func canInstallUpdates() bool {
	return false
}

func updateAssetExtension() string {
	return ""
}

func applyDownloadedUpdate(string) error {
	return errors.New("automatic updates are only supported on macOS and Windows")
}

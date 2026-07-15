//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func canInstallUpdates() bool {
	appPath, err := macOSAppPath()
	if err != nil {
		return false
	}
	probe, err := os.CreateTemp(filepath.Dir(appPath), ".espflasher-update-*")
	if err != nil {
		return false
	}
	probe.Close()
	return os.Remove(probe.Name()) == nil
}

func updateAssetExtension() string {
	return ".zip"
}

func applyDownloadedUpdate(packagePath string) error {
	appPath, err := macOSAppPath()
	if err != nil {
		return err
	}
	workingDirectory := filepath.Dir(packagePath)
	stagingDirectory := filepath.Join(workingDirectory, "staging")
	if err := os.Mkdir(stagingDirectory, 0o700); err != nil {
		return fmt.Errorf("create update staging directory: %w", err)
	}
	if output, err := exec.Command("/usr/bin/ditto", "-x", "-k", packagePath, stagingDirectory).CombinedOutput(); err != nil {
		return fmt.Errorf("unpack macOS update: %w: %s", err, strings.TrimSpace(string(output)))
	}

	newAppPath := filepath.Join(stagingDirectory, filepath.Base(appPath))
	if info, err := os.Stat(newAppPath); err != nil || !info.IsDir() {
		return errors.New("macOS update does not contain the expected application bundle")
	}
	if err := verifyMatchingCodeSignature(appPath, newAppPath); err != nil {
		return err
	}

	scriptPath := filepath.Join(workingDirectory, "install.sh")
	backupPath := appPath + ".previous"
	script := "#!/bin/sh\n" +
		"while kill -0 " + fmt.Sprint(os.Getpid()) + " 2>/dev/null; do sleep 1; done\n" +
		"rm -rf " + shellQuote(backupPath) + "\n" +
		"if mv " + shellQuote(appPath) + " " + shellQuote(backupPath) + " && mv " + shellQuote(newAppPath) + " " + shellQuote(appPath) + "; then\n" +
		"  /usr/bin/open -n " + shellQuote(appPath) + "\n" +
		"  rm -rf " + shellQuote(backupPath) + "\n" +
		"else\n" +
		"  if [ -d " + shellQuote(backupPath) + " ]; then mv " + shellQuote(backupPath) + " " + shellQuote(appPath) + "; fi\n" +
		"  /usr/bin/open -n " + shellQuote(appPath) + "\n" +
		"fi\n" +
		"rm -rf " + shellQuote(workingDirectory) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return fmt.Errorf("create macOS update helper: %w", err)
	}
	if err := exec.Command("/bin/sh", scriptPath).Start(); err != nil {
		return fmt.Errorf("start macOS update helper: %w", err)
	}
	return nil
}

func macOSAppPath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate application: %w", err)
	}
	path := filepath.Clean(executable)
	components := strings.Split(path, string(filepath.Separator))
	for index := len(components) - 1; index >= 0; index-- {
		if strings.HasSuffix(components[index], ".app") {
			appPath := strings.Join(components[:index+1], string(filepath.Separator))
			if !filepath.IsAbs(appPath) {
				appPath = string(filepath.Separator) + appPath
			}
			if filepath.Clean(filepath.Join(appPath, "Contents", "MacOS")) == filepath.Dir(path) {
				return appPath, nil
			}
		}
	}
	return "", errors.New("automatic updates require the application bundle, not a development executable")
}

func verifyMatchingCodeSignature(currentApp, newApp string) error {
	// Unsigned development builds remain usable. Once releases are signed, never
	// replace a signed app with an unsigned or invalidly signed bundle.
	if err := exec.Command("/usr/bin/codesign", "--verify", "--deep", "--strict", currentApp).Run(); err != nil {
		return nil
	}
	if err := exec.Command("/usr/bin/codesign", "--verify", "--deep", "--strict", newApp).Run(); err != nil {
		return errors.New("downloaded macOS update has an invalid code signature")
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\\"'\\\"'") + "'"
}

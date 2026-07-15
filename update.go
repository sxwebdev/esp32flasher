package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	githubReleaseAPI = "https://api.github.com/repos/sxwebdev/esp32flasher/releases/latest"
	maxUpdateSize    = 512 << 20 // 512 MiB is well above the expected desktop packages.
)

var errUpdateDownloadNotHTTPS = errors.New("update download does not use HTTPS")

// appVersion is set from a release tag with -ldflags. Development builds keep
// the default value and may check for updates, but cannot install one.
var appVersion = "0.0.0-dev"

// GetAppVersion returns the version embedded into the release build.
func (a *App) GetAppVersion() string {
	return appVersion
}

type UpdateInfo struct {
	Available  bool   `json:"available"`
	CanInstall bool   `json:"canInstall"`
	Version    string `json:"version"`
	Notes      string `json:"notes"`
}

type githubRelease struct {
	TagName    string         `json:"tag_name"`
	Body       string         `json:"body"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type updateCandidate struct {
	version string
	notes   string
	asset   releaseAsset
}

// CheckForUpdate asks GitHub for the newest stable release. Network failures
// are returned to the caller so the UI can quietly leave the update control
// hidden without affecting normal flashing.
func (a *App) CheckForUpdate() (UpdateInfo, error) {
	return checkForUpdate(context.Background(), newUpdateHTTPClient(), githubReleaseAPI, appVersion, expectedUpdateAsset(), canInstallAutomaticUpdate())
}

// InstallUpdate downloads the same release the user was shown, schedules the
// platform installer, and then exits this process. The helper launched by the
// platform implementation replaces the files only after this process has gone.
func (a *App) InstallUpdate() error {
	a.updateMu.Lock()
	defer a.updateMu.Unlock()

	if !canInstallAutomaticUpdate() {
		return errors.New("automatic updates are unavailable for this installation")
	}

	candidate, err := findUpdate(context.Background(), newUpdateHTTPClient(), githubReleaseAPI, appVersion, expectedUpdateAsset())
	if err != nil {
		return err
	}
	if candidate == nil {
		return errors.New("no newer compatible release is available")
	}

	assetPath, err := downloadUpdate(context.Background(), newUpdateHTTPClient(), candidate.asset)
	if err != nil {
		return err
	}
	if err := applyDownloadedUpdate(assetPath); err != nil {
		_ = os.RemoveAll(filepath.Dir(assetPath))
		return err
	}

	// applyDownloadedUpdate has handed off to a separate process. Its work
	// starts only after this application exits, avoiding locked binaries on both
	// supported platforms.
	go func() {
		time.Sleep(150 * time.Millisecond)
		wailsruntime.Quit(a.ctx)
	}()
	return nil
}

func canInstallAutomaticUpdate() bool {
	return appVersion != "0.0.0-dev" && canInstallUpdates()
}

func checkForUpdate(ctx context.Context, client *http.Client, endpoint, currentVersion, assetName string, canInstall bool) (UpdateInfo, error) {
	candidate, err := findUpdate(ctx, client, endpoint, currentVersion, assetName)
	if err != nil || candidate == nil {
		return UpdateInfo{}, err
	}
	return UpdateInfo{
		Available:  true,
		CanInstall: canInstall,
		Version:    candidate.version,
		Notes:      candidate.notes,
	}, nil
}

func findUpdate(ctx context.Context, client *http.Client, endpoint, currentVersion, assetName string) (*updateCandidate, error) {
	current, err := parseVersion(currentVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid application version %q: %w", currentVersion, err)
	}

	release, err := fetchLatestRelease(ctx, client, endpoint)
	if err != nil {
		return nil, err
	}
	if release.Draft || release.Prerelease {
		return nil, nil
	}

	latest, err := parseVersion(release.TagName)
	if err != nil {
		return nil, fmt.Errorf("latest release has invalid version %q: %w", release.TagName, err)
	}
	if latest.compare(current) <= 0 {
		return nil, nil
	}

	for _, asset := range release.Assets {
		if asset.Name == assetName {
			if asset.Size <= 0 || asset.Size > maxUpdateSize {
				return nil, fmt.Errorf("release asset %q has an invalid size", asset.Name)
			}
			if !isHTTPSURL(asset.BrowserDownloadURL) {
				return nil, fmt.Errorf("release asset %q does not use HTTPS", asset.Name)
			}
			return &updateCandidate{version: release.TagName, notes: release.Body, asset: asset}, nil
		}
	}

	return nil, nil // The newest release has no package for this platform yet.
}

func fetchLatestRelease(ctx context.Context, client *http.Client, endpoint string) (githubRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return githubRelease{}, fmt.Errorf("create update request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "espflasher-updater")

	response, err := client.Do(request)
	if err != nil {
		return githubRelease{}, fmt.Errorf("check GitHub releases: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("check GitHub releases: unexpected status %s", response.Status)
	}

	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&release); err != nil {
		return githubRelease{}, fmt.Errorf("decode GitHub release: %w", err)
	}
	return release, nil
}

func downloadUpdate(ctx context.Context, client *http.Client, asset releaseAsset) (string, error) {
	if !isHTTPSURL(asset.BrowserDownloadURL) {
		return "", errUpdateDownloadNotHTTPS
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("create update download request: %w", err)
	}

	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download update: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download update: unexpected status %s", response.Status)
	}
	if response.ContentLength > maxUpdateSize {
		return "", errors.New("update package is too large")
	}

	directory, err := os.MkdirTemp("", "espflasher-update-*")
	if err != nil {
		return "", fmt.Errorf("create update directory: %w", err)
	}
	path := filepath.Join(directory, asset.Name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		_ = os.RemoveAll(directory)
		return "", fmt.Errorf("create update package: %w", err)
	}

	written, copyErr := io.Copy(file, io.LimitReader(response.Body, maxUpdateSize+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.RemoveAll(directory)
		return "", fmt.Errorf("save update package: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.RemoveAll(directory)
		return "", fmt.Errorf("close update package: %w", closeErr)
	}
	if written <= 0 || written > maxUpdateSize || (asset.Size > 0 && written != asset.Size) {
		_ = os.RemoveAll(directory)
		return "", errors.New("downloaded update package has an unexpected size")
	}

	return path, nil
}

func newUpdateHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			if request.URL.Scheme != "https" {
				return errors.New("update download redirected to a non-HTTPS URL")
			}
			return nil
		},
	}
}

func expectedUpdateAsset() string {
	return "espflasher-" + goruntime.GOOS + "-" + goruntime.GOARCH + updateAssetExtension()
}

func isHTTPSURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

type version struct {
	major      int
	minor      int
	patch      int
	preRelease string
}

func parseVersion(raw string) (version, error) {
	value := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	if value == "" {
		return version{}, errors.New("version is empty")
	}
	value, _, _ = strings.Cut(value, "+")
	core, preRelease, _ := strings.Cut(value, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return version{}, errors.New("version must have major, minor, and patch numbers")
	}
	parsed := version{preRelease: preRelease}
	for index, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return version{}, errors.New("invalid numeric version component")
		}
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return version{}, errors.New("invalid numeric version component")
		}
		switch index {
		case 0:
			parsed.major = number
		case 1:
			parsed.minor = number
		case 2:
			parsed.patch = number
		}
	}
	return parsed, nil
}

func (left version) compare(right version) int {
	for _, numbers := range [][2]int{{left.major, right.major}, {left.minor, right.minor}, {left.patch, right.patch}} {
		if numbers[0] < numbers[1] {
			return -1
		}
		if numbers[0] > numbers[1] {
			return 1
		}
	}
	if left.preRelease == right.preRelease {
		return 0
	}
	if left.preRelease == "" {
		return 1
	}
	if right.preRelease == "" {
		return -1
	}
	if left.preRelease < right.preRelease {
		return -1
	}
	return 1
}

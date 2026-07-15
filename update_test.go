package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParseVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    version
		wantErr bool
	}{
		{name: "release tag", input: "v1.2.3", want: version{major: 1, minor: 2, patch: 3}},
		{name: "pre-release and metadata", input: "1.2.3-rc.1+build.9", want: version{major: 1, minor: 2, patch: 3, preRelease: "rc.1"}},
		{name: "incomplete", input: "1.2", wantErr: true},
		{name: "leading zero", input: "01.2.3", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseVersion(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatal("parseVersion() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseVersion() error = %v", err)
			}
			if got != test.want {
				t.Errorf("parseVersion() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestCheckForUpdate(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/latest" {
			t.Errorf("request path = %q, want /latest", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
  "tag_name": "v1.3.0",
  "body": "Bug fixes",
  "assets": [{
    "name": "espflasher-darwin-arm64.zip",
    "browser_download_url": "https://github.com/sxwebdev/esp32flasher/releases/download/v1.3.0/espflasher-darwin-arm64.zip",
    "size": 42
  }]
}`))
	}))
	t.Cleanup(server.Close)

	got, err := checkForUpdate(t.Context(), server.Client(), server.URL+"/latest", "v1.2.3", "espflasher-darwin-arm64.zip", true)
	if err != nil {
		t.Fatalf("checkForUpdate() error = %v", err)
	}
	if !got.Available || !got.CanInstall {
		t.Errorf("checkForUpdate() flags = %#v, want available installable update", got)
	}
	if got.Version != "v1.3.0" || got.Notes != "Bug fixes" {
		t.Errorf("checkForUpdate() = %#v, want release metadata", got)
	}
}

func TestFindUpdateSkipsIncompatibleAsset(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"tag_name":"v1.3.0","assets":[]}`))
	}))
	t.Cleanup(server.Close)

	got, err := findUpdate(t.Context(), server.Client(), server.URL, "v1.2.3", "espflasher-windows-amd64-installer.exe")
	if err != nil {
		t.Fatalf("findUpdate() error = %v", err)
	}
	if got != nil {
		t.Errorf("findUpdate() = %#v, want nil without platform asset", got)
	}
}

func TestVersionCompare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  version
		right version
		want  int
	}{
		{name: "major upgrade", left: version{major: 2}, right: version{major: 1, minor: 9, patch: 9}, want: 1},
		{name: "older patch", left: version{major: 1, minor: 2, patch: 2}, right: version{major: 1, minor: 2, patch: 3}, want: -1},
		{name: "release after pre-release", left: version{major: 1, minor: 2, patch: 3}, right: version{major: 1, minor: 2, patch: 3, preRelease: "rc.1"}, want: 1},
		{name: "same version", left: version{major: 1, minor: 2, patch: 3}, right: version{major: 1, minor: 2, patch: 3}, want: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := test.left.compare(test.right)
			if got != test.want {
				t.Errorf("compare() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestDownloadUpdate(t *testing.T) {
	t.Parallel()

	payload := []byte("release package")
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/package" {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write(payload)
	}))
	t.Cleanup(server.Close)

	path, err := downloadUpdate(t.Context(), server.Client(), releaseAsset{
		Name:               "espflasher-darwin-arm64.zip",
		BrowserDownloadURL: server.URL + "/package",
		Size:               int64(len(payload)),
	})
	if err != nil {
		t.Fatalf("downloadUpdate() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(path)) })

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read downloaded package: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("downloaded package = %q, want %q", got, payload)
	}
}

func TestDownloadUpdateRejectsNonHTTPS(t *testing.T) {
	t.Parallel()

	_, err := downloadUpdate(t.Context(), http.DefaultClient, releaseAsset{
		Name:               "espflasher-windows-amd64-installer.exe",
		BrowserDownloadURL: "http://example.com/update.exe",
		Size:               1,
	})
	if !errors.Is(err, errUpdateDownloadNotHTTPS) {
		t.Errorf("downloadUpdate() error = %v, want HTTPS validation error", err)
	}
}

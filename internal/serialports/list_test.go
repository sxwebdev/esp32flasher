package serialports

import (
	"errors"
	"slices"
	"testing"
)

func TestDiscoverCombinesSourcesWhenOneMissesDevice(t *testing.T) {
	_ = t.Context()

	registry := func() ([]string, error) { return []string{"COM3"}, nil }
	setupAPI := func() ([]string, error) { return []string{"COM12", "COM3"}, nil }

	got, err := discover(registry, setupAPI)
	if err != nil {
		t.Fatalf("discover() error = %v", err)
	}
	if want := []string{"COM3", "COM12"}; !slices.Equal(got, want) {
		t.Fatalf("discover() = %v, want %v", got, want)
	}
}

func TestDiscoverKeepsWorkingWhenOneSourceFails(t *testing.T) {
	_ = t.Context()

	sourceErr := errors.New("registry unavailable")
	failing := func() ([]string, error) { return nil, sourceErr }
	working := func() ([]string, error) { return []string{"COM7"}, nil }

	got, err := discover(failing, working)
	if err != nil {
		t.Fatalf("discover() error = %v", err)
	}
	if want := []string{"COM7"}; !slices.Equal(got, want) {
		t.Fatalf("discover() = %v, want %v", got, want)
	}
}

func TestDiscoverReportsFailureWhenEverySourceFails(t *testing.T) {
	_ = t.Context()

	registryErr := errors.New("registry unavailable")
	setupAPIErr := errors.New("SetupAPI unavailable")

	_, err := discover(
		func() ([]string, error) { return nil, registryErr },
		func() ([]string, error) { return nil, setupAPIErr },
	)
	if !errors.Is(err, registryErr) || !errors.Is(err, setupAPIErr) {
		t.Fatalf("discover() error = %v, want both source errors", err)
	}
}

func TestNormalizeRemovesEmptyAndCaseInsensitiveDuplicates(t *testing.T) {
	_ = t.Context()

	got := normalize([]string{" COM10 ", "COM2", "com2", "", " /dev/cu.usbserial "})
	want := []string{"/dev/cu.usbserial", "COM2", "COM10"}
	if !slices.Equal(got, want) {
		t.Fatalf("normalize() = %v, want %v", got, want)
	}
}

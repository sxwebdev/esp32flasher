package serialports

import (
	"errors"
	"slices"
	"testing"
)

func TestFromNames(t *testing.T) {
	_ = t.Context()

	names := []string{"COM3", "COM12"}
	got, err := fromNames(names, nil)
	if err != nil {
		t.Fatalf("fromNames() error = %v", err)
	}
	want := []Port{{Name: "COM3"}, {Name: "COM12"}}
	if !slices.Equal(got, want) {
		t.Fatalf("fromNames() = %v, want %v", got, want)
	}
}

func TestFromNamesPreservesErrorIdentity(t *testing.T) {
	_ = t.Context()

	sourceErr := errors.New("enumeration failed")
	ports, err := fromNames([]string{"COM3"}, sourceErr)
	if ports != nil || !errors.Is(err, sourceErr) {
		t.Fatalf("fromNames() = (%v, %v), want (nil, source error)", ports, err)
	}
}

func TestDiscoverCombinesAndEnrichesSources(t *testing.T) {
	_ = t.Context()

	setupAPI := func() ([]Port, error) {
		return []Port{{Name: "COM12", Description: "USB-SERIAL CH340 (COM12)"}}, nil
	}
	registry := func() ([]Port, error) { return []Port{{Name: "COM3"}}, nil }
	dosDevices := func() ([]Port, error) { return []Port{{Name: "COM12"}, {Name: "COM3"}}, nil }

	got, err := discover(setupAPI, registry, dosDevices)
	if err != nil {
		t.Fatalf("discover() error = %v", err)
	}
	want := []Port{{Name: "COM3"}, {Name: "COM12", Description: "USB-SERIAL CH340"}}
	if !slices.Equal(got, want) {
		t.Fatalf("discover() = %v, want %v", got, want)
	}
}

func TestDiscoverKeepsWorkingWhenTwoSourcesFail(t *testing.T) {
	_ = t.Context()

	sourceErr := errors.New("source unavailable")
	failing := func() ([]Port, error) { return nil, sourceErr }
	working := func() ([]Port, error) { return []Port{{Name: "COM7"}}, nil }

	got, err := discover(failing, failing, working)
	if err != nil {
		t.Fatalf("discover() error = %v", err)
	}
	if want := []Port{{Name: "COM7"}}; !slices.Equal(got, want) {
		t.Fatalf("discover() = %v, want %v", got, want)
	}
}

func TestDiscoverReportsFailureWhenEverySourceFails(t *testing.T) {
	_ = t.Context()

	registryErr := errors.New("registry unavailable")
	setupAPIErr := errors.New("SetupAPI unavailable")
	dosDeviceErr := errors.New("DOS devices unavailable")

	_, err := discover(
		func() ([]Port, error) { return nil, registryErr },
		func() ([]Port, error) { return nil, setupAPIErr },
		func() ([]Port, error) { return nil, dosDeviceErr },
	)
	if !errors.Is(err, registryErr) || !errors.Is(err, setupAPIErr) || !errors.Is(err, dosDeviceErr) {
		t.Fatalf("discover() error = %v, want every source error", err)
	}
}

func TestNormalizeRemovesEmptyAndCaseInsensitiveDuplicates(t *testing.T) {
	_ = t.Context()

	got := normalize([]Port{
		{Name: " COM10 "},
		{Name: "COM2"},
		{Name: "com2", Description: "CP210x (com2)"},
		{},
		{Name: " /dev/cu.usbserial "},
	})
	want := []Port{
		{Name: "/dev/cu.usbserial"},
		{Name: "COM2", Description: "CP210x"},
		{Name: "COM10"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("normalize() = %v, want %v", got, want)
	}
}

func TestCleanDescriptionRemovesUnhelpfulValues(t *testing.T) {
	_ = t.Context()

	tests := []struct {
		name        string
		description string
		port        string
		want        string
	}{
		{name: "empty", description: "  ", port: "COM3", want: ""},
		{name: "port name only", description: "com3", port: "COM3", want: ""},
		{name: "friendly name", description: " CP210x USB UART ", port: "COM3", want: "CP210x USB UART"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = t.Context()
			if got := cleanDescription(tt.description, tt.port); got != tt.want {
				t.Fatalf("cleanDescription() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSplitMultiString(t *testing.T) {
	_ = t.Context()

	got := splitMultiString([]uint16{'C', 'O', 'M', '3', 0, 'C', 'O', 'M', '1', '2', 0, 0})
	want := []string{"COM3", "COM12"}
	if !slices.Equal(got, want) {
		t.Fatalf("splitMultiString() = %v, want %v", got, want)
	}
}

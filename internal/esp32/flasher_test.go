package esp32

import (
	"encoding/binary"
	"errors"
	"slices"
	"testing"
	"time"
)

func TestFlashEndLeavesROMRunningUntilHardwareReset(t *testing.T) {
	t.Context()
	response := slipEncode([]byte{0x01, ESP_FLASH_END, 0, 0, 0, 0, 0, 0, 0, 0})
	port := &fakeSerialPort{readData: response}
	flasher := &ESP32Flasher{port: port}

	if err := flasher.flashEnd(); err != nil {
		t.Fatalf("flashEnd() error = %v", err)
	}
	if len(port.writes) != 1 {
		t.Fatalf("writes = %d, want 1", len(port.writes))
	}
	packet, err := slipDecode(port.writes[0])
	if err != nil {
		t.Fatalf("decode FLASH_END packet: %v", err)
	}
	if got := binary.LittleEndian.Uint32(packet[8:12]); got != 1 {
		t.Fatalf("FLASH_END stay-in-loader flag = %d, want 1", got)
	}
}

func TestRebootTargetUsesOneStableHardReset(t *testing.T) {
	t.Context()
	port := &fakeSerialPort{}
	flasher := &ESP32Flasher{port: port}

	if err := flasher.rebootTarget(func(delay time.Duration) {
		port.actions = append(port.actions, "WAIT="+delay.String())
	}); err != nil {
		t.Fatalf("rebootTarget() error = %v", err)
	}
	want := []string{
		"DTR=false",
		"RTS=false",
		"WAIT=50ms",
		"RTS=true",
		"WAIT=100ms",
		"RTS=false",
		"WAIT=4s",
		"DTR=false",
		"RTS=false",
	}
	if !slices.Equal(port.actions, want) {
		t.Fatalf("control line sequence = %v, want %v", port.actions, want)
	}
	if wantBaudRates := []int{115200}; !slices.Equal(port.baudRates, wantBaudRates) {
		t.Fatalf("baud rates = %v, want %v", port.baudRates, wantBaudRates)
	}
}

func TestRebootTargetStopsWhenResetCannotBeAsserted(t *testing.T) {
	t.Context()
	port := &fakeSerialPort{failAction: "RTS=true"}
	flasher := &ESP32Flasher{port: port}

	err := flasher.rebootTarget(func(time.Duration) {})
	if !errors.Is(err, errControlLine) {
		t.Fatalf("rebootTarget() error = %v, want %v", err, errControlLine)
	}
	want := []string{"DTR=false", "RTS=false", "RTS=true"}
	if !slices.Equal(port.actions, want) {
		t.Fatalf("actions after failure = %v, want %v", port.actions, want)
	}
}

func TestRebootTargetRefreshesDTRAfterRTSOnWindows(t *testing.T) {
	t.Context()
	port := &fakeSerialPort{}
	flasher := &ESP32Flasher{port: port, refreshDTRAfterRTS: true}

	if err := flasher.rebootTarget(func(delay time.Duration) {
		port.actions = append(port.actions, "WAIT="+delay.String())
	}); err != nil {
		t.Fatalf("rebootTarget() error = %v", err)
	}
	want := []string{
		"DTR=false",
		"RTS=false",
		"DTR=false",
		"WAIT=50ms",
		"RTS=true",
		"DTR=false",
		"WAIT=100ms",
		"RTS=false",
		"DTR=false",
		"WAIT=4s",
		"DTR=false",
		"RTS=false",
		"DTR=false",
	}
	if !slices.Equal(port.actions, want) {
		t.Fatalf("control line sequence = %v, want %v", port.actions, want)
	}
}

func TestTightResetUsesCP2102CompatibleStateTransitions(t *testing.T) {
	t.Context()
	tests := []struct {
		name      string
		bootDelay time.Duration
	}{
		{name: "normal delay", bootDelay: 50 * time.Millisecond},
		{name: "slow CP2102 delay", bootDelay: 550 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Context()
			port := &fakeSerialPort{}
			flasher := &ESP32Flasher{port: port, modemControl: &fakeModemControl{port: port}}

			err := flasher.tightResetWithSleepAndDelay(func(delay time.Duration) {
				port.actions = append(port.actions, "WAIT="+delay.String())
			}, tt.bootDelay)
			if err != nil {
				t.Fatalf("tightResetWithSleepAndDelay() error = %v", err)
			}

			want := []string{
				"DTR=false,RTS=false",
				"DTR=true,RTS=true",
				"DTR=false,RTS=true",
				"WAIT=100ms",
				"DTR=true,RTS=false",
				"WAIT=" + tt.bootDelay.String(),
				"DTR=false,RTS=false",
				"DTR=false",
			}
			if !slices.Equal(port.actions, want) {
				t.Fatalf("control line sequence = %v, want %v", port.actions, want)
			}
		})
	}
}

func TestTightResetStopsOnAtomicControlError(t *testing.T) {
	t.Context()
	port := &fakeSerialPort{failAction: "DTR=false,RTS=true"}
	flasher := &ESP32Flasher{port: port, modemControl: &fakeModemControl{port: port}}

	err := flasher.tightResetWithSleepAndDelay(func(time.Duration) {}, 50*time.Millisecond)
	if !errors.Is(err, errControlLine) {
		t.Fatalf("tightResetWithSleepAndDelay() error = %v, want %v", err, errControlLine)
	}
	want := []string{
		"DTR=false,RTS=false",
		"DTR=true,RTS=true",
		"DTR=false,RTS=true",
	}
	if !slices.Equal(port.actions, want) {
		t.Fatalf("actions after failure = %v, want %v", port.actions, want)
	}
}

func TestCloseReleasesAtomicControlBeforeSerialPort(t *testing.T) {
	t.Context()
	port := &fakeSerialPort{}
	flasher := &ESP32Flasher{port: port, modemControl: &fakeModemControl{port: port}}

	if err := flasher.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	want := []string{"CONTROL_CLOSE", "PORT_CLOSE"}
	if !slices.Equal(port.actions, want) {
		t.Fatalf("close order = %v, want %v", port.actions, want)
	}
}

func TestClassicResetUsesEspressifControlLineSequence(t *testing.T) {
	t.Context()
	port := &fakeSerialPort{}
	flasher := &ESP32Flasher{port: port}

	err := flasher.classicReset(func(delay time.Duration) {
		port.actions = append(port.actions, "WAIT="+delay.String())
	})
	if err != nil {
		t.Fatalf("classicReset() error = %v", err)
	}

	want := []string{
		"DTR=false",
		"RTS=true",
		"WAIT=100ms",
		"DTR=true",
		"RTS=false",
		"WAIT=50ms",
		"DTR=false",
		"WAIT=50ms",
	}
	if !slices.Equal(port.actions, want) {
		t.Fatalf("control line sequence = %v, want %v", port.actions, want)
	}
}

func TestClassicResetStopsOnControlLineError(t *testing.T) {
	t.Context()
	port := &fakeSerialPort{failAction: "RTS=true"}
	flasher := &ESP32Flasher{port: port}

	err := flasher.classicReset(func(time.Duration) {})
	if !errors.Is(err, errControlLine) {
		t.Fatalf("classicReset() error = %v, want %v", err, errControlLine)
	}
	want := []string{"DTR=false", "RTS=true"}
	if !slices.Equal(port.actions, want) {
		t.Fatalf("actions after failure = %v, want %v", port.actions, want)
	}
}

func TestClassicResetRefreshesDTRAfterRTSOnWindows(t *testing.T) {
	t.Context()
	port := &fakeSerialPort{}
	flasher := &ESP32Flasher{port: port, refreshDTRAfterRTS: true}

	err := flasher.classicReset(func(delay time.Duration) {
		port.actions = append(port.actions, "WAIT="+delay.String())
	})
	if err != nil {
		t.Fatalf("classicReset() error = %v", err)
	}

	want := []string{
		"DTR=false",
		"RTS=true",
		"DTR=false",
		"WAIT=100ms",
		"DTR=true",
		"RTS=false",
		"DTR=true",
		"WAIT=50ms",
		"DTR=false",
		"WAIT=50ms",
	}
	if !slices.Equal(port.actions, want) {
		t.Fatalf("control line sequence = %v, want %v", port.actions, want)
	}
}

func TestSetRTSReportsWindowsDTRRefreshError(t *testing.T) {
	t.Context()
	port := &fakeSerialPort{failAction: "DTR=false"}
	flasher := &ESP32Flasher{port: port, refreshDTRAfterRTS: true}

	err := flasher.setRTS(true, false)
	if !errors.Is(err, errControlLine) {
		t.Fatalf("setRTS() error = %v, want %v", err, errControlLine)
	}
	want := []string{"RTS=true", "DTR=false"}
	if !slices.Equal(port.actions, want) {
		t.Fatalf("control line sequence = %v, want %v", port.actions, want)
	}
}

func TestClassicResetSupportsExtendedBootDelay(t *testing.T) {
	t.Context()
	port := &fakeSerialPort{}
	flasher := &ESP32Flasher{port: port}

	err := flasher.classicResetWithDelay(func(delay time.Duration) {
		port.actions = append(port.actions, "WAIT="+delay.String())
	}, 550*time.Millisecond)
	if err != nil {
		t.Fatalf("classicResetWithDelay() error = %v", err)
	}

	want := []string{
		"DTR=false",
		"RTS=true",
		"WAIT=100ms",
		"DTR=true",
		"RTS=false",
		"WAIT=550ms",
		"DTR=false",
		"WAIT=50ms",
	}
	if !slices.Equal(port.actions, want) {
		t.Fatalf("control line sequence = %v, want %v", port.actions, want)
	}
}

func TestDirectResetUsesDirectWiringSequence(t *testing.T) {
	t.Context()
	port := &fakeSerialPort{}
	flasher := &ESP32Flasher{port: port}

	err := flasher.directResetWithSleep(func(delay time.Duration) {
		port.actions = append(port.actions, "WAIT="+delay.String())
	})
	if err != nil {
		t.Fatalf("directResetWithSleep() error = %v", err)
	}

	want := []string{
		"DTR=false",
		"RTS=false",
		"WAIT=50ms",
		"DTR=true",
		"WAIT=50ms",
		"RTS=true",
		"WAIT=100ms",
		"RTS=false",
		"WAIT=50ms",
		"DTR=false",
		"WAIT=50ms",
	}
	if !slices.Equal(port.actions, want) {
		t.Fatalf("control line sequence = %v, want %v", port.actions, want)
	}
}

func TestDirectResetStopsOnControlLineError(t *testing.T) {
	t.Context()
	port := &fakeSerialPort{failAction: "RTS=true"}
	flasher := &ESP32Flasher{port: port}

	err := flasher.directResetWithSleep(func(time.Duration) {})
	if !errors.Is(err, errControlLine) {
		t.Fatalf("directResetWithSleep() error = %v, want %v", err, errControlLine)
	}
	want := []string{"DTR=false", "RTS=false", "DTR=true", "RTS=true"}
	if !slices.Equal(port.actions, want) {
		t.Fatalf("actions after failure = %v, want %v", port.actions, want)
	}
}

func TestFlashSizeForImage(t *testing.T) {
	t.Context()
	tests := []struct {
		name    string
		offset  uint32
		size    uint32
		want    uint32
		wantErr bool
	}{
		{name: "application uses common minimum", offset: 0x10000, size: 0x100000, want: 4 * 1024 * 1024},
		{name: "full 4 MB image", size: 4 * 1024 * 1024, want: 4 * 1024 * 1024},
		{name: "8 MB image", size: 8 * 1024 * 1024, want: 8 * 1024 * 1024},
		{name: "range needs 16 MB", offset: 0x10000, size: 8 * 1024 * 1024, want: 16 * 1024 * 1024},
		{name: "range exceeds maximum", offset: 1, size: 16 * 1024 * 1024, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := flashSizeForImage(tt.offset, tt.size)
			if (err != nil) != tt.wantErr {
				t.Fatalf("flashSizeForImage() error = %v, wantErr %t", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("flashSizeForImage() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSetHostBaudRateChangesOpenPortMode(t *testing.T) {
	t.Context()
	port := &fakeSerialPort{}
	flasher := &ESP32Flasher{port: port}

	if err := flasher.setHostBaudRate(921600); err != nil {
		t.Fatalf("setHostBaudRate() error = %v", err)
	}
	want := []int{921600}
	if !slices.Equal(port.baudRates, want) {
		t.Fatalf("baud rates = %v, want %v", port.baudRates, want)
	}
}

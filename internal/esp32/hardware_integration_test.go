package esp32

import (
	"espflasher/internal/firmware"
	"fmt"
	"os"
	"testing"
)

type hardwareTestReporter struct {
	lastProgress int
}

func (r *hardwareTestReporter) emitProgress(progress int, message string) {
	if progress == r.lastProgress {
		return
	}
	r.lastProgress = progress
	if progress%5 == 0 || progress >= 95 {
		fmt.Printf("[%3d%%] %s\n", progress, message)
	}
}

func (*hardwareTestReporter) emitLog(message string) {
	fmt.Println(message)
}

// TestFlashESP32Hardware only runs when ESP32_FLASH_PORT is explicitly set.
// A normal go test invocation never touches a connected board.
func TestFlashESP32Hardware(t *testing.T) {
	t.Context()

	portName := os.Getenv("ESP32_FLASH_PORT")
	if portName == "" {
		t.Skip("set ESP32_FLASH_PORT to enable the hardware test")
	}
	imagePath := os.Getenv("ESP32_FLASH_IMAGE")
	if imagePath == "" {
		imagePath = "../../testdata/esp32_rx_hardworker_latest.merged.bin"
	}

	data, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatalf("read firmware %q: %v", imagePath, err)
	}
	image := firmware.Detect(imagePath, data)
	if !image.Full || image.Offset != 0 {
		t.Fatalf("hardware test requires a full merged image, classification = %+v", image)
	}

	reporter := &hardwareTestReporter{lastProgress: -1}
	fmt.Printf("Firmware: %s (%d bytes), offset 0x%x\n", imagePath, len(data), image.Offset)
	fmt.Printf("Port: %s\n", portName)

	flasher, err := New(portName, &Callbacks{
		Progress: reporter.emitProgress,
		Log:      reporter.emitLog,
	})
	if err != nil {
		t.Fatalf("connect to ROM bootloader: %v", err)
	}
	defer func() {
		if err := flasher.Close(); err != nil {
			t.Errorf("close serial port: %v", err)
		}
	}()
	if os.Getenv("ESP32_REBOOT_ONLY") == "1" {
		if err := flasher.RebootTarget(); err != nil {
			t.Fatalf("reboot target: %v", err)
		}
		return
	}

	if os.Getenv("ESP32_PROBE_ONLY") == "1" {
		if err := flasher.sync(); err != nil {
			t.Fatalf("probe sync: %v", err)
		}
		if err := flasher.detectChip(); err != nil {
			t.Fatalf("probe chip detection: %v", err)
		}
		if err := flasher.spiAttach(); err != nil {
			t.Fatalf("probe SPI attach: %v", err)
		}
		flashSize, err := flashSizeForImage(image.Offset, uint32(len(data)))
		if err != nil {
			t.Fatalf("probe flash size: %v", err)
		}
		if err := flasher.spiSetParameters(flashSize); err != nil {
			t.Fatalf("probe SPI parameters: %v", err)
		}
		fmt.Printf("Probe successful: %s\n", flasher.chipType)
		return
	}

	if err := flasher.Flash(data, image.Offset); err != nil {
		t.Fatalf("flash firmware: %v", err)
	}
	if err := flasher.RebootTarget(); err != nil {
		t.Fatalf("reboot target: %v", err)
	}
}

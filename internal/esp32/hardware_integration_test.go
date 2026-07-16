package esp32

import (
	"bytes"
	"espflasher/internal/firmware"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type hardwareTestReporter struct {
	lastProgress int
}

func (r *hardwareTestReporter) emitProgress(progress int, message ProgressMessage) {
	if progress == r.lastProgress {
		return
	}
	r.lastProgress = progress
	if progress%5 == 0 || progress >= 95 {
		fmt.Printf("[%3d%%] %s\n", progress, message.Key)
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
		imagePath = "testdata/esp32_rx_hardworker_latest.merged.bin"
	}
	imagePath = resolveHardwareImagePath(imagePath)

	data, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatalf("read firmware %q: %v", imagePath, err)
	}
	image := firmware.Detect(imagePath, data)

	reporter := &hardwareTestReporter{lastProgress: -1}
	imageKind := "application"
	if image.Full {
		imageKind = "merged"
	}
	fmt.Printf("Firmware: %s (%s, %d bytes), offset 0x%x\n", imagePath, imageKind, len(data), image.Offset)
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
		prepareHardwareBootVerification(t, flasher)
		if err := flasher.RebootTarget(); err != nil {
			t.Fatalf("reboot target: %v", err)
		}
		verifyHardwareApplicationBoot(t, flasher)
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
	prepareHardwareBootVerification(t, flasher)
	if err := flasher.RebootTarget(); err != nil {
		t.Fatalf("reboot target: %v", err)
	}
	verifyHardwareApplicationBoot(t, flasher)
}

func prepareHardwareBootVerification(t *testing.T, flasher *ESP32Flasher) {
	t.Helper()
	if os.Getenv("ESP32_VERIFY_BOOT") != "1" {
		return
	}
	flasher.rxBuffer = nil
	if err := flasher.port.ResetInputBuffer(); err != nil {
		t.Fatalf("clear UART before reboot verification: %v", err)
	}
}

func verifyHardwareApplicationBoot(t *testing.T, flasher *ESP32Flasher) {
	t.Helper()
	if os.Getenv("ESP32_VERIFY_BOOT") != "1" {
		return
	}
	if err := flasher.port.SetReadTimeout(200 * time.Millisecond); err != nil {
		t.Fatalf("set boot verification timeout: %v", err)
	}

	duration := 4 * time.Second
	if os.Getenv("ESP32_VERIFY_UPTIME") == "1" {
		duration = 35 * time.Second
	}
	deadline := time.Now().Add(duration)
	buffer := make([]byte, 1024)
	var output []byte
	for time.Now().Before(deadline) {
		n, err := flasher.port.Read(buffer)
		if n > 0 {
			output = append(output, buffer[:n]...)
		}
		if err != nil {
			t.Fatalf("read application boot output: %v", err)
		}
	}

	resetCount := bytes.Count(output, []byte("rst:"))
	fastBoot := bytes.Contains(output, []byte("boot:0x13 (SPI_FAST_FLASH_BOOT)"))
	applicationBanner := bytes.Contains(output, []byte("ESP32 BLDC Rover"))
	gModeCount := bytes.Count(output, []byte("g mode\r\n"))
	fmt.Printf("Reset verification over %s: resets=%d, fast_boot=%t, application_banner=%t, g_mode_lines=%d, uart_bytes=%d\n",
		duration, resetCount, fastBoot, applicationBanner, gModeCount, len(output))
	if resetCount != 1 {
		t.Fatalf("observed %d reset banners after one RebootTarget call, want exactly 1", resetCount)
	}
	if !fastBoot {
		t.Fatal("normal SPI Flash boot banner was not received")
	}
	if !applicationBanner {
		t.Fatal("application banner was not received")
	}
}

func resolveHardwareImagePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return filepath.Join("..", "..", path)
}

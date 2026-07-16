package main

import (
	"context"
	"espflasher/internal/esp32"
	"espflasher/internal/firmware"
	"espflasher/internal/serialports"
	"fmt"
	"os"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx context.Context

	monitorControlMu sync.Mutex
	monitorMu        sync.Mutex
	monitor          *monitorSession

	progressMu   sync.Mutex
	progressSet  bool
	lastProgress int

	updateMu sync.Mutex
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// ListPorts returns the available serial ports.
func (a *App) ListPorts() ([]string, error) {
	return serialports.List()
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// ChooseFile opens the firmware file picker.
func (a *App) ChooseFile() (string, error) {
	filePath, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select a firmware file",
		Filters: []runtime.FileFilter{
			{
				DisplayName: "Firmware Files",
				Pattern:     "*.bin",
			},
		},
	})

	return filePath, err
}

// emitProgress sends flashing progress to the frontend.
func (a *App) emitProgress(progress int, message string) {
	a.progressMu.Lock()
	if a.progressSet && progress == a.lastProgress {
		a.progressMu.Unlock()
		return
	}
	a.progressSet = true
	a.lastProgress = progress
	a.progressMu.Unlock()

	runtime.EventsEmit(a.ctx, "flash-progress", map[string]any{
		"progress": progress,
		"message":  message,
	})
}

func (a *App) resetProgress() {
	a.progressMu.Lock()
	a.progressSet = false
	a.progressMu.Unlock()
}

// emitLog sends a log message to the frontend.
func (a *App) emitLog(message string) {
	runtime.EventsEmit(a.ctx, "flash-log", message)
}

func (a *App) flasherCallbacks() *esp32.Callbacks {
	return &esp32.Callbacks{
		Progress: a.emitProgress,
		Log:      a.emitLog,
	}
}

// Flash writes a full merged image at 0x0 or an application image at 0x10000.
func (a *App) Flash(portName, filePath string) error {
	a.resetProgress()

	// Check that the image still exists before opening the serial port.
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", filePath)
	}

	a.emitProgress(0, "Starting flash...")
	a.emitLog("🔄 Initializing...")

	// Load the complete image into memory.
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	a.emitProgress(10, "Firmware loaded")
	a.emitLog(fmt.Sprintf("📄 Loaded firmware: %d bytes", len(data)))
	image := firmware.Detect(filePath, data)
	if image.Full {
		a.emitLog("💾 Full merged image detected: Flash will be overwritten from address 0x0")
	} else {
		a.emitLog("📦 Application image detected: writing from address 0x10000")
	}

	a.emitProgress(20, "Connecting to ESP32...")
	a.emitLog("🔗 Connecting to ESP32...")

	flasher, err := esp32.New(portName, a.flasherCallbacks())
	if err != nil {
		a.emitLog("❌ Could not enter the ROM bootloader automatically")
		a.emitLog("💡 Hold BOOT, press and release EN/RESET, release BOOT, then start flashing again")
		return fmt.Errorf("failed to connect to ESP32 ROM bootloader: %w", err)
	}
	defer flasher.Close()

	// Write the image while reporting progress.
	if err := flasher.Flash(data, image.Offset); err != nil {
		a.emitProgress(0, "Flash failed")
		return fmt.Errorf("failed to flash: %w", err)
	}

	// Reboot into normal execution only after ROM-side verification succeeds.
	if err := flasher.RebootTarget(); err != nil {
		return fmt.Errorf("failed to reboot ESP32 after flashing: %w", err)
	}

	a.emitProgress(100, "Flashing complete!")
	a.emitLog("✅ Firmware flashed successfully!")
	a.emitLog("💡 Flash has been restored; the ESP32 should now boot normally")

	return nil
}

// FlashWithRetry retries the complete flash operation.
func (a *App) FlashWithRetry(portName, filePath string, maxAttempts int) error {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		a.emitLog(fmt.Sprintf("🔄 Flash attempt %d/%d", attempt, maxAttempts))

		err := a.Flash(portName, filePath)
		if err == nil {
			return nil
		}

		a.emitLog(fmt.Sprintf("❌ Attempt %d failed: %v", attempt, err))

		if attempt < maxAttempts {
			a.emitLog("⏳ Waiting before the next attempt...")
		}
	}

	return fmt.Errorf("flashing failed after %d attempts", maxAttempts)
}

// FlashMultipleFiles writes a set of images at explicit offsets.
func (a *App) FlashMultipleFiles(portName string, files map[string]uint32) error {
	// For example: {"bootloader.bin": 0x1000, "app.bin": 0x10000, "partitions.bin": 0x8000}.

	a.emitLog("🔄 Multiple-image flash mode...")

	// Reuse a single ROM bootloader connection for every image.
	flasher, err := esp32.New(portName, a.flasherCallbacks())
	if err != nil {
		// Fall back to a board that the user has manually put into download mode.
		a.emitLog("⚠️ Switching to manual bootloader mode for multiple images...")
		flasher, err = esp32.NewManual(portName, a.flasherCallbacks())
		if err != nil {
			return fmt.Errorf("failed to create flasher: %w", err)
		}
	}
	defer flasher.Close()

	// Negotiate a faster transfer rate when possible.
	flasher.SetBaudRate(460800)

	// Write each image at its caller-provided offset.
	fileCount := 0
	totalFiles := len(files)

	for filename, offset := range files {
		fileCount++
		a.emitLog(fmt.Sprintf("📄 Flashing file %d/%d: %s -> 0x%x", fileCount, totalFiles, filename, offset))

		data, err := os.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", filename, err)
		}

		if err := flasher.Flash(data, offset); err != nil {
			return fmt.Errorf("failed to flash %s: %w", filename, err)
		}
	}

	if err := flasher.RebootTarget(); err != nil {
		return fmt.Errorf("failed to reboot ESP32 after flashing: %w", err)
	}
	a.emitLog("✅ All files were flashed successfully!")
	return nil
}

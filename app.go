package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	serialport "go.bug.st/serial"
)

// App struct
type App struct {
	ctx         context.Context
	monitorPort serialport.Port
	stopMonitor chan bool
	lineBuffer  string // Buffer for accumulating incomplete lines
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// ListPorts returns list of available COM ports
func (a *App) ListPorts() ([]string, error) {
	return serialport.GetPortsList()
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// ChooseFile opens file selection dialog
func (a *App) ChooseFile() (string, error) {
	filePath, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select firmware file",
		Filters: []runtime.FileFilter{
			{
				DisplayName: "Firmware Files",
				Pattern:     "*.bin",
			},
		},
	})

	return filePath, err
}

// emitProgress sends progress to frontend
func (a *App) emitProgress(progress int, message string) {
	runtime.EventsEmit(a.ctx, "flash-progress", map[string]interface{}{
		"progress": progress,
		"message":  message,
	})
}

// emitLog sends log message to frontend
func (a *App) emitLog(message string) {
	runtime.EventsEmit(a.ctx, "flash-log", message)
}

// FlashFile represents a file to flash with its address
type FlashFile struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
}

// FlashMultiple writes multiple firmware files to ESP32 at specified addresses
func (a *App) FlashMultiple(portName string, files []FlashFile) error {
	if len(files) == 0 {
		return fmt.Errorf("no files to flash")
	}

	// Check all files exist
	for _, f := range files {
		if _, err := os.Stat(f.Path); os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", f.Path)
		}
	}

	a.emitProgress(0, "Starting flash...")
	a.emitLog(fmt.Sprintf("🔄 Flashing %d file(s)...", len(files)))

	// Create ESP32 flasher
	a.emitProgress(10, "Connecting to ESP32...")
	a.emitLog("🔗 Connecting to ESP32...")

	flasher, err := NewESP32FlasherWithProgress(portName, a)
	if err != nil {
		return fmt.Errorf("failed to create flasher: %w", err)
	}
	defer flasher.Close()

	// Flash each file
	totalFiles := len(files)
	for i, f := range files {
		// Read file
		data, err := os.ReadFile(f.Path)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", f.Path, err)
		}

		a.emitLog(fmt.Sprintf("📄 [%d/%d] %s: %d bytes @ 0x%X", i+1, totalFiles, f.Path, len(data), f.Offset))

		// Flash data
		if err := flasher.FlashData(data, uint32(f.Offset), portName); err != nil {
			a.emitProgress(0, "Flash error")
			return fmt.Errorf("failed to flash %s: %w", f.Path, err)
		}
	}

	return nil
}

// Flash writes firmware to ESP32 at specified address using built-in esptool implementation
func (a *App) Flash(portName, filePath string, offset int) error {
	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", filePath)
	}

	a.emitProgress(0, "Starting flash...")
	a.emitLog(fmt.Sprintf("🔄 Initializing... Address: 0x%X", offset))

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	a.emitProgress(10, "File loaded")
	a.emitLog(fmt.Sprintf("📄 File loaded: %d bytes (%.2f KB)", len(data), float64(len(data))/1024))

	// Create ESP32 flasher
	a.emitProgress(20, "Connecting to ESP32...")
	a.emitLog("🔗 Connecting to ESP32...")

	flasher, err := NewESP32FlasherWithProgress(portName, a)
	if err != nil {
		return fmt.Errorf("failed to create flasher: %w", err)
	}
	defer flasher.Close()

	// Flash data with progress (starts at 30%)
	if err := flasher.FlashData(data, uint32(offset), portName); err != nil {
		a.emitProgress(0, "Flash error")
		return fmt.Errorf("failed to flash: %w", err)
	}

	return nil
}

// MonitorPort creates connection to port for monitoring
func (a *App) MonitorPort(portName string, baudRate int) error {
	// If already monitoring, stop it first
	if a.monitorPort != nil {
		a.StopMonitor()
	}

	// Open port for monitoring with retry logic
	// After flashing, the port may need time to be released by the OS
	mode := &serialport.Mode{
		BaudRate: baudRate,
		Parity:   serialport.NoParity,
		DataBits: 8,
		StopBits: serialport.OneStopBit,
	}

	var port serialport.Port
	var err error
	for range 5 {
		port, err = serialport.Open(portName, mode)
		if err == nil {
			break
		}
		// Wait before retry - port may still be held by previous operation
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		return fmt.Errorf("failed to open port for monitoring: %w", err)
	}

	a.monitorPort = port
	a.stopMonitor = make(chan bool, 1)
	a.lineBuffer = "" // Clear line buffer

	// Clear any garbage in the input buffer
	port.ResetInputBuffer()

	a.emitLog(fmt.Sprintf("🔍 Starting monitor on %s (%d baud)", portName, baudRate))
	a.emitLog("💡 Press 'Stop' to stop monitoring")

	// Start goroutine to read data
	go func() {
		defer func() {
			// Safe port close in goroutine
			if a.monitorPort != nil {
				a.monitorPort.Close()
				a.monitorPort = nil
			}
		}()

		buffer := make([]byte, 256) // Smaller buffer to reduce processing load

		for {
			select {
			case <-a.stopMonitor:
				return
			default:
				// Check if port is still open
				if a.monitorPort == nil {
					return
				}

				// Set read timeout
				if err := a.monitorPort.SetReadTimeout(50 * time.Millisecond); err != nil {
					return
				}

				n, err := a.monitorPort.Read(buffer)
				if err != nil {
					// If timeout - continue
					if strings.Contains(err.Error(), "timeout") {
						continue
					}
					// Check for "bad file descriptor" - just stop without error
					if strings.Contains(err.Error(), "bad file descriptor") ||
						strings.Contains(err.Error(), "file already closed") {
						return
					}
					// For other errors - send to log and stop
					runtime.EventsEmit(a.ctx, "monitor-error", err.Error())
					return
				}

				if n > 0 {
					// Filter out non-printable characters
					// Keep: newline, carriage return, tab, printable ASCII (32-126), and UTF-8 (128-255)
					filtered := make([]byte, 0, n)
					for _, b := range buffer[:n] {
						if b == '\n' || b == '\r' || b == '\t' || (b >= 32 && b < 127) || b >= 128 {
							filtered = append(filtered, b)
						}
					}

					if len(filtered) == 0 {
						continue
					}

					// Add filtered data to buffer
					a.lineBuffer += string(filtered)

					// Process all complete lines
					for {
						newlineIdx := strings.Index(a.lineBuffer, "\n")
						if newlineIdx == -1 {
							// No complete lines, wait for more data
							break
						}

						// Extract complete line
						line := a.lineBuffer[:newlineIdx]
						a.lineBuffer = a.lineBuffer[newlineIdx+1:]

						// Remove extra \r and send line only if not empty
						line = strings.TrimSpace(line)
						if line != "" {
							runtime.EventsEmit(a.ctx, "monitor-data", line)
						}
					}

					// If buffer gets too large without \n, send as is and clear
					if len(a.lineBuffer) > 500 {
						line := strings.TrimSpace(a.lineBuffer)
						if line != "" {
							runtime.EventsEmit(a.ctx, "monitor-data", line)
						}
						a.lineBuffer = ""
					}
				}
			}
		}
	}()

	return nil
}

// StopMonitor stops port monitoring
func (a *App) StopMonitor() {
	// Send stop signal first
	if a.stopMonitor != nil {
		select {
		case a.stopMonitor <- true:
		default:
			// Channel already closed or full
		}
	}

	// Close port immediately - this will cause Read() to return error and exit goroutine
	if a.monitorPort != nil {
		a.monitorPort.Close()
		a.monitorPort = nil
	}

	// Now safe to close channel
	if a.stopMonitor != nil {
		close(a.stopMonitor)
		a.stopMonitor = nil
	}

	a.lineBuffer = "" // Clear line buffer

	runtime.EventsEmit(a.ctx, "monitor-stop", "")
	a.emitLog("⏹️ Monitor stopped")
}

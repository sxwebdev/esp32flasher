package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	serialport "go.bug.st/serial"
)

// MonitorPort opens a serial connection and streams complete lines to the frontend.
func (a *App) MonitorPort(portName string, baudRate int) error {
	if a.monitorPort != nil {
		a.StopMonitor()
	}

	mode := &serialport.Mode{
		BaudRate: baudRate,
		Parity:   serialport.NoParity,
		DataBits: 8,
		StopBits: serialport.OneStopBit,
	}

	port, err := serialport.Open(portName, mode)
	if err != nil {
		return fmt.Errorf("failed to open port for monitoring: %w", err)
	}

	a.monitorPort = port
	a.stopMonitor = make(chan bool, 1)
	a.lineBuffer = ""

	a.emitLog(fmt.Sprintf("🔍 Monitoring port %s at %d baud", portName, baudRate))
	a.emitLog("💡 Press Stop to end serial monitoring")

	go func() {
		defer func() {
			if a.monitorPort != nil {
				a.monitorPort.Close()
				a.monitorPort = nil
			}
		}()

		buffer := make([]byte, 1024)

		for {
			select {
			case <-a.stopMonitor:
				return
			default:
				if a.monitorPort == nil {
					return
				}

				if err := a.monitorPort.SetReadTimeout(50 * time.Millisecond); err != nil {
					return
				}

				n, err := a.monitorPort.Read(buffer)
				if err != nil {
					if strings.Contains(err.Error(), "timeout") {
						continue
					}
					if strings.Contains(err.Error(), "bad file descriptor") ||
						strings.Contains(err.Error(), "file already closed") {
						return
					}
					runtime.EventsEmit(a.ctx, "monitor-error", err.Error())
					return
				}

				if n > 0 {
					a.lineBuffer += string(buffer[:n])

					for {
						newlineIdx := strings.Index(a.lineBuffer, "\n")
						if newlineIdx == -1 {
							break
						}

						line := a.lineBuffer[:newlineIdx]
						a.lineBuffer = a.lineBuffer[newlineIdx+1:]
						line = strings.TrimSpace(line)
						if line != "" {
							runtime.EventsEmit(a.ctx, "monitor-data", line)
						}
					}

					if len(a.lineBuffer) > 1000 {
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

// StopMonitor stops serial monitoring and releases the port.
func (a *App) StopMonitor() {
	if a.stopMonitor != nil {
		select {
		case a.stopMonitor <- true:
		default:
		}
		close(a.stopMonitor)
		a.stopMonitor = nil
	}

	time.Sleep(200 * time.Millisecond)

	if a.monitorPort != nil {
		a.monitorPort.Close()
		a.monitorPort = nil
	}

	a.lineBuffer = ""
	runtime.EventsEmit(a.ctx, "monitor-stop", "")
	a.emitLog("⏹️ Serial monitoring stopped")
}

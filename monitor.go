package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	serialport "go.bug.st/serial"
)

const (
	monitorReadTimeout      = 50 * time.Millisecond
	monitorBatchInterval    = 75 * time.Millisecond
	monitorBinaryInterval   = time.Second
	monitorRepeatInterval   = time.Second
	monitorStopTimeout      = time.Second
	maxSerialLineBytes      = 4096
	maxMonitorBatchLines    = 64
	serialBinarySampleBytes = 12
)

type monitorEventSink func(name string, data any)

type monitorSession struct {
	port      serialport.Port
	stop      chan struct{}
	done      chan struct{}
	stopOnce  sync.Once
	closeOnce sync.Once
}

func newMonitorSession(port serialport.Port) *monitorSession {
	return &monitorSession{
		port: port,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
}

func (s *monitorSession) stopNow() {
	s.stopOnce.Do(func() {
		close(s.stop)
		s.closePort()
	})
}

func (s *monitorSession) closePort() {
	s.closeOnce.Do(func() {
		_ = s.port.Close()
	})
}

// MonitorPort opens a serial connection and streams bounded batches to the frontend.
func (a *App) MonitorPort(portName string, baudRate int) error {
	a.monitorControlMu.Lock()
	defer a.monitorControlMu.Unlock()

	if err := a.stopMonitor(false); err != nil {
		return err
	}

	mode := monitorPortMode(baudRate)

	port, err := serialport.Open(portName, mode)
	if err != nil {
		return fmt.Errorf("failed to open port for monitoring: %w", err)
	}
	if err := releaseMonitorControlLines(port); err != nil {
		_ = port.Close()
		return fmt.Errorf("release ESP32 boot/reset lines for monitoring: %w", err)
	}

	session := newMonitorSession(port)
	a.monitorMu.Lock()
	a.monitor = session
	a.monitorMu.Unlock()

	a.emitLog(fmt.Sprintf("🔍 Monitoring port %s at %d baud", portName, baudRate))
	a.emitLog("💡 Press Stop to end serial monitoring")

	go func() {
		session.run(func(name string, data any) {
			runtime.EventsEmit(a.ctx, name, data)
		})

		a.monitorMu.Lock()
		if a.monitor == session {
			a.monitor = nil
		}
		a.monitorMu.Unlock()
	}()

	return nil
}

func monitorPortMode(baudRate int) *serialport.Mode {
	return &serialport.Mode{
		BaudRate: baudRate,
		Parity:   serialport.NoParity,
		DataBits: 8,
		StopBits: serialport.OneStopBit,
		InitialStatusBits: &serialport.ModemOutputBits{
			DTR: false,
			RTS: false,
		},
	}
}

func releaseMonitorControlLines(port serialport.Port) error {
	// A nil InitialStatusBits value defaults both lines to asserted. On boards
	// with direct DTR→GPIO0 and RTS→EN wiring, that holds the ESP32 in reset.
	if err := port.SetDTR(false); err != nil {
		return fmt.Errorf("release DTR/GPIO0: %w", err)
	}
	if err := port.SetRTS(false); err != nil {
		return fmt.Errorf("release RTS/EN: %w", err)
	}
	return nil
}

// StopMonitor stops serial monitoring and releases the port.
func (a *App) StopMonitor() error {
	a.monitorControlMu.Lock()
	defer a.monitorControlMu.Unlock()

	return a.stopMonitor(true)
}

func (a *App) stopMonitor(notify bool) error {
	a.monitorMu.Lock()
	session := a.monitor
	if session != nil {
		a.monitor = nil
	}
	a.monitorMu.Unlock()

	if session != nil {
		session.stopNow()
		timer := time.NewTimer(monitorStopTimeout)
		defer timer.Stop()
		select {
		case <-session.done:
		case <-timer.C:
			return fmt.Errorf("serial monitor did not stop within %s", monitorStopTimeout)
		}
	}

	if notify {
		runtime.EventsEmit(a.ctx, "monitor-stop", "")
		a.emitLog("⏹️ Serial monitoring stopped")
	}
	return nil
}

func (s *monitorSession) run(emit monitorEventSink) {
	defer close(s.done)
	defer s.closePort()

	if err := s.port.SetReadTimeout(monitorReadTimeout); err != nil {
		emit("monitor-error", fmt.Sprintf("set serial read timeout: %v", err))
		return
	}

	decoder := serialLineDecoder{}
	readBuffer := make([]byte, 4096)
	pending := make([]string, 0, maxMonitorBatchLines)
	var binaryBytes int
	var binarySample []byte
	var lastText string
	var repeatedText int
	lastBatch := time.Now()
	lastBinary := time.Now()
	lastRepeatNotice := time.Now()

	appendRepeatNotice := func(now time.Time) {
		if repeatedText == 0 {
			return
		}
		pending = append(pending, formatRepeatedSerialNotice(repeatedText))
		repeatedText = 0
		lastRepeatNotice = now
	}

	flush := func(force bool) {
		now := time.Now()
		if repeatedText > 0 && (force || now.Sub(lastRepeatNotice) >= monitorRepeatInterval) {
			appendRepeatNotice(now)
		}
		if binaryBytes > 0 && (force || now.Sub(lastBinary) >= monitorBinaryInterval) {
			pending = append(pending, formatBinarySerialNotice(binaryBytes, binarySample))
			binaryBytes = 0
			binarySample = nil
			lastBinary = now
		}
		if len(pending) == 0 {
			return
		}
		if !force && len(pending) < maxMonitorBatchLines && now.Sub(lastBatch) < monitorBatchInterval {
			return
		}
		emit("monitor-data", strings.Join(pending, "\n"))
		pending = pending[:0]
		lastBatch = now
	}

	consume := func(records []serialRecord) {
		for _, record := range records {
			if record.text != "" {
				if record.text == lastText {
					repeatedText++
					continue
				}
				appendRepeatNotice(time.Now())
				pending = append(pending, record.text)
				lastText = record.text
				continue
			}
			appendRepeatNotice(time.Now())
			lastText = ""
			binaryBytes += record.binaryBytes
			if len(binarySample) == 0 && len(record.binarySample) > 0 {
				binarySample = append(binarySample, record.binarySample...)
			}
		}
	}

	for {
		select {
		case <-s.stop:
			consume(decoder.Flush())
			flush(true)
			return
		default:
		}

		n, err := s.port.Read(readBuffer)
		if n > 0 {
			consume(decoder.Write(readBuffer[:n]))
		}

		select {
		case <-s.stop:
			consume(decoder.Flush())
			flush(true)
			return
		default:
		}

		if err != nil && n == 0 {
			if isClosedSerialError(err) {
				return
			}
			if isSerialTimeoutError(err) {
				flush(false)
				continue
			}
			emit("monitor-error", err.Error())
			return
		}
		flush(false)
	}
}

type serialRecord struct {
	text         string
	binaryBytes  int
	binarySample []byte
}

type serialLineDecoder struct {
	buffer []byte
}

func (d *serialLineDecoder) Write(data []byte) []serialRecord {
	d.buffer = append(d.buffer, data...)
	records := make([]serialRecord, 0)

	for {
		newline := bytes.IndexByte(d.buffer, '\n')
		if newline < 0 {
			break
		}
		records = appendSerialRecord(records, d.buffer[:newline], false)
		d.buffer = d.buffer[newline+1:]
	}

	for len(d.buffer) > maxSerialLineBytes {
		records = appendSerialRecord(records, d.buffer[:maxSerialLineBytes], true)
		d.buffer = d.buffer[maxSerialLineBytes:]
	}

	return records
}

func (d *serialLineDecoder) Flush() []serialRecord {
	records := appendSerialRecord(nil, d.buffer, false)
	d.buffer = nil
	return records
}

func appendSerialRecord(records []serialRecord, raw []byte, continued bool) []serialRecord {
	raw = bytes.Trim(raw, "\r")
	if len(bytes.TrimSpace(raw)) == 0 {
		return records
	}
	if isSerialText(raw) {
		text := strings.TrimSpace(string(raw))
		if continued {
			text += " … [continued]"
		}
		return append(records, serialRecord{text: text})
	}

	sampleLength := min(len(raw), serialBinarySampleBytes)
	sample := append([]byte(nil), raw[:sampleLength]...)
	return append(records, serialRecord{binaryBytes: len(raw), binarySample: sample})
}

func isSerialText(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}

	var printable, total int
	for _, r := range string(data) {
		total++
		if unicode.IsPrint(r) || r == '\t' || r == '\r' || r == '\x1b' {
			printable++
		}
	}
	return total > 0 && printable*100/total >= 85
}

func formatBinarySerialNotice(size int, sample []byte) string {
	return fmt.Sprintf("⚠️ Received %d bytes of non-text serial data (sample: %s). Check the selected baud rate.",
		size, strings.ToUpper(hex.EncodeToString(sample)))
}

func formatRepeatedSerialNotice(count int) string {
	unit := "times"
	if count == 1 {
		unit = "time"
	}
	return fmt.Sprintf("↳ Previous line repeated %d %s", count, unit)
}

func isClosedSerialError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "bad file descriptor") ||
		strings.Contains(message, "file already closed") ||
		strings.Contains(message, "port is closed")
}

func isSerialTimeoutError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "timeout")
}

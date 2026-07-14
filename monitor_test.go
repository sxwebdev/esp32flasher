package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"espflasher/internal/esp32"
	"espflasher/internal/firmware"

	serialport "go.bug.st/serial"
)

func TestMonitorModeReleasesBootAndResetLines(t *testing.T) {
	t.Context()
	mode := monitorPortMode(115200)

	if mode.InitialStatusBits == nil {
		t.Fatal("monitor mode leaves DTR/RTS at driver defaults, want both explicitly released")
	}
	if mode.InitialStatusBits.DTR || mode.InitialStatusBits.RTS {
		t.Fatalf("initial modem bits = %+v, want DTR=false and RTS=false", *mode.InitialStatusBits)
	}
}

// TestMonitorESP32Hardware runs only when ESP32_MONITOR_PORT is explicitly set.
// It performs one controlled reset and verifies that monitoring does not create
// a reset loop on direct DTR/RTS wiring.
func TestMonitorESP32Hardware(t *testing.T) {
	t.Context()
	portName := os.Getenv("ESP32_MONITOR_PORT")
	if portName == "" {
		t.Skip("set ESP32_MONITOR_PORT to enable the monitor hardware test")
	}

	port, err := serialport.Open(portName, monitorPortMode(115200))
	if err != nil {
		t.Fatalf("open monitor port: %v", err)
	}
	t.Cleanup(func() {
		if err := port.Close(); err != nil {
			t.Errorf("close monitor port: %v", err)
		}
	})
	if err := releaseMonitorControlLines(port); err != nil {
		t.Fatalf("release monitor control lines: %v", err)
	}
	if err := port.SetReadTimeout(200 * time.Millisecond); err != nil {
		t.Fatalf("set read timeout: %v", err)
	}
	if err := port.ResetInputBuffer(); err != nil {
		t.Fatalf("reset input buffer: %v", err)
	}

	if err := port.SetDTR(false); err != nil {
		t.Fatalf("release boot pin: %v", err)
	}
	if err := port.SetRTS(true); err != nil {
		t.Fatalf("assert reset: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := port.SetRTS(false); err != nil {
		t.Fatalf("release reset: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	readBuffer := make([]byte, 1024)
	var output []byte
	for time.Now().Before(deadline) {
		n, err := port.Read(readBuffer)
		if n > 0 {
			output = append(output, readBuffer[:n]...)
		}
		if err != nil {
			t.Fatalf("read monitor output: %v", err)
		}
	}

	if resets := bytes.Count(output, []byte("rst:")); resets != 1 {
		t.Fatalf("observed %d reset banners, want exactly 1; output=%q", resets, output)
	}
	if !bytes.Contains(output, []byte("ESP32 BLDC Rover")) {
		t.Fatalf("application banner not received; output=%q", output)
	}
}

// TestMonitorESP32RawCapture is a diagnostic hardware test that records a
// short unprocessed UART sample without resetting or writing to the device.
func TestMonitorESP32RawCapture(t *testing.T) {
	t.Context()
	portName := os.Getenv("ESP32_MONITOR_CAPTURE_PORT")
	if portName == "" {
		t.Skip("set ESP32_MONITOR_CAPTURE_PORT to capture raw monitor bytes")
	}

	port, err := serialport.Open(portName, monitorPortMode(115200))
	if err != nil {
		t.Fatalf("open monitor port: %v", err)
	}
	t.Cleanup(func() {
		if err := port.Close(); err != nil {
			t.Errorf("close monitor port: %v", err)
		}
	})
	if err := releaseMonitorControlLines(port); err != nil {
		t.Fatalf("release monitor control lines: %v", err)
	}
	if err := port.SetReadTimeout(50 * time.Millisecond); err != nil {
		t.Fatalf("set read timeout: %v", err)
	}

	deadline := time.Now().Add(250 * time.Millisecond)
	readBuffer := make([]byte, 4096)
	var output []byte
	for time.Now().Before(deadline) {
		n, err := port.Read(readBuffer)
		if n > 0 {
			output = append(output, readBuffer[:n]...)
		}
		if err != nil {
			t.Fatalf("read raw monitor output: %v", err)
		}
	}
	if len(output) == 0 {
		t.Log("raw monitor capture was empty")
		return
	}
	sample := output[:min(len(output), 512)]
	t.Logf("captured %d bytes; first sample=%q", len(output), sample)
}

// TestProductionFlashMonitorLifecycleHardware reproduces the desktop lifecycle:
// flash, reset, close the flasher port, wait for startup, then reopen monitoring.
func TestProductionFlashMonitorLifecycleHardware(t *testing.T) {
	t.Context()
	portName := os.Getenv("ESP32_LIFECYCLE_PORT")
	if portName == "" {
		t.Skip("set ESP32_LIFECYCLE_PORT to run the destructive lifecycle test")
	}

	imagePath := "testdata/esp32_rx_hardworker_latest.bin"
	data, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatalf("read firmware: %v", err)
	}
	image := firmware.Detect(imagePath, data)
	flasher, err := esp32.New(portName, nil)
	if err != nil {
		t.Fatalf("connect to ROM bootloader: %v", err)
	}
	t.Cleanup(func() { _ = flasher.Close() })
	if err := flasher.Flash(data, image.Offset); err != nil {
		t.Fatalf("flash firmware: %v", err)
	}
	if err := flasher.RebootTarget(); err != nil {
		t.Fatalf("reboot target: %v", err)
	}
	if err := flasher.Close(); err != nil {
		t.Fatalf("close flasher immediately after reboot: %v", err)
	}

	time.Sleep(8 * time.Second)
	port, err := serialport.Open(portName, monitorPortMode(115200))
	if err != nil {
		t.Fatalf("reopen monitor port: %v", err)
	}
	t.Cleanup(func() { _ = port.Close() })
	if err := releaseMonitorControlLines(port); err != nil {
		t.Fatalf("release monitor control lines: %v", err)
	}
	if err := port.SetReadTimeout(50 * time.Millisecond); err != nil {
		t.Fatalf("set monitor timeout: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	readBuffer := make([]byte, 4096)
	var output []byte
	for time.Now().Before(deadline) {
		n, err := port.Read(readBuffer)
		if n > 0 {
			output = append(output, readBuffer[:n]...)
		}
		if err != nil {
			t.Fatalf("read reopened monitor: %v", err)
		}
	}

	lineCounts := make(map[string]int)
	mostRepeatedLine := ""
	mostRepeatedCount := 0
	for _, rawLine := range bytes.Split(output, []byte("\n")) {
		line := string(bytes.TrimSpace(rawLine))
		if line == "" {
			continue
		}
		lineCounts[line]++
		if lineCounts[line] > mostRepeatedCount {
			mostRepeatedLine = line
			mostRepeatedCount = lineCounts[line]
		}
	}
	t.Logf("reopened monitor captured %d bytes; most repeated line=%q count=%d", len(output), mostRepeatedLine, mostRepeatedCount)
	if mostRepeatedCount > 10 {
		t.Fatalf("serial output loop after production port reopen: line %q repeated %d times", mostRepeatedLine, mostRepeatedCount)
	}
}

func TestReleaseMonitorControlLinesLeavesESP32Running(t *testing.T) {
	t.Context()
	port := &monitorControlLinePort{}

	if err := releaseMonitorControlLines(port); err != nil {
		t.Fatalf("releaseMonitorControlLines() error = %v", err)
	}
	want := []string{"DTR=false", "RTS=false"}
	if got := strings.Join(port.actions, ","); got != strings.Join(want, ",") {
		t.Fatalf("control line actions = %v, want %v", port.actions, want)
	}
}

func TestReleaseMonitorControlLinesStopsOnResetError(t *testing.T) {
	tests := []struct {
		name string
		port *monitorControlLinePort
		want []string
	}{
		{name: "DTR", port: &monitorControlLinePort{failDTR: errMonitorControlLine}, want: []string{"DTR=false"}},
		{name: "RTS", port: &monitorControlLinePort{failRTS: errMonitorControlLine}, want: []string{"DTR=false", "RTS=false"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := releaseMonitorControlLines(tt.port)
			if !errors.Is(err, errMonitorControlLine) {
				t.Fatalf("releaseMonitorControlLines() error = %v, want %v", err, errMonitorControlLine)
			}
			if got := strings.Join(tt.port.actions, ","); got != strings.Join(tt.want, ",") {
				t.Fatalf("control line actions = %v, want %v", tt.port.actions, tt.want)
			}
		})
	}
}

func TestSerialLineDecoderPreservesTextAcrossReads(t *testing.T) {
	t.Context()
	decoder := serialLineDecoder{}

	if records := decoder.Write([]byte("hel")); len(records) != 0 {
		t.Fatalf("first partial write produced %d records, want 0", len(records))
	}
	records := decoder.Write([]byte("lo\r\nworld\n"))
	want := []string{"hello", "world"}
	if len(records) != len(want) {
		t.Fatalf("decoded %d records, want %d", len(records), len(want))
	}
	for i, record := range records {
		if record.text != want[i] || record.binaryBytes != 0 {
			t.Errorf("record %d = %+v, want text %q", i, record, want[i])
		}
	}
}

func TestSerialLineDecoderClassifiesNonTextData(t *testing.T) {
	t.Context()
	tests := []struct {
		name string
		data []byte
	}{
		{name: "invalid UTF-8", data: []byte{0xff, 0xfe, '\n'}},
		{name: "control bytes", data: []byte{0x00, 0x01, 0x02, '\n'}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			decoder := serialLineDecoder{}
			records := decoder.Write(tt.data)
			if len(records) != 1 {
				t.Fatalf("decoded %d records, want 1", len(records))
			}
			if records[0].text != "" || records[0].binaryBytes != len(tt.data)-1 {
				t.Fatalf("record = %+v, want %d binary bytes", records[0], len(tt.data)-1)
			}
		})
	}
}

func TestSerialLineDecoderBoundsLongLines(t *testing.T) {
	t.Context()
	decoder := serialLineDecoder{}
	records := decoder.Write([]byte(strings.Repeat("A", maxSerialLineBytes+1)))
	if len(records) != 1 {
		t.Fatalf("decoded %d records, want 1 bounded record", len(records))
	}
	if !strings.HasSuffix(records[0].text, " … [continued]") {
		t.Fatalf("long line = %q, want continuation marker", records[0].text)
	}
	if tail := decoder.Flush(); len(tail) != 1 || tail[0].text != "A" {
		t.Fatalf("remaining records = %+v, want one-byte text tail", tail)
	}
}

func TestSerialErrorClassification(t *testing.T) {
	t.Context()
	if !isSerialTimeoutError(errors.New("read timeout")) {
		t.Fatal("timeout error was not classified as a timeout")
	}
	if isSerialTimeoutError(errors.New("device disconnected")) {
		t.Fatal("disconnect error was classified as a timeout")
	}
	if !isClosedSerialError(errors.New("file already closed")) {
		t.Fatal("closed-port error was not classified as closed")
	}
}

func TestMonitorSessionStopIsConcurrentAndIdempotent(t *testing.T) {
	t.Context()
	port := newBlockingMonitorPort()
	session := newMonitorSession(port)
	go session.run(func(string, any) {})
	waitForTestSignal(t, port.readStarted)

	var callers sync.WaitGroup
	for range 16 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			session.stopNow()
		}()
	}
	callers.Wait()
	waitForTestSignal(t, session.done)

	if got := port.closeCalls.Load(); got != 1 {
		t.Fatalf("Close called %d times, want exactly once", got)
	}
}

func TestMonitorSessionCollapsesBinaryFlood(t *testing.T) {
	t.Context()
	const chunks = 100
	const chunkSize = 1024
	port := newBinaryFloodMonitorPort(chunks, chunkSize)
	session := newMonitorSession(port)

	var eventMu sync.Mutex
	var events []string
	go session.run(func(name string, data any) {
		if name != "monitor-data" {
			return
		}
		eventMu.Lock()
		events = append(events, data.(string))
		eventMu.Unlock()
	})

	waitForTestSignal(t, port.floodWritten)
	session.stopNow()
	waitForTestSignal(t, session.done)

	eventMu.Lock()
	defer eventMu.Unlock()
	if len(events) != 1 {
		t.Fatalf("binary flood emitted %d UI events, want 1", len(events))
	}
	if !strings.Contains(events[0], "102400 bytes of non-text serial data") {
		t.Fatalf("binary warning = %q, want aggregated byte count", events[0])
	}
}

func TestMonitorSessionCollapsesRepeatedTextFlood(t *testing.T) {
	t.Context()
	const repeats = 1000
	port := newTextFloodMonitorPort("g mode\r\n", repeats)
	session := newMonitorSession(port)

	var eventMu sync.Mutex
	var events []string
	go session.run(func(name string, data any) {
		if name != "monitor-data" {
			return
		}
		eventMu.Lock()
		events = append(events, data.(string))
		eventMu.Unlock()
	})

	waitForTestSignal(t, port.floodWritten)
	session.stopNow()
	waitForTestSignal(t, session.done)

	eventMu.Lock()
	defer eventMu.Unlock()
	output := strings.Join(events, "\n")
	if occurrences := strings.Count(output, "g mode"); occurrences != 1 {
		t.Fatalf("repeated line emitted %d times, want exactly 1; events=%d", occurrences, len(events))
	}
	wantSummary := "Previous line repeated 999 times"
	if !strings.Contains(output, wantSummary) {
		t.Fatalf("monitor output has no %q summary: %q", wantSummary, output)
	}
}

func waitForTestSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	t.Cleanup(cancel)
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for test signal: %v", context.Cause(ctx))
	}
}

type monitorPortStub struct{}

func (*monitorPortStub) SetMode(*serialport.Mode) error { return nil }
func (*monitorPortStub) Read([]byte) (int, error)       { return 0, nil }
func (*monitorPortStub) Write(p []byte) (int, error)    { return len(p), nil }
func (*monitorPortStub) Drain() error                   { return nil }
func (*monitorPortStub) ResetInputBuffer() error        { return nil }
func (*monitorPortStub) ResetOutputBuffer() error       { return nil }
func (*monitorPortStub) SetDTR(bool) error              { return nil }
func (*monitorPortStub) SetRTS(bool) error              { return nil }
func (*monitorPortStub) GetModemStatusBits() (*serialport.ModemStatusBits, error) {
	return &serialport.ModemStatusBits{}, nil
}
func (*monitorPortStub) SetReadTimeout(time.Duration) error { return nil }
func (*monitorPortStub) Close() error                       { return nil }
func (*monitorPortStub) Break(time.Duration) error          { return nil }

var errMonitorControlLine = errors.New("monitor control line failure")

type monitorControlLinePort struct {
	monitorPortStub
	actions []string
	failDTR error
	failRTS error
}

func (p *monitorControlLinePort) SetDTR(value bool) error {
	p.actions = append(p.actions, fmt.Sprintf("DTR=%t", value))
	return p.failDTR
}

func (p *monitorControlLinePort) SetRTS(value bool) error {
	p.actions = append(p.actions, fmt.Sprintf("RTS=%t", value))
	return p.failRTS
}

type blockingMonitorPort struct {
	monitorPortStub
	readStarted chan struct{}
	closed      chan struct{}
	readOnce    sync.Once
	closeOnce   sync.Once
	closeCalls  atomic.Int32
}

func newBlockingMonitorPort() *blockingMonitorPort {
	return &blockingMonitorPort{
		readStarted: make(chan struct{}),
		closed:      make(chan struct{}),
	}
}

func (p *blockingMonitorPort) Read([]byte) (int, error) {
	p.readOnce.Do(func() { close(p.readStarted) })
	<-p.closed
	return 0, errors.New("port is closed")
}

func (p *blockingMonitorPort) Close() error {
	p.closeCalls.Add(1)
	p.closeOnce.Do(func() { close(p.closed) })
	return nil
}

type binaryFloodMonitorPort struct {
	monitorPortStub
	remaining    int
	chunkSize    int
	floodWritten chan struct{}
	closed       chan struct{}
	floodOnce    sync.Once
	closeOnce    sync.Once
}

func newBinaryFloodMonitorPort(chunks, chunkSize int) *binaryFloodMonitorPort {
	return &binaryFloodMonitorPort{
		remaining:    chunks,
		chunkSize:    chunkSize,
		floodWritten: make(chan struct{}),
		closed:       make(chan struct{}),
	}
}

func (p *binaryFloodMonitorPort) Read(buffer []byte) (int, error) {
	if p.remaining > 0 {
		n := min(len(buffer), p.chunkSize)
		for i := range n {
			buffer[i] = 0xff
		}
		p.remaining--
		if p.remaining == 0 {
			p.floodOnce.Do(func() { close(p.floodWritten) })
		}
		return n, nil
	}
	<-p.closed
	return 0, errors.New("port is closed")
}

func (p *binaryFloodMonitorPort) Close() error {
	p.closeOnce.Do(func() { close(p.closed) })
	return nil
}

type textFloodMonitorPort struct {
	monitorPortStub
	line         []byte
	remaining    int
	floodWritten chan struct{}
	closed       chan struct{}
	floodOnce    sync.Once
	closeOnce    sync.Once
}

func newTextFloodMonitorPort(line string, repeats int) *textFloodMonitorPort {
	return &textFloodMonitorPort{
		line:         []byte(line),
		remaining:    repeats,
		floodWritten: make(chan struct{}),
		closed:       make(chan struct{}),
	}
}

func (p *textFloodMonitorPort) Read(buffer []byte) (int, error) {
	if p.remaining > 0 {
		lines := min(p.remaining, len(buffer)/len(p.line))
		offset := 0
		for range lines {
			offset += copy(buffer[offset:], p.line)
		}
		p.remaining -= lines
		if p.remaining == 0 {
			p.floodOnce.Do(func() { close(p.floodWritten) })
		}
		return offset, nil
	}
	<-p.closed
	return 0, errors.New("port is closed")
}

func (p *textFloodMonitorPort) Close() error {
	p.closeOnce.Do(func() { close(p.closed) })
	return nil
}

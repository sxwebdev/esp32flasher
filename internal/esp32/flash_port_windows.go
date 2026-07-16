//go:build windows

package esp32

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
	"golang.org/x/sys/windows"
)

const maxReadTimeoutMilliseconds = 0x7FFFFFFE

const platformUsesEsptoolWindowsControl = true

type windowsFlashPort struct {
	mu         sync.Mutex
	handle     windows.Handle
	hasTimeout bool
	dtr        bool
	rts        bool
}

func openFlashPort(portName string, mode *serial.Mode) (serial.Port, modemControl, error, error) {
	if !strings.HasPrefix(portName, `\\.\`) {
		portName = `\\.\` + portName
	}
	path, err := windows.UTF16PtrFromString(portName)
	if err != nil {
		return nil, nil, nil, err
	}

	handle, err := windows.CreateFile(
		path,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OVERLAPPED,
		0,
	)
	if err != nil {
		return nil, nil, nil, err
	}

	dtr, rts := true, true
	if mode.InitialStatusBits != nil {
		dtr = mode.InitialStatusBits.DTR
		rts = mode.InitialStatusBits.RTS
	}
	port := &windowsFlashPort{handle: handle, dtr: dtr, rts: rts}
	if err := windows.SetupComm(handle, 4096, 4096); err != nil {
		_ = port.Close()
		return nil, nil, nil, err
	}
	if err := port.SetMode(mode); err != nil {
		_ = port.Close()
		return nil, nil, nil, err
	}
	if err := port.SetReadTimeout(serial.NoTimeout); err != nil {
		_ = port.Close()
		return nil, nil, nil, err
	}

	if err := port.SetDTR(dtr); err != nil {
		_ = port.Close()
		return nil, nil, nil, err
	}
	if err := port.SetRTS(rts); err != nil {
		_ = port.Close()
		return nil, nil, nil, err
	}
	if err := windows.PurgeComm(handle, windows.PURGE_TXCLEAR|windows.PURGE_TXABORT|windows.PURGE_RXCLEAR|windows.PURGE_RXABORT); err != nil {
		_ = port.Close()
		return nil, nil, nil, err
	}

	return port, nil, nil, nil
}

func (p *windowsFlashPort) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.handle == 0 {
		return nil
	}
	err := windows.CloseHandle(p.handle)
	p.handle = 0
	return err
}

func (p *windowsFlashPort) SetMode(mode *serial.Mode) error {
	state := &windows.DCB{}
	if err := windows.GetCommState(p.handle, state); err != nil {
		return fmt.Errorf("get serial mode: %w", err)
	}

	state.BaudRate = uint32(mode.BaudRate)
	if state.BaudRate == 0 {
		state.BaudRate = windows.CBR_9600
	}
	state.ByteSize = byte(mode.DataBits)
	if state.ByteSize == 0 {
		state.ByteSize = 8
	}
	state.Parity = windows.NOPARITY
	switch mode.Parity {
	case serial.OddParity:
		state.Parity = windows.ODDPARITY
	case serial.EvenParity:
		state.Parity = windows.EVENPARITY
	case serial.MarkParity:
		state.Parity = windows.MARKPARITY
	case serial.SpaceParity:
		state.Parity = windows.SPACEPARITY
	}
	state.StopBits = windows.ONESTOPBIT
	switch mode.StopBits {
	case serial.OnePointFiveStopBits:
		state.StopBits = windows.ONE5STOPBITS
	case serial.TwoStopBits:
		state.StopBits = windows.TWOSTOPBITS
	}

	// Configure DTR/RTS and disable hardware/software flow control, matching
	// pySerial's serialwin32 transport used by esptool.
	state.Flags |= 0x00000001
	state.Flags &^= 0x00000002
	if mode.Parity != serial.NoParity {
		state.Flags |= 0x00000002
	}
	state.Flags &^= 0x00000030 | 0x00003000
	if p.dtr {
		state.Flags |= 0x00000010
	}
	if p.rts {
		state.Flags |= 0x00001000
	}
	// Disable hardware and software flow control, matching pySerial's
	// default configuration used by esptool.
	state.Flags &^= 0x00000004 | 0x00000008 | 0x00000040
	state.Flags &^= 0x00000100 | 0x00000200 | 0x00000400 | 0x00000800 | 0x00004000
	state.Flags |= 0x00000080
	state.XonLim = 2048
	state.XoffLim = 512
	state.XonChar = 17
	state.XoffChar = 19

	if err := windows.SetCommState(p.handle, state); err != nil {
		return fmt.Errorf("set serial mode: %w", err)
	}
	return nil
}

func (p *windowsFlashPort) SetDTR(asserted bool) error {
	function := uint32(windows.CLRDTR)
	if asserted {
		function = windows.SETDTR
	}
	if err := windows.EscapeCommFunction(p.handle, function); err != nil {
		return fmt.Errorf("set DTR to %t: %w", asserted, err)
	}
	p.dtr = asserted
	return nil
}

func (p *windowsFlashPort) SetRTS(asserted bool) error {
	function := uint32(windows.CLRRTS)
	if asserted {
		function = windows.SETRTS
	}
	if err := windows.EscapeCommFunction(p.handle, function); err != nil {
		return fmt.Errorf("set RTS to %t: %w", asserted, err)
	}
	p.rts = asserted
	return nil
}

func (p *windowsFlashPort) Read(buffer []byte) (int, error) {
	for {
		event, err := newWindowsOverlappedEvent()
		if err != nil {
			return 0, err
		}

		var count uint32
		err = windows.ReadFile(p.handle, buffer, &count, event)
		if err == windows.ERROR_IO_PENDING {
			err = windows.GetOverlappedResult(p.handle, event, &count, true)
		}
		_ = windows.CloseHandle(event.HEvent)
		if err != nil {
			return int(count), err
		}
		if count > 0 {
			return int(count), nil
		}

		p.mu.Lock()
		hasTimeout := p.hasTimeout
		p.mu.Unlock()
		if hasTimeout {
			return 0, nil
		}
	}
}

func (p *windowsFlashPort) Write(buffer []byte) (int, error) {
	event, err := newWindowsOverlappedEvent()
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(event.HEvent)

	var count uint32
	err = windows.WriteFile(p.handle, buffer, &count, event)
	if err == windows.ERROR_IO_PENDING {
		err = windows.GetOverlappedResult(p.handle, event, &count, true)
	}
	return int(count), err
}

func newWindowsOverlappedEvent() (*windows.Overlapped, error) {
	handle, err := windows.CreateEvent(nil, 1, 0, nil)
	return &windows.Overlapped{HEvent: handle}, err
}

func (p *windowsFlashPort) Drain() error {
	return windows.FlushFileBuffers(p.handle)
}

func (p *windowsFlashPort) ResetInputBuffer() error {
	return windows.PurgeComm(p.handle, windows.PURGE_RXCLEAR|windows.PURGE_RXABORT)
}

func (p *windowsFlashPort) ResetOutputBuffer() error {
	return windows.PurgeComm(p.handle, windows.PURGE_TXCLEAR|windows.PURGE_TXABORT)
}

func (p *windowsFlashPort) SetReadTimeout(timeout time.Duration) error {
	timeouts := &windows.CommTimeouts{
		ReadIntervalTimeout:        0xFFFFFFFF,
		ReadTotalTimeoutMultiplier: 0xFFFFFFFF,
		ReadTotalTimeoutConstant:   maxReadTimeoutMilliseconds,
	}
	if timeout != serial.NoTimeout {
		milliseconds := timeout.Milliseconds()
		if milliseconds < 0 || milliseconds > 0xFFFFFFFE {
			return fmt.Errorf("invalid read timeout: %s", timeout)
		}
		if milliseconds > maxReadTimeoutMilliseconds {
			milliseconds = maxReadTimeoutMilliseconds
		}
		timeouts.ReadTotalTimeoutConstant = uint32(milliseconds)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if err := windows.SetCommTimeouts(p.handle, timeouts); err != nil {
		return err
	}
	p.hasTimeout = timeout != serial.NoTimeout
	return nil
}

func (p *windowsFlashPort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	var status uint32
	if err := windows.GetCommModemStatus(p.handle, &status); err != nil {
		return nil, err
	}
	return &serial.ModemStatusBits{
		CTS: status&0x0010 != 0,
		DSR: status&0x0020 != 0,
		RI:  status&0x0040 != 0,
		DCD: status&0x0080 != 0,
	}, nil
}

func (p *windowsFlashPort) Break(duration time.Duration) error {
	if err := windows.SetCommBreak(p.handle); err != nil {
		return err
	}
	time.Sleep(duration)
	return windows.ClearCommBreak(p.handle)
}

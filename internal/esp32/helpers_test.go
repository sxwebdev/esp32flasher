package esp32

import (
	"errors"
	"fmt"
	"time"

	"go.bug.st/serial"
)

var errControlLine = errors.New("control line failure")

type fakeSerialPort struct {
	actions    []string
	failAction string
	baudRates  []int
}

func (p *fakeSerialPort) record(action string) error {
	p.actions = append(p.actions, action)
	if action == p.failAction {
		return errControlLine
	}
	return nil
}

func (p *fakeSerialPort) SetMode(mode *serial.Mode) error {
	p.baudRates = append(p.baudRates, mode.BaudRate)
	return nil
}
func (p *fakeSerialPort) Read([]byte) (int, error)           { return 0, nil }
func (p *fakeSerialPort) Write(b []byte) (int, error)        { return len(b), nil }
func (p *fakeSerialPort) Drain() error                       { return nil }
func (p *fakeSerialPort) ResetInputBuffer() error            { return nil }
func (p *fakeSerialPort) ResetOutputBuffer() error           { return nil }
func (p *fakeSerialPort) SetDTR(v bool) error                { return p.record(fmt.Sprintf("DTR=%t", v)) }
func (p *fakeSerialPort) SetRTS(v bool) error                { return p.record(fmt.Sprintf("RTS=%t", v)) }
func (p *fakeSerialPort) SetReadTimeout(time.Duration) error { return nil }
func (p *fakeSerialPort) Close() error                       { return nil }
func (p *fakeSerialPort) Break(time.Duration) error          { return nil }
func (p *fakeSerialPort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	return &serial.ModemStatusBits{}, nil
}

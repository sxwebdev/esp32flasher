//go:build !windows

package esp32

import "go.bug.st/serial"

const platformUsesEsptoolWindowsControl = false

func openFlashPort(portName string, mode *serial.Mode) (serial.Port, modemControl, error, error) {
	control, controlErr := openModemControl(portName)
	port, err := serial.Open(portName, mode)
	if err != nil && control != nil {
		_ = control.Close()
	}
	return port, control, controlErr, err
}

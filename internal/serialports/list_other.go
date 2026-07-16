//go:build !windows

package serialports

import serialport "go.bug.st/serial"

// List returns the serial ports reported by the current operating system.
func List() ([]string, error) {
	return serialport.GetPortsList()
}

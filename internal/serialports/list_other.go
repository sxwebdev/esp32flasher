//go:build !windows

package serialports

import serialport "go.bug.st/serial"

// List returns the serial ports reported by the current operating system.
func List() ([]Port, error) {
	names, err := serialport.GetPortsList()
	return fromNames(names, err)
}

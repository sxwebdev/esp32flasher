//go:build windows

package serialports

import (
	serialport "go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

// List combines registry and SetupAPI discovery on Windows.
func List() ([]string, error) {
	return discover(serialport.GetPortsList, setupAPIPorts)
}

// setupAPIPorts finds present devices from the Windows Ports device class.
// This covers USB CDC and composite devices that do not appear in SERIALCOMM.
func setupAPIPorts() ([]string, error) {
	details, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, err
	}

	ports := make([]string, 0, len(details))
	for _, port := range details {
		if port != nil {
			ports = append(ports, port.Name)
		}
	}
	return ports, nil
}

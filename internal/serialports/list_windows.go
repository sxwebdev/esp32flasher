//go:build windows

package serialports

import (
	"errors"

	serialport "go.bug.st/serial"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const maxDOSDeviceBuffer = 1 << 20

var (
	portsClassGUID = windows.GUID{
		Data1: 0x4D36E978,
		Data2: 0xE325,
		Data3: 0x11CE,
		Data4: [8]byte{0xBF, 0xC1, 0x08, 0x00, 0x2B, 0xE1, 0x03, 0x18},
	}
	comPortInterfaceGUID = windows.GUID{
		Data1: 0x86E0D1E0,
		Data2: 0x8089,
		Data3: 0x11D0,
		Data4: [8]byte{0x9C, 0xE4, 0x08, 0x00, 0x3E, 0x30, 0x1F, 0x73},
	}
)

// List combines three independent discovery paths on Windows. QueryDosDevice
// catches active COM devices whose drivers do not populate SERIALCOMM or the
// standard Ports device class correctly.
func List() ([]Port, error) {
	return discover(setupAPIPorts, registryPorts, dosDevicePorts)
}

func registryPorts() ([]Port, error) {
	names, err := serialport.GetPortsList()
	return fromNames(names, err)
}

// setupAPIPorts checks both the Ports device class and the COM-port device
// interface. Per-device errors are ignored so one broken driver cannot discard
// every other port from the result.
func setupAPIPorts() ([]Port, error) {
	queries := []struct {
		guid  *windows.GUID
		flags windows.DIGCF
	}{
		{guid: &portsClassGUID, flags: windows.DIGCF_PRESENT},
		{guid: &comPortInterfaceGUID, flags: windows.DIGCF_PRESENT | windows.DIGCF_DEVICEINTERFACE},
	}

	var (
		ports     []Port
		queryErrs []error
		succeeded bool
	)
	for _, query := range queries {
		deviceInfoSet, err := windows.SetupDiGetClassDevsEx(query.guid, "", 0, query.flags, 0, "")
		if err != nil {
			queryErrs = append(queryErrs, err)
			continue
		}

		succeeded = true
		ports = append(ports, portsFromDeviceInfoSet(deviceInfoSet)...)
		_ = deviceInfoSet.Close()
	}
	if succeeded {
		return ports, nil
	}
	return nil, errors.Join(queryErrs...)
}

func portsFromDeviceInfoSet(deviceInfoSet windows.DevInfo) []Port {
	var ports []Port
	for index := 0; ; index++ {
		device, err := deviceInfoSet.EnumDeviceInfo(index)
		if err != nil {
			break
		}

		port, ok := portFromDeviceInfo(deviceInfoSet, device)
		if ok {
			ports = append(ports, port)
		}
	}
	return ports
}

func portFromDeviceInfo(deviceInfoSet windows.DevInfo, device *windows.DevInfoData) (Port, bool) {
	handle, err := deviceInfoSet.OpenDevRegKey(
		device,
		windows.DICS_FLAG_GLOBAL,
		0,
		windows.DIREG_DEV,
		windows.KEY_READ,
	)
	if err != nil {
		return Port{}, false
	}

	key := registry.Key(handle)
	name, _, nameErr := key.GetStringValue("PortName")
	_ = key.Close()
	if nameErr != nil || name == "" {
		return Port{}, false
	}

	description := deviceStringProperty(deviceInfoSet, device, windows.SPDRP_FRIENDLYNAME)
	if description == "" {
		description = deviceStringProperty(deviceInfoSet, device, windows.SPDRP_DEVICEDESC)
	}
	return Port{Name: name, Description: description}, true
}

func deviceStringProperty(deviceInfoSet windows.DevInfo, device *windows.DevInfoData, property windows.SPDRP) string {
	value, err := deviceInfoSet.DeviceRegistryProperty(device, property)
	if err != nil {
		return ""
	}
	description, _ := value.(string)
	return description
}

func dosDevicePorts() ([]Port, error) {
	for bufferSize := 4096; bufferSize <= maxDOSDeviceBuffer; bufferSize *= 2 {
		buffer := make([]uint16, bufferSize)
		length, err := windows.QueryDosDevice(nil, &buffer[0], uint32(len(buffer)))
		if errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
			continue
		}
		if err != nil {
			return nil, err
		}

		names := splitMultiString(buffer[:length])
		ports := make([]Port, 0, len(names))
		for _, name := range names {
			if _, isCOM := comPortNumber(name); isCOM {
				ports = append(ports, Port{Name: name})
			}
		}
		return ports, nil
	}

	return nil, windows.ERROR_INSUFFICIENT_BUFFER
}

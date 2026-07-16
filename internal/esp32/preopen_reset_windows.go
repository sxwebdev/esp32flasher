//go:build windows

package esp32

import (
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

const (
	platformSupportsPreOpenReset = true

	dcbDTRControlMask   = uint32(0x00000030)
	dcbDTRControlEnable = uint32(0x00000010)
	dcbRTSControlMask   = uint32(0x00003000)
	dcbRTSControlEnable = uint32(0x00001000)
)

type windowsAtomicModemControl struct {
	handle windows.Handle
}

func preOpenBootloaderReset(portName string) error {
	if !strings.HasPrefix(portName, `\\.\`) {
		portName = `\\.\` + portName
	}
	name, err := windows.UTF16PtrFromString(portName)
	if err != nil {
		return err
	}

	handle, err := windows.CreateFile(
		name,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_OVERLAPPED,
		0,
	)
	if err != nil {
		return err
	}
	control := &windowsAtomicModemControl{handle: handle}
	defer control.Close()

	return atomicBootloaderReset(control, time.Sleep)
}

func (c *windowsAtomicModemControl) Set(dtr, rts bool) error {
	state := &windows.DCB{}
	if err := windows.GetCommState(c.handle, state); err != nil {
		return err
	}

	state.Flags &^= dcbDTRControlMask | dcbRTSControlMask
	if dtr {
		state.Flags |= dcbDTRControlEnable
	}
	if rts {
		state.Flags |= dcbRTSControlEnable
	}
	return windows.SetCommState(c.handle, state)
}

func (c *windowsAtomicModemControl) Close() error {
	if c.handle == 0 {
		return nil
	}
	err := windows.CloseHandle(c.handle)
	c.handle = 0
	return err
}

//go:build darwin || linux || freebsd || openbsd

package esp32

import "golang.org/x/sys/unix"

const platformRequiresDTRRefreshAfterRTS = false

type unixModemControl struct {
	fd int
}

func openModemControl(portName string) (modemControl, error) {
	fd, err := unix.Open(portName, unix.O_RDWR|unix.O_NOCTTY|unix.O_NDELAY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return &unixModemControl{fd: fd}, nil
}

func (c *unixModemControl) Set(dtr, rts bool) error {
	status, err := unix.IoctlGetInt(c.fd, unix.TIOCMGET)
	if err != nil {
		return err
	}
	if dtr {
		status |= unix.TIOCM_DTR
	} else {
		status &^= unix.TIOCM_DTR
	}
	if rts {
		status |= unix.TIOCM_RTS
	} else {
		status &^= unix.TIOCM_RTS
	}
	return unix.IoctlSetPointerInt(c.fd, unix.TIOCMSET, status)
}

func (c *unixModemControl) Close() error {
	return unix.Close(c.fd)
}

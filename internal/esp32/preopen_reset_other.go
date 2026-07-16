//go:build !windows

package esp32

const platformSupportsPreOpenReset = false

func preOpenBootloaderReset(string) error {
	return nil
}

//go:build windows

package esp32

// Some Windows USB serial drivers do not propagate an RTS-only DCB change.
// Reapplying DTR forces the combined control-line state to the adapter.
const platformRequiresDTRRefreshAfterRTS = true

func openModemControl(string) (modemControl, error) {
	return nil, nil
}

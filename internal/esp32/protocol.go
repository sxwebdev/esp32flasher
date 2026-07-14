package esp32

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"
)

// slipEncode frames data using SLIP.
func slipEncode(data []byte) []byte {
	var buf bytes.Buffer
	buf.WriteByte(SLIP_END)

	for _, b := range data {
		switch b {
		case SLIP_END:
			buf.WriteByte(SLIP_ESC)
			buf.WriteByte(SLIP_ESC_END)
		case SLIP_ESC:
			buf.WriteByte(SLIP_ESC)
			buf.WriteByte(SLIP_ESC_ESC)
		default:
			buf.WriteByte(b)
		}
	}

	buf.WriteByte(SLIP_END)
	return buf.Bytes()
}

// slipDecode decodes a SLIP frame.
func slipDecode(data []byte) ([]byte, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("packet too short: %d bytes", len(data))
	}

	// Locate the frame boundaries.
	start := -1
	end := -1

	for i := range data {
		if data[i] == SLIP_END {
			if start == -1 {
				start = i
			} else {
				end = i
				break
			}
		}
	}

	if start == -1 || end == -1 {
		return nil, fmt.Errorf("no valid SLIP packet found")
	}

	var buf bytes.Buffer
	escaped := false

	for i := start + 1; i < end; i++ {
		b := data[i]
		if escaped {
			switch b {
			case SLIP_ESC_END:
				buf.WriteByte(SLIP_END)
			case SLIP_ESC_ESC:
				buf.WriteByte(SLIP_ESC)
			default:
				return nil, fmt.Errorf("invalid escape sequence: 0x%02x", b)
			}
			escaped = false
		} else if b == SLIP_ESC {
			escaped = true
		} else {
			buf.WriteByte(b)
		}
	}

	if escaped {
		return nil, fmt.Errorf("packet ends with escape character")
	}

	return buf.Bytes(), nil
}

// extractSLIPFrame returns the first non-empty SLIP frame and preserves the
// remainder. The ROM may return multiple frames in a single serial read.
func extractSLIPFrame(data []byte) (frame, rest []byte, found bool) {
	for len(data) > 0 {
		start := bytes.IndexByte(data, SLIP_END)
		if start == -1 {
			return nil, nil, false
		}
		data = data[start:]

		relativeEnd := bytes.IndexByte(data[1:], SLIP_END)
		if relativeEnd == -1 {
			return nil, data, false
		}
		end := relativeEnd + 1
		if end == 1 { // Two adjacent delimiters do not form a packet.
			data = data[1:]
			continue
		}

		decoded, err := slipDecode(data[:end+1])
		data = data[end+1:]
		if err != nil || len(decoded) == 0 {
			continue
		}
		return decoded, data, true
	}

	return nil, nil, false
}

// sendCommand sends a command to the ESP32 ROM.
func (f *ESP32Flasher) sendCommand(cmd byte, data []byte, checksum uint32) error {
	packet := make([]byte, 8+len(data))
	packet[0] = 0x00                                              // Direction (request)
	packet[1] = cmd                                               // Command
	binary.LittleEndian.PutUint16(packet[2:4], uint16(len(data))) // Size
	binary.LittleEndian.PutUint32(packet[4:8], checksum)          // Checksum
	copy(packet[8:], data)                                        // Data

	encoded := slipEncode(packet)

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("📤 Sending command 0x%02x (%d data bytes)", cmd, len(data)))
	}

	return f.writeAll(encoded)
}

// sendCommandFast sends a command without logging on the hot path.
func (f *ESP32Flasher) sendCommandFast(cmd byte, data []byte, checksum uint32) error {
	packet := make([]byte, 8+len(data))
	packet[0] = 0x00                                              // Direction (request)
	packet[1] = cmd                                               // Command
	binary.LittleEndian.PutUint16(packet[2:4], uint16(len(data))) // Size
	binary.LittleEndian.PutUint32(packet[4:8], checksum)          // Checksum
	copy(packet[8:], data)                                        // Data

	encoded := slipEncode(packet)

	// Avoid logging here to maximize throughput.
	return f.writeAll(encoded)
}

func (f *ESP32Flasher) writeAll(data []byte) error {
	for len(data) > 0 {
		n, err := f.port.Write(data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

// readResponse reads and decodes an ESP32 ROM response.
func (f *ESP32Flasher) readResponse(timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	buffer := make([]byte, 1024)

	for {
		if frame, rest, ok := extractSLIPFrame(f.rxBuffer); ok {
			f.rxBuffer = rest
			return frame, nil
		} else {
			f.rxBuffer = rest
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("timeout reading SLIP response after %v", timeout)
		}
		readTimeout := min(remaining, 100*time.Millisecond)
		if err := f.port.SetReadTimeout(readTimeout); err != nil {
			return nil, fmt.Errorf("set read timeout: %w", err)
		}

		n, err := f.port.Read(buffer)
		if n > 0 {
			f.rxBuffer = append(f.rxBuffer, buffer[:n]...)
		}
		if err != nil && n == 0 && time.Now().After(deadline) {
			return nil, fmt.Errorf("read SLIP response: %w", err)
		}
	}
}

func (f *ESP32Flasher) readResponseForCommand(cmd byte, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		response, err := f.readResponse(time.Until(deadline))
		if err != nil {
			return nil, err
		}
		if len(response) >= 8 && response[0] == 0x01 && response[1] == cmd {
			return response, nil
		}
	}
	return nil, fmt.Errorf("timeout waiting for command 0x%02x response", cmd)
}

func checkROMStatus(response []byte) error {
	return checkROMStatusAfter(response, 0)
}

func checkROMStatusAfter(response []byte, responseDataLength int) error {
	statusOffset := 8 + responseDataLength
	if len(response) < statusOffset+2 {
		return fmt.Errorf("ROM response has no status bytes: %x", response)
	}
	status := response[statusOffset]
	if status == 0 {
		return nil
	}

	reason := response[statusOffset+1]
	reasonText := map[byte]string{
		0x05: "invalid or unsupported command",
		0x06: "command execution failed",
		0x07: "invalid CRC",
		0x08: "flash write error",
		0x09: "flash read error",
		0x0a: "flash read length error",
		0x0b: "deflate error",
	}[reason]
	if reasonText == "" {
		reasonText = "unknown ROM error"
	}

	return fmt.Errorf("ROM status=%d reason=0x%02x (%s), response=%x", status, reason, reasonText, response)
}

func (f *ESP32Flasher) flashMD5(offset, size uint32) (string, error) {
	data := make([]byte, 16)
	binary.LittleEndian.PutUint32(data[0:4], offset)
	binary.LittleEndian.PutUint32(data[4:8], size)

	if err := f.sendCommand(ESP_SPI_FLASH_MD5, data, 0); err != nil {
		return "", fmt.Errorf("send flash MD5 command: %w", err)
	}
	response, err := f.readResponseForCommand(ESP_SPI_FLASH_MD5, 60*time.Second)
	if err != nil {
		return "", fmt.Errorf("flash MD5 timeout: %w", err)
	}
	const digestLength = 32
	if len(response) < 8+digestLength {
		return "", fmt.Errorf("invalid flash MD5 response: %x", response)
	}
	if err := checkROMStatusAfter(response, digestLength); err != nil {
		return "", fmt.Errorf("calculate flash MD5: %w", err)
	}

	digest := string(response[8 : 8+digestLength])
	if _, err := hex.DecodeString(digest); err != nil {
		return "", fmt.Errorf("ROM returned invalid MD5 %q: %w", digest, err)
	}
	return strings.ToLower(digest), nil
}

// sync synchronizes with the ROM bootloader.
func (f *ESP32Flasher) sync() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Synchronizing with the ROM bootloader...")
	}

	syncData := make([]byte, 36)
	syncData[0] = 0x07
	syncData[1] = 0x07
	syncData[2] = 0x12
	syncData[3] = 0x20
	for i := 4; i < 36; i++ {
		syncData[i] = 0x55
	}

	for attempt := 1; attempt <= ESP_SYNC_ATTEMPTS; attempt++ {
		f.rxBuffer = nil
		if err := f.port.ResetInputBuffer(); err != nil {
			return fmt.Errorf("reset input buffer: %w", err)
		}
		if err := f.sendCommand(ESP_SYNC, syncData, 0); err != nil {
			return fmt.Errorf("send SYNC: %w", err)
		}

		_, err := f.readResponseForCommand(ESP_SYNC, time.Second)
		if err != nil {
			if f.callback != nil {
				f.callback.emitLog(fmt.Sprintf("⚠️ SYNC attempt %d/%d: %v", attempt, ESP_SYNC_ATTEMPTS, err))
			}
			continue
		}

		// One SYNC usually produces multiple responses. Drain them before
		// READ_REG so an old SYNC frame cannot satisfy the next command.
		time.Sleep(50 * time.Millisecond)
		f.rxBuffer = nil
		if err := f.port.ResetInputBuffer(); err != nil {
			return fmt.Errorf("drain SYNC responses: %w", err)
		}
		if f.callback != nil {
			f.callback.emitLog("✅ SYNC successful")
		}
		return nil
	}

	return fmt.Errorf("ESP32 did not answer SYNC")
}

// forceBootloaderStabilization retries resets to stabilize a noisy bootloader connection.
func (f *ESP32Flasher) forceBootloaderStabilization() error {
	if f.callback != nil {
		f.callback.emitLog("🔧 Stabilizing the bootloader with several consecutive resets...")
	}

	// Perform several quick resets to stabilize the connection.
	for range 3 {
		f.port.SetDTR(true) // GPIO0 = LOW (boot mode)
		f.port.SetRTS(true) // EN = LOW (reset)
		time.Sleep(50 * time.Millisecond)

		f.port.SetRTS(false) // EN = HIGH (release reset)
		time.Sleep(100 * time.Millisecond)

		// Drain stale bytes.
		f.port.ResetInputBuffer()
		f.port.ResetOutputBuffer()
		time.Sleep(200 * time.Millisecond)
	}

	f.port.SetDTR(false)               // GPIO0 = HIGH (release boot mode)
	time.Sleep(500 * time.Millisecond) // Allow the ROM bootloader to settle.

	// Retry SYNC after stabilization.
	syncData := make([]byte, 36)
	syncData[0] = 0x07
	syncData[1] = 0x07
	syncData[2] = 0x12
	syncData[3] = 0x20
	for i := 4; i < 36; i++ {
		syncData[i] = 0x55
	}

	for range 5 {
		f.port.ResetInputBuffer()
		f.port.ResetOutputBuffer()
		time.Sleep(300 * time.Millisecond)

		if err := f.sendCommand(ESP_SYNC, syncData, 0); err != nil {
			continue
		}

		response, err := f.readResponseRobust(2 * time.Second)
		if err != nil {
			continue
		}

		if len(response) >= 8 && response[0] == 0x01 && response[1] == ESP_SYNC {
			if f.callback != nil {
				f.callback.emitLog("✅ Bootloader stabilization successful!")
			}
			return nil
		}
	}

	return fmt.Errorf("failed to stabilize bootloader after all attempts")
}

// readResponseRobust reads a response while tolerating unrelated serial bytes.
func (f *ESP32Flasher) readResponseRobust(timeout time.Duration) ([]byte, error) {
	return f.readResponse(timeout)
}

// readResponseFast is the low-latency response path used for flash data.
func (f *ESP32Flasher) readResponseFast(timeout time.Duration) ([]byte, error) {
	return f.readResponse(timeout)
}

// testSync checks the ROM connection with a SYNC command.
func (f *ESP32Flasher) testSync() bool {
	f.rxBuffer = nil
	if err := f.port.ResetInputBuffer(); err != nil {
		return false
	}
	if err := f.port.ResetOutputBuffer(); err != nil {
		return false
	}

	syncData := make([]byte, 36)
	syncData[0] = 0x07
	syncData[1] = 0x07
	syncData[2] = 0x12
	syncData[3] = 0x20
	for i := 4; i < 36; i++ {
		syncData[i] = 0x55
	}

	if err := f.sendCommand(ESP_SYNC, syncData, 0); err != nil {
		return false
	}

	response, err := f.readResponseForCommand(ESP_SYNC, time.Second)
	if err != nil {
		return false
	}

	return len(response) >= 8 && response[0] == 0x01 && response[1] == ESP_SYNC
}

// detectChip identifies the ESP32 family member.
func (f *ESP32Flasher) detectChip() error {
	if f.callback != nil {
		f.callback.emitLog("🔍 Detecting chip type...")
	}

	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, CHIP_DETECT_MAGIC_REG_ADDR)

	if err := f.sendCommand(ESP_READ_REG, data, 0); err != nil {
		return fmt.Errorf("failed to read chip detect register: %w", err)
	}

	response, err := f.readResponseForCommand(ESP_READ_REG, 2*time.Second)
	if err != nil {
		return fmt.Errorf("failed to read chip detect response: %w", err)
	}

	if len(response) < 8 {
		return fmt.Errorf("invalid chip detect response length: %d", len(response))
	}

	// In a ROM response, READ_REG returns the register in the 32-bit value field
	// of the packet header, not in the status bytes after the header.
	regValue := binary.LittleEndian.Uint32(response[4:8])
	f.chipType = chipTypeFromMagic(regValue)

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("✅ Detected chip: %s (0x%08x)", f.chipType.String(), regValue))
	}

	return nil
}

func chipTypeFromMagic(regValue uint32) ChipType {
	switch regValue {
	case ESP32_CHIP_MAGIC:
		return CHIP_ESP32
	case ESP32S2_CHIP_MAGIC:
		return CHIP_ESP32S2
	case ESP32S3_CHIP_MAGIC:
		return CHIP_ESP32S3
	case ESP32C3_CHIP_MAGIC:
		return CHIP_ESP32C3
	default:
		return CHIP_UNKNOWN
	}
}

// spiAttach connects the ROM loader to SPI Flash.
func (f *ESP32Flasher) spiAttach() error {
	if f.callback != nil {
		f.callback.emitLog("🔗 Attaching SPI Flash...")
	}

	data := make([]byte, 8)
	binary.LittleEndian.PutUint32(data[0:4], 0)
	binary.LittleEndian.PutUint32(data[4:8], 0)

	if err := f.sendCommand(ESP_SPI_ATTACH, data, 0); err != nil {
		return fmt.Errorf("failed to send SPI attach: %w", err)
	}

	response, err := f.readResponseForCommand(ESP_SPI_ATTACH, 5*time.Second)
	if err != nil {
		return fmt.Errorf("SPI attach timeout: %w", err)
	}

	if len(response) < 8 || response[0] != 0x01 || response[1] != ESP_SPI_ATTACH {
		return fmt.Errorf("invalid SPI attach response")
	}
	if err := checkROMStatus(response); err != nil {
		return fmt.Errorf("SPI attach failed: %w", err)
	}

	if f.callback != nil {
		f.callback.emitLog("✅ SPI Flash attached")
	}

	return nil
}

func (f *ESP32Flasher) spiSetParameters(size uint32) error {
	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("💾 Configuring SPI Flash: %d MB", size/(1024*1024)))
	}

	data := makeSPIParameters(size)
	if err := f.sendCommand(ESP_SPI_SET_PARAMS, data, 0); err != nil {
		return fmt.Errorf("send SPI parameters: %w", err)
	}
	response, err := f.readResponseForCommand(ESP_SPI_SET_PARAMS, 5*time.Second)
	if err != nil {
		return fmt.Errorf("SPI parameters timeout: %w", err)
	}
	if len(response) < 8 || response[0] != 0x01 || response[1] != ESP_SPI_SET_PARAMS {
		return fmt.Errorf("invalid SPI parameters response")
	}
	if err := checkROMStatus(response); err != nil {
		return fmt.Errorf("set SPI parameters: %w", err)
	}

	return nil
}

func makeSPIParameters(size uint32) []byte {
	data := make([]byte, 24)
	binary.LittleEndian.PutUint32(data[0:4], 0)            // Let the ROM detect the Flash ID.
	binary.LittleEndian.PutUint32(data[4:8], size)         // Complete Flash chip size.
	binary.LittleEndian.PutUint32(data[8:12], 64*1024)     // Erase block.
	binary.LittleEndian.PutUint32(data[12:16], 4*1024)     // Erase sector.
	binary.LittleEndian.PutUint32(data[16:20], 256)        // Program page.
	binary.LittleEndian.PutUint32(data[20:24], 0x0000ffff) // Status mask.
	return data
}

// calculateChecksum computes the ROM XOR checksum.
func calculateChecksum(data []byte) uint32 {
	checksum := uint32(0xEF)
	for _, b := range data {
		checksum ^= uint32(b)
	}
	return checksum & 0xFF
}

// isInBootloader checks whether the ESP32 is running its ROM bootloader.
func (f *ESP32Flasher) isInBootloader() bool {
	f.port.ResetInputBuffer()
	f.port.ResetOutputBuffer()
	time.Sleep(300 * time.Millisecond)

	buffer := make([]byte, 2048)
	var bootloaderOutput strings.Builder

	for range 8 {
		f.port.SetReadTimeout(300 * time.Millisecond)
		n, _ := f.port.Read(buffer)
		if n > 0 {
			bootloaderOutput.WriteString(string(buffer[:n]))
		}
		time.Sleep(50 * time.Millisecond)
	}

	if f.callback != nil && bootloaderOutput.String() != "" {
		f.callback.emitLog(fmt.Sprintf("🔍 Bootloader output: %s", strings.TrimSpace(bootloaderOutput.String())))
	}

	// Look for bootloader output markers.
	isBootloader := strings.Contains(bootloaderOutput.String(), "waiting for download") ||
		strings.Contains(bootloaderOutput.String(), "download mode") ||
		strings.Contains(bootloaderOutput.String(), "Brownout") ||
		strings.Contains(bootloaderOutput.String(), "rst:0x10") ||
		strings.Contains(bootloaderOutput.String(), "boot:0x13") ||
		strings.Contains(bootloaderOutput.String(), "ets_main.c")

	// Application output means the reset did not enter download mode.
	if strings.Contains(bootloaderOutput.String(), "ESP32 RC Transmitter") ||
		strings.Contains(bootloaderOutput.String(), "WiFi") ||
		strings.Contains(bootloaderOutput.String(), "app_main") ||
		strings.Contains(bootloaderOutput.String(), "Firmware version") {
		if f.callback != nil {
			f.callback.emitLog("❌ ESP32 booted the application instead of the ROM bootloader")
		}
		return false
	}

	f.port.ResetInputBuffer()
	f.port.ResetOutputBuffer()
	time.Sleep(200 * time.Millisecond)

	return isBootloader || f.testSync()
}

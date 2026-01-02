package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"time"

	"go.bug.st/serial"
)

// ESP32 ROM bootloader opcodes
const (
	OpcodeFlashBegin     byte = 0x02
	OpcodeFlashData      byte = 0x03
	OpcodeFlashEnd       byte = 0x04
	OpcodeMemBegin       byte = 0x05
	OpcodeMemEnd         byte = 0x06
	OpcodeMemData        byte = 0x07
	OpcodeSync           byte = 0x08
	OpcodeWriteReg       byte = 0x09
	OpcodeReadReg        byte = 0x0a
	OpcodeSpiAttachFlash byte = 0x0d
	OpcodeReadFlash      byte = 0x0e
	OpcodeChangeBaudrate byte = 0x0f
	OpcodeFlashDeflBegin byte = 0x10
	OpcodeFlashDeflData  byte = 0x11
	OpcodeFlashDeflEnd   byte = 0x12
	OpcodeSpiFlashMd5    byte = 0x13

	// SLIP protocol constants
	SlipEnd    byte = 0xC0
	SlipEsc    byte = 0xDB
	SlipEscEnd byte = 0xDC
	SlipEscEsc byte = 0xDD

	// Flash constants
	FlashBlockSize uint32 = 0x4000 // 16KB blocks for faster write
	FlashReadMax   uint32 = 64     // max bytes per read
)

// ProgressCallback interface for progress callbacks
type ProgressCallback interface {
	emitProgress(progress int, message string)
	emitLog(message string)
}

// ESP32Flasher structure for working with ESP32
type ESP32Flasher struct {
	port          serial.Port
	portName      string
	callback      ProgressCallback
	flashAttached bool
}

// NewESP32FlasherWithProgress creates a new flasher instance with progress callbacks
func NewESP32FlasherWithProgress(portName string, callback ProgressCallback) (*ESP32Flasher, error) {
	mode := &serial.Mode{
		BaudRate: 115200,
		Parity:   serial.NoParity,
		DataBits: 8,
		StopBits: serial.OneStopBit,
	}

	port, err := serial.Open(portName, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to open port: %w", err)
	}

	flasher := &ESP32Flasher{
		port:     port,
		portName: portName,
		callback: callback,
	}

	// Try to enter bootloader mode
	if err := flasher.enterBootloader(); err != nil {
		port.Close()
		return nil, fmt.Errorf("failed to enter bootloader: %w", err)
	}

	return flasher, nil
}

// Close closes the connection
func (f *ESP32Flasher) Close() error {
	return f.port.Close()
}

// enterBootloader puts ESP32 into bootloader mode using reset sequence from Fluepke/esptool
func (f *ESP32Flasher) enterBootloader() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Entering bootloader mode...")
	}

	// Reset sequence based on Fluepke/esptool (proven to work):
	// DTR controls GPIO0: false=HIGH (normal), true=LOW (bootloader)
	// RTS controls EN: true=reset, false=running

	// Step 1: Set IO0=HIGH (normal operation)
	f.port.SetDTR(false)
	// Step 2: Set EN=LOW (chip in reset)
	f.port.SetRTS(true)
	time.Sleep(100 * time.Millisecond)

	// Step 3: Set IO0=LOW (bootloader mode)
	f.port.SetDTR(true)
	// Step 4: Set EN=HIGH (chip out of reset, enters bootloader)
	f.port.SetRTS(false)
	time.Sleep(50 * time.Millisecond)

	// Flush buffers
	f.port.ResetInputBuffer()
	f.port.ResetOutputBuffer()

	// Wait for bootloader to be ready
	time.Sleep(100 * time.Millisecond)

	// Try to sync with bootloader
	for attempt := 0; attempt < 5; attempt++ {
		if f.callback != nil {
			f.callback.emitLog(fmt.Sprintf("🔗 Sync attempt %d/5...", attempt+1))
		}

		if err := f.sync(); err == nil {
			if f.callback != nil {
				f.callback.emitLog("✅ ESP32 entered bootloader mode")
			}
			return nil
		}

		// Try reset again with inverted logic for some adapters
		if attempt == 2 {
			if f.callback != nil {
				f.callback.emitLog("⚠️ Trying inverted DTR/RTS logic...")
			}
			// Inverted logic reset
			f.port.SetDTR(true)
			f.port.SetRTS(false)
			time.Sleep(100 * time.Millisecond)
			f.port.SetDTR(false)
			f.port.SetRTS(true)
			time.Sleep(50 * time.Millisecond)
			f.port.SetRTS(false)
			time.Sleep(100 * time.Millisecond)
			f.port.ResetInputBuffer()
		}
	}

	if f.callback != nil {
		f.callback.emitLog("❌ Failed to enter bootloader mode")
		f.callback.emitLog("💡 Try manual mode:")
		f.callback.emitLog("   1. Hold BOOT button (GPIO0)")
		f.callback.emitLog("   2. Press and release RESET button (EN)")
		f.callback.emitLog("   3. Release BOOT button")
		f.callback.emitLog("   4. Flash again")
	}

	return fmt.Errorf("failed to enter bootloader mode after 5 attempts")
}

// slipEncode encodes data using SLIP protocol
func slipEncode(data []byte) []byte {
	// Replace escape chars first, then headers
	escaped := bytes.ReplaceAll(data, []byte{SlipEsc}, []byte{SlipEsc, SlipEscEsc})
	escaped = bytes.ReplaceAll(escaped, []byte{SlipEnd}, []byte{SlipEsc, SlipEscEnd})

	result := make([]byte, len(escaped)+2)
	result[0] = SlipEnd
	copy(result[1:], escaped)
	result[len(result)-1] = SlipEnd
	return result
}

// slipRead reads a SLIP frame with timeout
func (f *ESP32Flasher) slipRead(timeout time.Duration) ([]byte, error) {
	startTime := time.Now()
	result := make([]byte, 0, 1024)
	byteBuf := make([]byte, 1)
	inFrame := false
	inEscape := false

	for {
		if time.Since(startTime) > timeout {
			return nil, fmt.Errorf("read timeout after %v, received %d bytes", time.Since(startTime), len(result))
		}

		f.port.SetReadTimeout(10 * time.Millisecond)
		n, err := f.port.Read(byteBuf)
		if err != nil || n == 0 {
			continue
		}

		b := byteBuf[0]

		if !inFrame {
			if b == SlipEnd {
				inFrame = true
			}
			continue
		}

		if inEscape {
			switch b {
			case SlipEscEnd:
				result = append(result, SlipEnd)
			case SlipEscEsc:
				result = append(result, SlipEsc)
			default:
				return nil, fmt.Errorf("unexpected byte 0x%02x after escape", b)
			}
			inEscape = false
			continue
		}

		switch b {
		case SlipEnd:
			if len(result) > 0 {
				return result, nil
			}
		case SlipEsc:
			inEscape = true
		default:
			result = append(result, b)
		}
	}
}

// sendCommand sends a command to ESP32
func (f *ESP32Flasher) sendCommand(opcode byte, data []byte, checksum uint32) error {
	// Build command packet: [direction(1)][opcode(1)][size(2)][checksum(4)][data(N)]
	packet := make([]byte, 8+len(data))
	packet[0] = 0x00 // Direction: request
	packet[1] = opcode
	binary.LittleEndian.PutUint16(packet[2:4], uint16(len(data)))
	binary.LittleEndian.PutUint32(packet[4:8], checksum)
	copy(packet[8:], data)

	encoded := slipEncode(packet)
	_, err := f.port.Write(encoded)
	return err
}

// executeCommand sends command and waits for response
func (f *ESP32Flasher) executeCommand(opcode byte, data []byte, checksum uint32, timeout time.Duration) ([]byte, error) {
	if err := f.sendCommand(opcode, data, checksum); err != nil {
		return nil, err
	}

	// Try to read response, retry if opcode doesn't match
	for i := 0; i < 16; i++ {
		response, err := f.slipRead(timeout)
		if err != nil {
			return nil, err
		}

		if len(response) < 8 {
			continue
		}

		// Check direction (should be 0x01 for response) and opcode
		if response[0] != 0x01 {
			continue
		}
		if response[1] != opcode {
			continue
		}

		return response, nil
	}

	return nil, fmt.Errorf("no valid response after 16 retries")
}

// checkExecuteCommand executes command with retries and status check
func (f *ESP32Flasher) checkExecuteCommand(opcode byte, data []byte, checksum uint32, timeout time.Duration, retries int) ([]byte, error) {
	var lastErr error

	for i := 0; i < retries; i++ {
		response, err := f.executeCommand(opcode, data, checksum, timeout)
		if err != nil {
			lastErr = err
			continue
		}

		// Check status in response
		// Response format: [direction(1)][opcode(1)][size(2)][value(4)][data...][status(2)]
		if len(response) >= 10 {
			status := response[len(response)-2]
			if status == 0 {
				return response, nil
			}
			errorCode := response[len(response)-1]
			lastErr = fmt.Errorf("command 0x%02x failed: status=%d, error=%d", opcode, status, errorCode)
		} else {
			return response, nil // Short response, assume success
		}
	}

	return nil, lastErr
}

// sync synchronizes with ESP32 bootloader
func (f *ESP32Flasher) sync() error {
	// Sync payload: 0x07, 0x07, 0x12, 0x20 followed by 32 bytes of 0x55
	payload := make([]byte, 36)
	payload[0] = 0x07
	payload[1] = 0x07
	payload[2] = 0x12
	payload[3] = 0x20
	for i := 4; i < 36; i++ {
		payload[i] = 0x55
	}

	// Clear buffers before sync
	f.port.ResetInputBuffer()

	response, err := f.executeCommand(OpcodeSync, payload, 0, 1*time.Second)
	if err != nil {
		return err
	}

	// Check response
	if len(response) < 8 || response[0] != 0x01 || response[1] != OpcodeSync {
		return fmt.Errorf("invalid sync response")
	}

	return nil
}

// attachSpiFlash attaches SPI flash
func (f *ESP32Flasher) attachSpiFlash() error {
	if f.callback != nil {
		f.callback.emitLog("🔗 Attaching SPI Flash...")
	}

	data := make([]byte, 8) // 8 bytes of zeros for default SPI
	_, err := f.checkExecuteCommand(OpcodeSpiAttachFlash, data, 0, 100*time.Millisecond, 3)
	if err != nil {
		return err
	}

	f.flashAttached = true
	if f.callback != nil {
		f.callback.emitLog("✅ SPI Flash attached")
	}
	return nil
}

// calculateChecksum calculates checksum for data
func calculateChecksum(data []byte) uint32 {
	checksum := uint32(0xEF)
	for _, b := range data {
		checksum ^= uint32(b)
	}
	return checksum
}

// compressData compresses data using zlib
func compressData(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := zlib.NewWriterLevel(&buf, 9)
	if err != nil {
		return nil, err
	}
	_, err = w.Write(data)
	if err != nil {
		w.Close()
		return nil, err
	}
	w.Close()
	return buf.Bytes(), nil
}

// changeBaudRate changes baud rate for faster transfer
func (f *ESP32Flasher) changeBaudRate(newBaud int) error {
	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("⚡ Switching to %d baud...", newBaud))
	}

	// Send change baudrate command
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint32(payload[0:4], uint32(newBaud))
	binary.LittleEndian.PutUint32(payload[4:8], 0) // old baud (0 = don't care)

	_, err := f.checkExecuteCommand(OpcodeChangeBaudrate, payload, 0, 1*time.Second, 3)
	if err != nil {
		return err
	}

	// Change local port baud rate
	time.Sleep(50 * time.Millisecond)
	if err := f.port.SetMode(&serial.Mode{
		BaudRate: newBaud,
		Parity:   serial.NoParity,
		DataBits: 8,
		StopBits: serial.OneStopBit,
	}); err != nil {
		return err
	}

	time.Sleep(50 * time.Millisecond)
	f.port.ResetInputBuffer()

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("✅ Speed: %d baud", newBaud))
	}
	return nil
}

// FlashData flashes data to ESP32
func (f *ESP32Flasher) FlashData(data []byte, offset uint32, portName string) error {
	// 0. Try to increase baud rate for faster transfer
	if err := f.changeBaudRate(460800); err != nil {
		if f.callback != nil {
			f.callback.emitLog("⚠️ Failed to increase speed, continuing at 115200")
		}
	}

	// 1. Attach SPI flash if not attached
	if !f.flashAttached {
		if err := f.attachSpiFlash(); err != nil {
			return fmt.Errorf("SPI attach failed: %w", err)
		}
	}

	// 2. Compress data
	if f.callback != nil {
		f.callback.emitLog("📦 Compressing data...")
		f.callback.emitProgress(40, "Compressing...")
	}

	compressed, err := compressData(data)
	if err != nil {
		return fmt.Errorf("compression failed: %w", err)
	}

	if f.callback != nil {
		ratio := float64(len(compressed)) / float64(len(data)) * 100
		f.callback.emitLog(fmt.Sprintf("📦 Compressed: %d → %d bytes (%.1f%%)", len(data), len(compressed), ratio))
	}

	// 3. Calculate blocks
	numBlocks := (uint32(len(compressed)) + FlashBlockSize - 1) / FlashBlockSize
	uncompressedSize := (uint32(len(data)) + FlashBlockSize - 1) / FlashBlockSize * FlashBlockSize

	// 4. Begin flash (deflate mode)
	if f.callback != nil {
		f.callback.emitLog("🗑️ Erasing flash sectors...")
		f.callback.emitProgress(50, "Erasing flash...")
	}

	beginPayload := make([]byte, 16)
	binary.LittleEndian.PutUint32(beginPayload[0:4], uncompressedSize) // erase size
	binary.LittleEndian.PutUint32(beginPayload[4:8], numBlocks)        // num blocks
	binary.LittleEndian.PutUint32(beginPayload[8:12], FlashBlockSize)  // block size
	binary.LittleEndian.PutUint32(beginPayload[12:16], offset)         // offset

	// Timeout depends on size - erasing 2MB can take 30+ seconds
	eraseTimeout := 30 * time.Second
	if uncompressedSize > 1024*1024 {
		eraseTimeout = 60 * time.Second // 60 seconds for > 1MB
	}
	_, err = f.checkExecuteCommand(OpcodeFlashDeflBegin, beginPayload, 0, eraseTimeout, 3)
	if err != nil {
		return fmt.Errorf("flash begin failed: %w", err)
	}

	if f.callback != nil {
		f.callback.emitLog("✅ Flash ready for writing")
	}

	time.Sleep(10 * time.Millisecond)

	// 5. Send data blocks
	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("📤 Sending data (%d blocks)...", numBlocks))
		f.callback.emitProgress(60, "Sending data...")
	}

	sent := uint32(0)
	total := uint32(len(compressed))
	sequence := uint32(0)

	for sent < total {
		blockLen := total - sent
		if blockLen > FlashBlockSize {
			blockLen = FlashBlockSize
		}

		block := compressed[sent : sent+blockLen]
		checksum := calculateChecksum(block)

		// Build data packet: [size(4)][sequence(4)][0(4)][0(4)][data]
		dataPayload := make([]byte, 16+len(block))
		binary.LittleEndian.PutUint32(dataPayload[0:4], uint32(len(block)))
		binary.LittleEndian.PutUint32(dataPayload[4:8], sequence)
		binary.LittleEndian.PutUint32(dataPayload[8:12], 0)
		binary.LittleEndian.PutUint32(dataPayload[12:16], 0)
		copy(dataPayload[16:], block)

		// Try to send block with retries
		var blockErr error
		for retry := 0; retry < 3; retry++ {
			_, blockErr = f.checkExecuteCommand(OpcodeFlashDeflData, dataPayload, checksum, 10*time.Second, 3)
			if blockErr == nil {
				break
			}
			if f.callback != nil {
				f.callback.emitLog(fmt.Sprintf("⚠️ Block %d error, retry %d/3", sequence, retry+1))
			}
		}

		if blockErr != nil {
			return fmt.Errorf("flash data failed at block %d: %w", sequence, blockErr)
		}

		sent += blockLen
		sequence++

		// Update progress
		if f.callback != nil {
			progress := 60 + int(float64(sent)/float64(total)*35) // 60-95%
			percent := float64(sent) / float64(total) * 100
			f.callback.emitProgress(progress, fmt.Sprintf("Writing %.1f%%", percent))

			if sequence%10 == 0 || sent >= total {
				f.callback.emitLog(fmt.Sprintf("📦 Written %d/%d blocks (%.1f%%)", sequence, numBlocks, percent))
			}
		}
	}

	// 6. End flash (optional - some bootloaders don't require this)
	if f.callback != nil {
		f.callback.emitLog("🔄 Finishing flash...")
		f.callback.emitProgress(95, "Finishing...")
	}

	// Try to send flash end, but don't fail if it doesn't work
	endPayload := make([]byte, 4)
	binary.LittleEndian.PutUint32(endPayload, 0) // 0 = reboot
	f.checkExecuteCommand(OpcodeFlashDeflEnd, endPayload, 0, 3*time.Second, 1)

	if f.callback != nil {
		f.callback.emitProgress(100, "")
		f.callback.emitLog("✅ Flash complete!")
	}

	return nil
}

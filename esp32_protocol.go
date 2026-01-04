package main

import (
	"bytes"
	"compress/zlib"
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"

	"go.bug.st/serial"
)

// ESP32 stub loader data from esptool (https://github.com/espressif/esptool)
// This stub is uploaded to ESP32 RAM and provides proper flash erase/write/verify functionality
// Source: https://raw.githubusercontent.com/espressif/esptool/master/esptool/targets/stub_flasher/1/esp32.json
var esp32StubData = struct {
	Entry     uint32
	Text      string // base64 encoded
	TextStart uint32
	Data      string // base64 encoded
	DataStart uint32
}{
	Entry:     0x400BE658, // 1074521688 decimal
	TextStart: 0x400BE000, // 1074520064 decimal
	DataStart: 0x3FFDEBAC, // 1073605548 decimal
	Text:      "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",
	Data:      "DMD8P1XoC0Dr6AtAd+0LQIvpC0AO6QtAi+kLQOTpC0Dr6gtAYesLQAbrC0AB6AtAl+oLQODqC0AC6gtAgusLQCzqC0CC6wtA4ugLQETpC0CL6QtA5OkLQPToC0BP7AtAO+0LQCLnC0Bb7QtAIucLQCLnC0Ai5wtAIucLQCLnC0Ai5wtAIucLQCLnC0Dj6wtAIucLQGrsC0A77QtA",
}

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
	OpcodeSpiSetParams   byte = 0x0b
	OpcodeSpiAttachFlash byte = 0x0d
	OpcodeReadFlash      byte = 0x0e
	OpcodeChangeBaudrate byte = 0x0f
	OpcodeFlashDeflBegin byte = 0x10
	OpcodeFlashDeflData  byte = 0x11
	OpcodeFlashDeflEnd   byte = 0x12
	OpcodeSpiFlashMd5    byte = 0x13
	OpcodeEraseFlash     byte = 0xd0
	OpcodeEraseRegion    byte = 0xd1
	OpcodeReadFlashSlow  byte = 0xd2
	OpcodeRunUserCode    byte = 0xd3

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

// hardReset performs hardware reset of ESP32 using DTR/RTS lines
func (f *ESP32Flasher) hardReset() {
	// Reset sequence: pull EN low, then high (normal boot, not bootloader)
	f.port.SetDTR(false) // IO0 = HIGH (normal mode)
	f.port.SetRTS(true)  // EN = LOW (reset)
	time.Sleep(100 * time.Millisecond)
	f.port.SetRTS(false) // EN = HIGH (run)
	time.Sleep(100 * time.Millisecond)
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

	// SPI_ATTACH command: 8 bytes, all zeros for default internal flash
	data := make([]byte, 8)
	_, err := f.checkExecuteCommand(OpcodeSpiAttachFlash, data, 0, 500*time.Millisecond, 3)
	if err != nil {
		return err
	}

	if f.callback != nil {
		f.callback.emitLog("✅ SPI Flash attached")
	}

	// Configure SPI flash parameters for 4MB flash (common ESP32 size)
	// This helps the ROM bootloader properly handle flash operations
	if f.callback != nil {
		f.callback.emitLog("⚙️ Configuring flash parameters...")
	}

	// SPI_SET_PARAMS: fl_id(4), total_size(4), block_size(4), sector_size(4), page_size(4), status_mask(4)
	// Common values: 4MB flash, 64KB block, 4KB sector, 256B page
	spiParams := make([]byte, 24)
	binary.LittleEndian.PutUint32(spiParams[0:4], 0)           // fl_id: 0 = auto-detect
	binary.LittleEndian.PutUint32(spiParams[4:8], 4*1024*1024) // total_size: 4MB
	binary.LittleEndian.PutUint32(spiParams[8:12], 64*1024)    // block_size: 64KB
	binary.LittleEndian.PutUint32(spiParams[12:16], 4*1024)    // sector_size: 4KB
	binary.LittleEndian.PutUint32(spiParams[16:20], 256)       // page_size: 256B
	binary.LittleEndian.PutUint32(spiParams[20:24], 0xFFFF)    // status_mask

	_, err = f.checkExecuteCommand(OpcodeSpiSetParams, spiParams, 0, 500*time.Millisecond, 3)
	if err != nil {
		if f.callback != nil {
			f.callback.emitLog(fmt.Sprintf("⚠️ SPI params config failed (may be OK): %v", err))
		}
		// Don't fail - some ROM versions don't support this
	} else {
		if f.callback != nil {
			f.callback.emitLog("✅ Flash parameters configured")
		}
	}

	f.flashAttached = true
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

// uploadStubLoader uploads the stub loader to ESP32 RAM and starts it
func (f *ESP32Flasher) uploadStubLoader() error {
	if f.callback != nil {
		f.callback.emitLog("📤 Uploading stub loader...")
	}

	// Decode text segment (code to IRAM)
	textData, err := base64.StdEncoding.DecodeString(esp32StubData.Text)
	if err != nil {
		return fmt.Errorf("failed to decode stub text: %w", err)
	}

	// Decode data segment (data to DRAM)
	dataData, err := base64.StdEncoding.DecodeString(esp32StubData.Data)
	if err != nil {
		return fmt.Errorf("failed to decode stub data: %w", err)
	}

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("   Text: %d bytes → 0x%08X", len(textData), esp32StubData.TextStart))
		f.callback.emitLog(fmt.Sprintf("   Data: %d bytes → 0x%08X", len(dataData), esp32StubData.DataStart))
	}

	// Upload text segment to IRAM
	if err := f.memWrite(textData, esp32StubData.TextStart); err != nil {
		return fmt.Errorf("failed to upload stub text: %w", err)
	}

	// Upload data segment to DRAM
	if err := f.memWrite(dataData, esp32StubData.DataStart); err != nil {
		return fmt.Errorf("failed to upload stub data: %w", err)
	}

	// Start the stub
	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("🚀 Starting stub at 0x%08X...", esp32StubData.Entry))
	}

	if err := f.memEnd(esp32StubData.Entry); err != nil {
		return fmt.Errorf("failed to start stub: %w", err)
	}

	// Wait for OHAI response from stub
	if err := f.waitForOHAI(); err != nil {
		return fmt.Errorf("stub did not respond: %w", err)
	}

	if f.callback != nil {
		f.callback.emitLog("✅ Stub loader running")
	}

	return nil
}

// memWrite writes data to ESP32 RAM using MEM_BEGIN/MEM_DATA/MEM_END
func (f *ESP32Flasher) memWrite(data []byte, address uint32) error {
	const memBlockSize = 0x1800 // 6KB blocks for memory upload

	dataLen := uint32(len(data))
	numBlocks := (dataLen + memBlockSize - 1) / memBlockSize

	// MEM_BEGIN: [size(4)][num_blocks(4)][block_size(4)][offset(4)]
	beginPayload := make([]byte, 16)
	binary.LittleEndian.PutUint32(beginPayload[0:4], dataLen)
	binary.LittleEndian.PutUint32(beginPayload[4:8], numBlocks)
	binary.LittleEndian.PutUint32(beginPayload[8:12], memBlockSize)
	binary.LittleEndian.PutUint32(beginPayload[12:16], address)

	_, err := f.checkExecuteCommand(OpcodeMemBegin, beginPayload, 0, 3*time.Second, 3)
	if err != nil {
		return fmt.Errorf("MEM_BEGIN failed: %w", err)
	}

	// Send data blocks
	sent := uint32(0)
	sequence := uint32(0)

	for sent < dataLen {
		blockLen := dataLen - sent
		if blockLen > memBlockSize {
			blockLen = memBlockSize
		}

		block := data[sent : sent+blockLen]
		checksum := calculateChecksum(block)

		// MEM_DATA: [size(4)][sequence(4)][0(4)][0(4)][data]
		dataPayload := make([]byte, 16+len(block))
		binary.LittleEndian.PutUint32(dataPayload[0:4], blockLen)
		binary.LittleEndian.PutUint32(dataPayload[4:8], sequence)
		binary.LittleEndian.PutUint32(dataPayload[8:12], 0)
		binary.LittleEndian.PutUint32(dataPayload[12:16], 0)
		copy(dataPayload[16:], block)

		_, err := f.checkExecuteCommand(OpcodeMemData, dataPayload, checksum, 3*time.Second, 3)
		if err != nil {
			return fmt.Errorf("MEM_DATA failed at block %d: %w", sequence, err)
		}

		sent += blockLen
		sequence++
	}

	return nil
}

// memEnd finishes memory upload and optionally executes code at entry point
func (f *ESP32Flasher) memEnd(entryPoint uint32) error {
	// MEM_END: [execute(4)][entry_point(4)]
	// execute: 0 = run, 1 = don't run
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint32(payload[0:4], 0) // 0 = execute
	binary.LittleEndian.PutUint32(payload[4:8], entryPoint)

	// When executing code, the ROM may reset/jump before sending response
	// Use short timeout and don't fail on timeout - just send the command
	if err := f.sendCommand(OpcodeMemEnd, payload, 0); err != nil {
		return err
	}

	// Try to read response but don't require it - stub execution may have already started
	// ROM has ~200ms timeout before code execution resets UART
	f.port.SetReadTimeout(200 * time.Millisecond)
	response := make([]byte, 64)
	f.port.Read(response) // Ignore result

	return nil
}

// waitForOHAI waits for the stub loader to send "OHAI" response
func (f *ESP32Flasher) waitForOHAI() error {
	// The stub sends "OHAI" as a SLIP frame: 0xC0 "OHAI" 0xC0
	// We need to read a SLIP frame and check for OHAI

	startTime := time.Now()
	timeout := 5 * time.Second
	accumulated := make([]byte, 0, 256)

	for time.Since(startTime) < timeout {
		f.port.SetReadTimeout(100 * time.Millisecond)
		buffer := make([]byte, 64)
		n, err := f.port.Read(buffer)
		if err != nil || n == 0 {
			continue
		}

		accumulated = append(accumulated, buffer[:n]...)

		// Look for "OHAI" in accumulated data
		if bytes.Contains(accumulated, []byte("OHAI")) {
			return nil
		}

		// Also check for SLIP-framed OHAI (0xC0 OHAI 0xC0)
		// The stub might send it as a proper SLIP frame
		for i := 0; i < len(accumulated)-5; i++ {
			if accumulated[i] == 0xC0 {
				// Check if next bytes spell OHAI followed by 0xC0
				if i+5 <= len(accumulated) &&
					accumulated[i+1] == 'O' &&
					accumulated[i+2] == 'H' &&
					accumulated[i+3] == 'A' &&
					accumulated[i+4] == 'I' {
					return nil
				}
			}
		}
	}

	// Debug: show what we received
	if f.callback != nil && len(accumulated) > 0 {
		hexStr := hex.EncodeToString(accumulated)
		if len(hexStr) > 64 {
			hexStr = hexStr[:64] + "..."
		}
		f.callback.emitLog(fmt.Sprintf("   Received: %s", hexStr))
	}

	return fmt.Errorf("timeout waiting for OHAI response (received %d bytes)", len(accumulated))
}

// compressData compresses data using zlib
func compressData(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := zlib.NewWriterLevel(&buf, zlib.BestCompression)
	if err != nil {
		return nil, err
	}
	_, err = w.Write(data)
	if err != nil {
		w.Close()
		return nil, err
	}
	err = w.Close()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// FlashData flashes data to ESP32 using stub loader with compression
func (f *ESP32Flasher) FlashData(data []byte, offset uint32, portName string) error {
	// 1. Upload and start stub loader (this is critical for proper flash operations)
	if err := f.uploadStubLoader(); err != nil {
		return fmt.Errorf("failed to upload stub loader: %w", err)
	}

	// 2. Change baud rate for faster transfer (stub supports higher speeds)
	if err := f.changeBaudRateStub(921600); err != nil {
		if f.callback != nil {
			f.callback.emitLog("⚠️ Failed to increase speed to 921600, trying 460800...")
		}
		if err := f.changeBaudRateStub(460800); err != nil {
			if f.callback != nil {
				f.callback.emitLog("⚠️ Failed to increase speed, continuing at 115200...")
			}
		}
	}

	// 3. Compress data
	if f.callback != nil {
		f.callback.emitLog("🗜️ Compressing firmware...")
	}

	compressedData, err := compressData(data)
	if err != nil {
		return fmt.Errorf("compression failed: %w", err)
	}

	compressionRatio := float64(len(compressedData)) / float64(len(data)) * 100
	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("   Original: %d bytes", len(data)))
		f.callback.emitLog(fmt.Sprintf("   Compressed: %d bytes (%.1f%%)", len(compressedData), compressionRatio))
	}

	// 4. Calculate blocks for compressed data
	dataSize := uint32(len(data))           // Original (uncompressed) size
	compSize := uint32(len(compressedData)) // Compressed size
	numBlocks := (compSize + FlashBlockSize - 1) / FlashBlockSize

	// Timeout for erase - stub handles this properly
	eraseTimeout := 60 * time.Second
	if dataSize > 2*1024*1024 {
		eraseTimeout = 120 * time.Second
	}

	// 5. Begin flash with compressed mode (FLASH_DEFL_BEGIN)
	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("📝 Starting flash at 0x%X (%d bytes)...", offset, dataSize))
		f.callback.emitProgress(30, "Erasing flash...")
	}

	// FLASH_DEFL_BEGIN parameters for stub:
	// [erase_size(4)][num_blocks(4)][block_size(4)][offset(4)][encrypted(4)]
	beginPayload := make([]byte, 20)
	binary.LittleEndian.PutUint32(beginPayload[0:4], dataSize)        // uncompressed size (for erase calculation)
	binary.LittleEndian.PutUint32(beginPayload[4:8], numBlocks)       // number of compressed blocks
	binary.LittleEndian.PutUint32(beginPayload[8:12], FlashBlockSize) // block size
	binary.LittleEndian.PutUint32(beginPayload[12:16], offset)        // flash offset
	binary.LittleEndian.PutUint32(beginPayload[16:20], 0)             // not encrypted

	startBegin := time.Now()
	_, err = f.checkExecuteCommand(OpcodeFlashDeflBegin, beginPayload, 0, eraseTimeout, 3)
	if err != nil {
		return fmt.Errorf("flash begin failed: %w", err)
	}
	eraseTime := time.Since(startBegin)

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("✅ Flash erased in %v", eraseTime.Round(time.Millisecond)))
	}

	// 6. Send compressed data blocks (FLASH_DEFL_DATA)
	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("📤 Sending %d compressed blocks...", numBlocks))
		f.callback.emitProgress(40, "Writing...")
	}

	sent := uint32(0)
	total := compSize
	sequence := uint32(0)

	for sent < total {
		blockLen := total - sent
		if blockLen > FlashBlockSize {
			blockLen = FlashBlockSize
		}

		block := compressedData[sent : sent+blockLen]
		checksum := calculateChecksum(block)

		// FLASH_DEFL_DATA: [size(4)][sequence(4)][0(4)][0(4)][compressed_data]
		dataPayload := make([]byte, 16+len(block))
		binary.LittleEndian.PutUint32(dataPayload[0:4], blockLen)
		binary.LittleEndian.PutUint32(dataPayload[4:8], sequence)
		binary.LittleEndian.PutUint32(dataPayload[8:12], 0)
		binary.LittleEndian.PutUint32(dataPayload[12:16], 0)
		copy(dataPayload[16:], block)

		// Send block with retries
		var blockErr error
		for retry := range 3 {
			_, blockErr = f.checkExecuteCommand(OpcodeFlashDeflData, dataPayload, checksum, 10*time.Second, 3)
			if blockErr == nil {
				break
			}
			if f.callback != nil {
				f.callback.emitLog(fmt.Sprintf("⚠️ Block %d error, retry %d/3: %v", sequence, retry+1, blockErr))
			}
			time.Sleep(100 * time.Millisecond)
		}

		if blockErr != nil {
			return fmt.Errorf("flash data failed at block %d: %w", sequence, blockErr)
		}

		sent += blockLen
		sequence++

		// Update progress
		if f.callback != nil {
			progress := 40 + int(float64(sent)/float64(total)*45) // 40-85%
			percent := float64(sent) / float64(total) * 100
			f.callback.emitProgress(progress, fmt.Sprintf("Writing %.1f%%", percent))

			if sequence%20 == 0 || sent >= total {
				f.callback.emitLog(fmt.Sprintf("📦 Sent %d/%d blocks (%.1f%%)", sequence, numBlocks, percent))
			}
		}
	}

	// 7. End flash (FLASH_DEFL_END)
	if f.callback != nil {
		f.callback.emitLog("🔄 Finishing flash...")
		f.callback.emitProgress(88, "Finishing...")
	}

	// FLASH_DEFL_END: [reboot(4)] - 0 = reboot, 1 = don't reboot
	endPayload := make([]byte, 4)
	binary.LittleEndian.PutUint32(endPayload, 1) // 1 = don't reboot yet (we want to verify first)
	f.checkExecuteCommand(OpcodeFlashDeflEnd, endPayload, 0, 3*time.Second, 1)

	// 8. Verify MD5
	if f.callback != nil {
		f.callback.emitLog("🔍 Verifying flash...")
		f.callback.emitProgress(90, "Verifying...")
	}

	expectedMD5 := md5.Sum(data)
	expectedHex := hex.EncodeToString(expectedMD5[:])

	actualMD5, err := f.readFlashMD5(offset, dataSize)
	if err != nil {
		if f.callback != nil {
			f.callback.emitLog(fmt.Sprintf("⚠️ MD5 verification failed: %v", err))
			f.callback.emitLog(fmt.Sprintf("   Expected: %s", expectedHex))
		}
	} else {
		if actualMD5 == expectedHex {
			if f.callback != nil {
				f.callback.emitLog(fmt.Sprintf("✅ MD5 verified: %s", actualMD5))
			}
		} else {
			if f.callback != nil {
				f.callback.emitLog("❌ MD5 mismatch!")
				f.callback.emitLog(fmt.Sprintf("   Expected: %s", expectedHex))
				f.callback.emitLog(fmt.Sprintf("   Got:      %s", actualMD5))
			}
			return fmt.Errorf("MD5 verification failed: expected %s, got %s", expectedHex, actualMD5)
		}
	}

	// 9. Hard reset ESP32
	if f.callback != nil {
		f.callback.emitLog("🔄 Resetting ESP32...")
		f.callback.emitProgress(95, "Resetting...")
	}
	f.hardReset()

	if f.callback != nil {
		f.callback.emitProgress(100, "")
		f.callback.emitLog("✅ Flash complete!")
	}

	return nil
}

// changeBaudRateStub changes baud rate when stub is running
// Stub uses different protocol for baud rate change
func (f *ESP32Flasher) changeBaudRateStub(newBaud int) error {
	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("⚡ Switching to %d baud...", newBaud))
	}

	// Stub expects: [new_baud(4)][old_baud(4)]
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint32(payload[0:4], uint32(newBaud))
	binary.LittleEndian.PutUint32(payload[4:8], 115200) // current baud

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

// readFlashMD5 reads MD5 hash of flash region using stub's SPI_FLASH_MD5 command
func (f *ESP32Flasher) readFlashMD5(address, size uint32) (string, error) {
	// SPI_FLASH_MD5: [address(4)][size(4)][0(4)][0(4)]
	payload := make([]byte, 16)
	binary.LittleEndian.PutUint32(payload[0:4], address)
	binary.LittleEndian.PutUint32(payload[4:8], size)
	binary.LittleEndian.PutUint32(payload[8:12], 0)
	binary.LittleEndian.PutUint32(payload[12:16], 0)

	// MD5 calculation can take time for large files
	timeout := 30 * time.Second
	if size > 1024*1024 {
		timeout = 60 * time.Second
	}

	response, err := f.checkExecuteCommand(OpcodeSpiFlashMd5, payload, 0, timeout, 3)
	if err != nil {
		return "", err
	}

	// Stub returns MD5 in raw 16-byte format (not hex string)
	// Format: [direction(1)][opcode(1)][size(2)][value(4)][md5_raw(16)][status(2)]
	// Total: 26 bytes minimum
	if len(response) < 24 {
		return "", fmt.Errorf("MD5 response too short: %d bytes", len(response))
	}

	// MD5 is at offset 8, 16 raw bytes - convert to hex string
	md5Raw := response[8 : 8+16]
	md5Hex := hex.EncodeToString(md5Raw)
	return md5Hex, nil
}

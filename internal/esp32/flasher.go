package esp32

import (
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"go.bug.st/serial"
)

// New creates a flasher and automatically enters the ROM bootloader.
func New(portName string, callback *Callbacks) (*ESP32Flasher, error) {
	mode := &serial.Mode{
		BaudRate: 115200,
		Parity:   serial.NoParity,
		DataBits: 8,
		StopBits: serial.OneStopBit,
		InitialStatusBits: &serial.ModemOutputBits{
			DTR: false,
			RTS: false,
		},
	}

	// Open a second descriptor before serial.Open acquires exclusive access.
	// Unix needs it to set DTR and RTS atomically for CP2102 DevKit circuits.
	control, controlErr := openModemControl(portName)
	port, err := serial.Open(portName, mode)
	if err != nil {
		if control != nil {
			_ = control.Close()
		}
		return nil, fmt.Errorf("failed to open port: %w", err)
	}

	flasher := &ESP32Flasher{
		port:               port,
		modemControl:       control,
		refreshDTRAfterRTS: platformRequiresDTRRefreshAfterRTS,
		callback:           callback,
		chipType:           CHIP_UNKNOWN,
		blockSize:          ESP_FLASH_WRITE_SIZE,
		allowSpeedIncrease: true,
	}
	if controlErr != nil && callback != nil {
		callback.emitLog(fmt.Sprintf("⚠️ Atomic DTR/RTS control is unavailable: %v", controlErr))
	}

	if err := flasher.enterBootloader(); err != nil {
		_ = flasher.Close()
		return nil, fmt.Errorf("failed to enter bootloader: %w", err)
	}

	return flasher, nil
}

// NewManual creates a flasher for a board already placed in download mode.
func NewManual(portName string, callback *Callbacks) (*ESP32Flasher, error) {
	mode := &serial.Mode{
		BaudRate: 115200,
		Parity:   serial.NoParity,
		DataBits: 8,
		StopBits: serial.OneStopBit,
		InitialStatusBits: &serial.ModemOutputBits{
			DTR: false,
			RTS: false,
		},
	}

	port, err := serial.Open(portName, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to open port: %w", err)
	}

	flasher := &ESP32Flasher{
		port:               port,
		refreshDTRAfterRTS: platformRequiresDTRRefreshAfterRTS,
		callback:           callback,
		chipType:           CHIP_UNKNOWN,
		blockSize:          ESP_FLASH_WRITE_SIZE,
		allowSpeedIncrease: false,
	}

	if callback != nil {
		callback.emitLog("⚠️ MANUAL BOOTLOADER MODE")
		callback.emitLog("🔧 Make sure the ESP32 is in download mode:")
		callback.emitLog("   • Hold BOOT while pressing RESET")
		callback.emitLog("   • Or hold GPIO0 at GND during reset")
		callback.emitLog("   • The UART should report 'waiting for download'")
		callback.emitLog("")
	}

	if !flasher.testSync() {
		port.Close()
		return nil, fmt.Errorf("ESP32 is not in the ROM bootloader; enter download mode manually and retry")
	}

	if callback != nil {
		callback.emitLog("✅ ESP32 is in the ROM bootloader!")
	}

	return flasher, nil
}

// Close releases the serial port.
func (f *ESP32Flasher) Close() error {
	var controlErr error
	if f.modemControl != nil {
		controlErr = f.modemControl.Close()
		f.modemControl = nil
	}
	return errors.Join(controlErr, f.port.Close())
}

// enterBootloader resets the ESP32 into download mode.
func (f *ESP32Flasher) enterBootloader() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Entering ESP32 download mode...")
	}

	// The user may already have placed the board in download mode.
	if f.testSync() {
		if f.callback != nil {
			f.callback.emitLog("✅ ESP32 is already in the ROM bootloader")
		}
		return nil
	}

	type resetStrategy struct {
		name     string
		attempts int
		reset    func() error
	}
	resetStrategies := make([]resetStrategy, 0, 5)
	if f.modemControl != nil {
		resetStrategies = append(resetStrategies,
			resetStrategy{name: "tight DTR/RTS", attempts: 1, reset: f.tightReset},
			resetStrategy{name: "slow tight DTR/RTS", attempts: 1, reset: f.slowTightReset},
		)
	}
	resetStrategies = append(resetStrategies,
		resetStrategy{name: "DevKit auto-reset", attempts: 1, reset: f.hardReset},
		resetStrategy{name: "slow DevKit auto-reset", attempts: 1, reset: f.slowHardReset},
		resetStrategy{name: "direct DTR→GPIO0, RTS→EN", attempts: 3, reset: f.directReset},
	)

	for _, strategy := range resetStrategies {
		for attempt := 1; attempt <= strategy.attempts; attempt++ {
			if f.callback != nil {
				f.callback.emitLog(fmt.Sprintf("🔄 Automatic reset: %s, attempt %d/%d", strategy.name, attempt, strategy.attempts))
			}
			if err := strategy.reset(); err != nil {
				return fmt.Errorf("%s control line reset failed: %w", strategy.name, err)
			}
			f.logBootBanner()
			if err := f.sync(); err == nil {
				if f.callback != nil {
					f.callback.emitLog(fmt.Sprintf("✅ ESP32 responded from the ROM bootloader (%s)", strategy.name))
				}
				return nil
			}
		}
	}

	if f.callback != nil {
		f.callback.emitLog("❌ Automatic ROM bootloader entry failed")
	}

	return fmt.Errorf("failed to enter bootloader mode")
}

func (f *ESP32Flasher) logBootBanner() {
	if f.callback == nil {
		return
	}
	if err := f.port.SetReadTimeout(100 * time.Millisecond); err != nil {
		return
	}
	buffer := make([]byte, 1024)
	n, err := f.port.Read(buffer)
	if err == nil && n > 0 {
		f.callback.emitLog(fmt.Sprintf("🔍 ROM boot output: %q", buffer[:n]))
	}
}

// tightReset follows esptool's Unix-tight state transitions. Passing through
// the both-asserted state before holding EN low is required by some CP2102
// DevKit/WROOM reset circuits.
func (f *ESP32Flasher) tightReset() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Tight DTR/RTS reset sequence")
	}
	return f.tightResetWithSleepAndDelay(time.Sleep, SERIAL_FLASHER_BOOT_HOLD_TIME_MS*time.Millisecond)
}

func (f *ESP32Flasher) slowTightReset() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Slow tight DTR/RTS reset sequence")
	}
	return f.tightResetWithSleepAndDelay(time.Sleep, (SERIAL_FLASHER_BOOT_HOLD_TIME_MS+500)*time.Millisecond)
}

func (f *ESP32Flasher) tightResetWithSleepAndDelay(sleep func(time.Duration), bootDelay time.Duration) error {
	if err := f.setDTRAndRTS(false, false); err != nil {
		return fmt.Errorf("release DTR/RTS before tight reset: %w", err)
	}
	if err := f.setDTRAndRTS(true, true); err != nil {
		return fmt.Errorf("assert DTR/RTS for tight reset: %w", err)
	}
	if err := f.setDTRAndRTS(false, true); err != nil {
		return fmt.Errorf("hold reset with boot pin released: %w", err)
	}
	sleep(SERIAL_FLASHER_RESET_HOLD_TIME_MS * time.Millisecond)
	if err := f.setDTRAndRTS(true, false); err != nil {
		return fmt.Errorf("release reset in download mode: %w", err)
	}
	sleep(bootDelay)
	if err := f.setDTRAndRTS(false, false); err != nil {
		return fmt.Errorf("release DTR/RTS after tight reset: %w", err)
	}
	// Matches esptool's final DTR release, needed by some Unix drivers.
	if err := f.port.SetDTR(false); err != nil {
		return fmt.Errorf("ensure boot pin is released after tight reset: %w", err)
	}

	return nil
}

func (f *ESP32Flasher) setDTRAndRTS(dtr, rts bool) error {
	if f.modemControl != nil {
		return f.modemControl.Set(dtr, rts)
	}
	if err := f.port.SetDTR(dtr); err != nil {
		return err
	}
	return f.setRTS(rts, dtr)
}

// setRTS applies the Windows usbser.sys workaround used by esptool. Some USB
// serial drivers only transmit a changed RTS value when DTR is applied again.
func (f *ESP32Flasher) setRTS(rts, currentDTR bool) error {
	if err := f.port.SetRTS(rts); err != nil {
		return err
	}
	if f.refreshDTRAfterRTS {
		if err := f.port.SetDTR(currentDTR); err != nil {
			return fmt.Errorf("refresh DTR after RTS change: %w", err)
		}
	}
	return nil
}

// hardReset performs the standard Espressif DevKit reset sequence.
func (f *ESP32Flasher) hardReset() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Standard Espressif DTR/RTS sequence")
	}
	return f.classicResetWithDelay(time.Sleep, SERIAL_FLASHER_BOOT_HOLD_TIME_MS*time.Millisecond)
}

func (f *ESP32Flasher) classicReset(sleep func(time.Duration)) error {
	return f.classicResetWithDelay(sleep, SERIAL_FLASHER_BOOT_HOLD_TIME_MS*time.Millisecond)
}

func (f *ESP32Flasher) slowHardReset() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Slow Espressif DTR/RTS sequence")
	}
	return f.classicResetWithDelay(time.Sleep, (SERIAL_FLASHER_BOOT_HOLD_TIME_MS+500)*time.Millisecond)
}

func (f *ESP32Flasher) classicResetWithDelay(sleep func(time.Duration), bootDelay time.Duration) error {
	// DTR and RTS are active-low. On the standard DevKit transistor circuit,
	// asserting both lines intentionally does not hold the chip in reset.
	if err := f.port.SetDTR(false); err != nil { // GPIO0 = HIGH
		return fmt.Errorf("release DTR: %w", err)
	}
	if err := f.setRTS(true, false); err != nil { // EN = LOW
		return fmt.Errorf("assert RTS: %w", err)
	}
	sleep(SERIAL_FLASHER_RESET_HOLD_TIME_MS * time.Millisecond)
	if err := f.port.SetDTR(true); err != nil { // GPIO0 = LOW
		return fmt.Errorf("assert DTR: %w", err)
	}
	if err := f.setRTS(false, true); err != nil { // EN = HIGH
		return fmt.Errorf("release RTS: %w", err)
	}
	sleep(bootDelay)
	if err := f.port.SetDTR(false); err != nil { // GPIO0 = HIGH
		return fmt.Errorf("release DTR after reset: %w", err)
	}
	sleep(50 * time.Millisecond)

	return nil
}

// directReset supports USB-to-UART adapters wired without the DevKit circuit:
// DTR directly controls GPIO0 and RTS controls EN. Both lines are asserted
// during reset, then EN is released before GPIO0.
func (f *ESP32Flasher) directReset() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Direct DTR→GPIO0, RTS→EN sequence")
	}
	return f.directResetWithSleep(time.Sleep)
}

func (f *ESP32Flasher) directResetWithSleep(sleep func(time.Duration)) error {
	if err := f.port.SetDTR(false); err != nil { // GPIO0 = HIGH
		return fmt.Errorf("release DTR: %w", err)
	}
	if err := f.setRTS(false, false); err != nil { // EN = HIGH
		return fmt.Errorf("release RTS: %w", err)
	}
	sleep(50 * time.Millisecond)

	if err := f.port.SetDTR(true); err != nil { // GPIO0 = LOW
		return fmt.Errorf("assert DTR: %w", err)
	}
	sleep(50 * time.Millisecond)
	if err := f.setRTS(true, true); err != nil { // EN = LOW
		return fmt.Errorf("assert RTS: %w", err)
	}
	sleep(SERIAL_FLASHER_RESET_HOLD_TIME_MS * time.Millisecond)
	if err := f.setRTS(false, true); err != nil { // EN = HIGH
		return fmt.Errorf("release RTS: %w", err)
	}
	sleep(SERIAL_FLASHER_BOOT_HOLD_TIME_MS * time.Millisecond)
	if err := f.port.SetDTR(false); err != nil { // GPIO0 = HIGH
		return fmt.Errorf("release DTR after reset: %w", err)
	}
	sleep(50 * time.Millisecond)

	return nil
}

// hardResetInverted performs a reset for adapters with inverted control logic.
func (f *ESP32Flasher) hardResetInverted() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Inverted reset sequence...")
	}

	f.port.SetDTR(false) // GPIO0 = LOW (inverted).
	f.port.SetRTS(true)  // EN = HIGH (inverted).
	time.Sleep(10 * time.Millisecond)

	f.port.SetRTS(false) // EN = LOW (inverted).
	time.Sleep(SERIAL_FLASHER_RESET_HOLD_TIME_MS * time.Millisecond)

	f.port.SetRTS(true) // EN = HIGH (inverted).
	time.Sleep(SERIAL_FLASHER_BOOT_HOLD_TIME_MS * time.Millisecond)

	f.port.SetDTR(true) // GPIO0 = HIGH (inverted).
	time.Sleep(200 * time.Millisecond)

	return nil
}

// alternativeReset performs an alternate control-line sequence.
func (f *ESP32Flasher) alternativeReset() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Alternate reset sequence...")
	}

	f.port.SetDTR(false) // GPIO0 = HIGH
	f.port.SetRTS(false) // EN = HIGH
	time.Sleep(100 * time.Millisecond)

	f.port.SetDTR(true) // GPIO0 = LOW
	time.Sleep(100 * time.Millisecond)

	f.port.SetRTS(true) // EN = LOW
	time.Sleep(100 * time.Millisecond)

	f.port.SetRTS(false) // EN = HIGH
	time.Sleep(250 * time.Millisecond)

	f.port.SetDTR(false) // GPIO0 = HIGH
	time.Sleep(250 * time.Millisecond)

	return nil
}

// aggressiveReset performs a longer reset sequence for difficult boards.
func (f *ESP32Flasher) aggressiveReset() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Extended reset sequence...")
	}

	f.port.ResetInputBuffer()
	f.port.ResetOutputBuffer()

	f.port.SetDTR(true) // GPIO0 = LOW
	f.port.SetRTS(true) // EN = LOW
	time.Sleep(200 * time.Millisecond)

	f.port.SetRTS(false) // EN = HIGH
	time.Sleep(300 * time.Millisecond)

	f.port.SetDTR(false) // GPIO0 = HIGH
	time.Sleep(100 * time.Millisecond)

	f.port.SetDTR(true) // Hold GPIO0 low again.
	time.Sleep(50 * time.Millisecond)
	f.port.SetDTR(false) // GPIO0 = HIGH
	time.Sleep(200 * time.Millisecond)

	return nil
}

// flashBegin starts a ROM Flash write operation.
func (f *ESP32Flasher) flashBegin(size, offset uint32) error {
	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("📋 Starting Flash write: %d bytes at address 0x%x", size, offset))
	}

	numBlocks := (size + f.blockSize - 1) / f.blockSize
	eraseSize := ((size + ESP_FLASH_SECTOR - 1) / ESP_FLASH_SECTOR) * ESP_FLASH_SECTOR

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("🧮 Parameters: %d blocks of %d bytes, erase size %d bytes",
			numBlocks, f.blockSize, eraseSize))
	}

	data := make([]byte, 16)
	binary.LittleEndian.PutUint32(data[0:4], eraseSize)
	binary.LittleEndian.PutUint32(data[4:8], numBlocks)
	binary.LittleEndian.PutUint32(data[8:12], f.blockSize)
	binary.LittleEndian.PutUint32(data[12:16], offset)

	if err := f.sendCommand(ESP_FLASH_BEGIN, data, 0); err != nil {
		return fmt.Errorf("failed to send flash begin: %w", err)
	}

	eraseTimeout := 15 * time.Second
	if size >= 2*1024*1024 {
		eraseTimeout = 60 * time.Second
	}
	response, err := f.readResponseForCommand(ESP_FLASH_BEGIN, eraseTimeout)
	if err != nil {
		return fmt.Errorf("flash begin timeout: %w", err)
	}

	if len(response) < 8 || response[0] != 0x01 || response[1] != ESP_FLASH_BEGIN {
		return fmt.Errorf("invalid flash begin response")
	}

	if err := checkROMStatus(response); err != nil {
		return fmt.Errorf("flash begin failed: %w", err)
	}

	if f.callback != nil {
		f.callback.emitLog("✅ Flash erased and ready for writing")
	}

	return nil
}

// flashData sends one Flash block with bounded retries.
func (f *ESP32Flasher) flashData(data []byte, seq uint32) error {
	header := make([]byte, 16)
	binary.LittleEndian.PutUint32(header[0:4], uint32(len(data)))
	binary.LittleEndian.PutUint32(header[4:8], seq)
	binary.LittleEndian.PutUint32(header[8:12], 0)
	binary.LittleEndian.PutUint32(header[12:16], 0)

	payload := append(header, data...)
	checksum := calculateChecksum(data)

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if attempt > 1 {
			if f.callback != nil {
				f.callback.emitLog(fmt.Sprintf("⚠️ Retrying block %d: attempt %d/3 (%v)", seq+1, attempt, lastErr))
			}
			time.Sleep(100 * time.Millisecond)
			f.rxBuffer = nil
			if err := f.port.ResetInputBuffer(); err != nil {
				return fmt.Errorf("reset input before retrying flash block %d: %w", seq, err)
			}
		}

		if err := f.sendCommandFast(ESP_FLASH_DATA, payload, checksum); err != nil {
			lastErr = fmt.Errorf("send flash data: %w", err)
			continue
		}

		response, err := f.readResponseForCommand(ESP_FLASH_DATA, 5*time.Second)
		if err != nil {
			lastErr = fmt.Errorf("read flash data response: %w", err)
			continue
		}
		if len(response) < 8 || response[0] != 0x01 || response[1] != ESP_FLASH_DATA {
			lastErr = fmt.Errorf("invalid flash data response: %x", response)
			continue
		}
		if err := checkROMStatus(response); err != nil {
			lastErr = err
			continue
		}

		return nil
	}

	return fmt.Errorf("flash data failed at seq %d after 3 attempts: %w", seq, lastErr)
}

// flashBeginFast starts a Flash write using the current fast-path parameters.
func (f *ESP32Flasher) flashBeginFast(size, offset uint32) error {
	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("📋 Starting fast Flash write: %d bytes at address 0x%x", size, offset))
	}

	numBlocks := (size + f.blockSize - 1) / f.blockSize
	eraseSize := ((size + ESP_FLASH_SECTOR - 1) / ESP_FLASH_SECTOR) * ESP_FLASH_SECTOR

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("🧮 Fast-path parameters: %d blocks of %d bytes, erase size %d bytes",
			numBlocks, f.blockSize, eraseSize))
	}

	data := make([]byte, 16)
	binary.LittleEndian.PutUint32(data[0:4], eraseSize)
	binary.LittleEndian.PutUint32(data[4:8], numBlocks)
	binary.LittleEndian.PutUint32(data[8:12], f.blockSize)
	binary.LittleEndian.PutUint32(data[12:16], offset)

	if err := f.sendCommand(ESP_FLASH_BEGIN, data, 0); err != nil {
		return fmt.Errorf("failed to send flash begin: %w", err)
	}

	response, err := f.readResponseForCommand(ESP_FLASH_BEGIN, 20*time.Second)
	if err != nil {
		return fmt.Errorf("flash begin timeout: %w", err)
	}

	if len(response) < 8 || response[0] != 0x01 || response[1] != ESP_FLASH_BEGIN {
		return fmt.Errorf("invalid flash begin response")
	}

	if err := checkROMStatus(response); err != nil {
		return fmt.Errorf("flash begin failed: %w", err)
	}

	if f.callback != nil {
		f.callback.emitLog("✅ Flash erased and ready for high-speed writing")
	}

	return nil
}

// flashEnd completes the ROM Flash operation.
func (f *ESP32Flasher) flashEnd() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Finishing Flash operation...")
	}

	data := make([]byte, 4)
	// Keep the ROM loader running until RebootTarget performs one deterministic
	// hardware reset. A zero value would reboot here and race the RTS pulse.
	binary.LittleEndian.PutUint32(data, 1)

	if err := f.sendCommand(ESP_FLASH_END, data, 0); err != nil {
		return fmt.Errorf("failed to send flash end: %w", err)
	}

	response, err := f.readResponseForCommand(ESP_FLASH_END, 5*time.Second)
	if err != nil {
		return fmt.Errorf("flash end timeout: %w", err)
	}

	if len(response) < 8 || response[0] != 0x01 || response[1] != ESP_FLASH_END {
		return fmt.Errorf("invalid flash end response")
	}
	if err := checkROMStatus(response); err != nil {
		return fmt.Errorf("flash end failed: %w", err)
	}

	if f.callback != nil {
		f.callback.emitLog("✅ Flash operation completed successfully")
	}

	return nil
}

// Flash writes data at the requested Flash offset and verifies it before reboot.
func (f *ESP32Flasher) Flash(data []byte, offset uint32) error {
	// 1. Synchronize.
	if f.callback != nil {
		f.callback.emitProgress(10, "Synchronizing...")
	}
	if err := f.sync(); err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	// 2. Detect the chip.
	if f.callback != nil {
		f.callback.emitProgress(20, "Detecting chip...")
	}
	if err := f.detectChip(); err != nil {
		return fmt.Errorf("chip detection failed: %w", err)
	}
	if f.allowSpeedIncrease {
		if err := f.enableFastBaudRate(); err != nil {
			return fmt.Errorf("baud rate negotiation failed: %w", err)
		}
	}

	// 3. Attach and configure SPI Flash.
	if f.callback != nil {
		f.callback.emitProgress(30, "Attaching SPI Flash...")
	}
	if err := f.spiAttach(); err != nil {
		return fmt.Errorf("SPI attach failed: %w", err)
	}
	flashSize, err := flashSizeForImage(offset, uint32(len(data)))
	if err != nil {
		return err
	}
	if err := f.spiSetParameters(flashSize); err != nil {
		return fmt.Errorf("SPI parameters failed: %w", err)
	}

	// 4. Begin the Flash operation and erase the target range.
	if f.callback != nil {
		f.callback.emitProgress(35, "Erasing Flash...")
	}
	if err := f.flashBegin(uint32(len(data)), offset); err != nil {
		return fmt.Errorf("flash begin failed: %w", err)
	}

	// 5. Transfer data at the negotiated baud rate.
	totalBlocks := (len(data) + int(f.blockSize) - 1) / int(f.blockSize)

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("📤 Transferring data (%d blocks of %d bytes = %.1f MB)...",
			totalBlocks, f.blockSize, float64(len(data))/1024/1024))
		f.callback.emitProgress(45, "Transferring data...")
	}

	startTime := time.Now()

	for seq := uint32(0); seq < uint32(totalBlocks); seq++ {
		start := int(seq) * int(f.blockSize)
		end := start + int(f.blockSize)
		if end > len(data) {
			end = len(data)
		}

		block := make([]byte, f.blockSize)
		copy(block, data[start:end])

		// Pad the final block with erased bytes.
		for i := end - start; i < int(f.blockSize); i++ {
			block[i] = 0xFF
		}

		if err := f.flashData(block, seq); err != nil {
			return fmt.Errorf("flash data failed at block %d/%d: %w", seq+1, totalBlocks, err)
		}

		// Update progress without flooding the UI.
		if f.callback != nil {
			progress := 45 + int(float64(seq+1)/float64(totalBlocks)*45) // 45-90%
			percent := float64(seq+1) / float64(totalBlocks) * 100

			// Calculate effective payload throughput.
			elapsed := time.Since(startTime).Seconds()
			bytesWritten := float64((seq + 1) * uint32(f.blockSize))
			speed := bytesWritten / elapsed / 1024 // KiB/s.

			f.callback.emitProgress(progress, fmt.Sprintf("Writing %.1f%% (%.0f KiB/s)", percent, speed))

			// Log milestones only.
			if seq == 0 || (seq+1)%50 == 0 || seq == uint32(totalBlocks-1) {
				f.callback.emitLog(fmt.Sprintf("📦 Block %d/%d (%.1f%%, %.0f KiB/s)",
					seq+1, totalBlocks, percent, speed))
			}
		}
	}

	// 6. Verify before FLASH_END; the application may modify NVS immediately after reboot.
	if f.callback != nil {
		f.callback.emitProgress(93, "Verifying MD5...")
		f.callback.emitLog("🔍 Verifying written Flash before reboot...")
	}
	writtenMD5, err := f.flashMD5(offset, uint32(len(data)))
	if err != nil {
		return fmt.Errorf("flash verification failed: %w", err)
	}
	expectedMD5 := fmt.Sprintf("%x", md5.Sum(data))
	if writtenMD5 != expectedMD5 {
		return fmt.Errorf("flash verification failed: expected MD5 %s, got %s", expectedMD5, writtenMD5)
	}
	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("✅ MD5 verified: %s", writtenMD5))
	}

	// 7. Finish the ROM operation without rebooting yet.
	if f.callback != nil {
		f.callback.emitProgress(95, "Finishing...")
	}
	if err := f.flashEnd(); err != nil {
		return fmt.Errorf("flash end failed: %w", err)
	}

	if f.callback != nil {
		f.callback.emitProgress(100, "Done!")
		f.callback.emitLog("🎉 Firmware flashed successfully!")
	}

	return nil
}

func flashSizeForImage(offset, size uint32) (uint32, error) {
	required := uint64(offset) + uint64(size)
	for _, flashSize := range []uint32{
		4 * 1024 * 1024,
		8 * 1024 * 1024,
		16 * 1024 * 1024,
	} {
		if required <= uint64(flashSize) {
			return flashSize, nil
		}
	}
	return 0, fmt.Errorf("firmware range 0x%x..0x%x exceeds supported 16 MB flash", offset, required)
}

// SetBaudRate changes the ROM and host serial baud rate.
func (f *ESP32Flasher) SetBaudRate(baudRate int) error {
	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("🔄 Changing baud rate to %d bps...", baudRate))
	}

	data := make([]byte, 8)
	binary.LittleEndian.PutUint32(data[0:4], uint32(baudRate))
	binary.LittleEndian.PutUint32(data[4:8], 0)

	if err := f.sendCommand(0x0F, data, 0); err != nil {
		return fmt.Errorf("failed to send baudrate change command: %w", err)
	}

	response, err := f.readResponseForCommand(0x0F, time.Second)
	if err != nil {
		return fmt.Errorf("baudrate change timeout: %w", err)
	}

	if len(response) < 8 || response[0] != 0x01 {
		return fmt.Errorf("invalid baudrate change response")
	}

	if err := f.setHostBaudRate(baudRate); err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("✅ Baud rate changed to %d bps", baudRate))
	}

	return nil
}

func (f *ESP32Flasher) enableFastBaudRate() error {
	for _, baudRate := range []int{921600, 460800} {
		if err := f.SetBaudRate(baudRate); err == nil && f.testSync() {
			if f.callback != nil {
				f.callback.emitLog(fmt.Sprintf("✅ High-speed connection verified at %d bps", baudRate))
			}
			return nil
		} else if f.callback != nil {
			f.callback.emitLog(fmt.Sprintf("⚠️ Could not verify %d bps; restoring 115200", baudRate))
		}

		if err := f.setHostBaudRate(115200); err != nil {
			return fmt.Errorf("restore host baud rate: %w", err)
		}
		f.rxBuffer = nil
		if err := f.enterBootloader(); err != nil {
			return fmt.Errorf("restore ROM bootloader after failed %d bps negotiation: %w", baudRate, err)
		}
	}

	if f.callback != nil {
		f.callback.emitLog("⚠️ High-speed mode is unavailable; continuing at 115200 bps")
	}
	return nil
}

func (f *ESP32Flasher) setHostBaudRate(baudRate int) error {
	mode := &serial.Mode{
		BaudRate: baudRate,
		Parity:   serial.NoParity,
		DataBits: 8,
		StopBits: serial.OneStopBit,
	}
	if err := f.port.SetMode(mode); err != nil {
		return fmt.Errorf("set host baud rate to %d: %w", baudRate, err)
	}
	return nil
}

// RebootTarget resets the ESP32 into normal execution mode.
func (f *ESP32Flasher) RebootTarget() error {
	return f.rebootTarget(time.Sleep)
}

func (f *ESP32Flasher) rebootTarget(sleep func(time.Duration)) error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Rebooting ESP32 into the flashed application...")
	}

	// Flashing may have negotiated a high UART rate. Restore the application
	// monitor rate before reset so boot output can be read immediately.
	if err := f.setHostBaudRate(115200); err != nil {
		return fmt.Errorf("restore application baud rate: %w", err)
	}
	if err := f.normalBootReset(sleep); err != nil {
		return err
	}

	if f.callback != nil {
		f.callback.emitLog("✅ ESP32 reset released; application startup requested")
	}

	return nil
}

func (f *ESP32Flasher) normalBootReset(sleep func(time.Duration)) error {
	// Start and finish with GPIO0 and EN released. The initial neutral state
	// prevents a leftover bootloader-entry signal from affecting the reset.
	if err := f.port.SetDTR(false); err != nil { // GPIO0 = HIGH (normal mode)
		return fmt.Errorf("release boot pin: %w", err)
	}
	if err := f.setRTS(false, false); err != nil { // EN = HIGH (neutral)
		return fmt.Errorf("release reset before reboot: %w", err)
	}
	sleep(50 * time.Millisecond)
	if err := f.setRTS(true, false); err != nil { // EN = LOW (reset)
		return fmt.Errorf("assert reset: %w", err)
	}
	sleep(SERIAL_FLASHER_RESET_HOLD_TIME_MS * time.Millisecond)
	if err := f.setRTS(false, false); err != nil { // EN = HIGH (release reset)
		return fmt.Errorf("release reset: %w", err)
	}

	// Keep the port open while the ROM and application initialize. Closing a
	// high-speed flashing session during early UART output can leave some USB
	// serial adapters continuously replaying the final received fragment when
	// the monitor reopens the port.
	sleep(SERIAL_APPLICATION_STARTUP_WAIT)
	if err := f.port.SetDTR(false); err != nil {
		return fmt.Errorf("keep boot pin released: %w", err)
	}
	if err := f.setRTS(false, false); err != nil {
		return fmt.Errorf("keep reset released: %w", err)
	}

	return nil
}

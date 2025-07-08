package main

import (
	"encoding/binary"
	"fmt"
	"time"

	"go.bug.st/serial"
)

// NewESP32FlasherWithProgress создает флешер с автоматическим входом в bootloader
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
		port:      port,
		portName:  portName,
		callback:  callback,
		chipType:  CHIP_UNKNOWN,
		blockSize: ESP_FLASH_WRITE_SIZE,
	}

	if err := flasher.enterBootloader(); err != nil {
		port.Close()
		return nil, fmt.Errorf("failed to enter bootloader: %w", err)
	}

	return flasher, nil
}

// NewESP32FlasherManual создает флешер для ручного режима
func NewESP32FlasherManual(portName string, callback ProgressCallback) (*ESP32Flasher, error) {
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
		port:      port,
		portName:  portName,
		callback:  callback,
		chipType:  CHIP_UNKNOWN,
		blockSize: ESP_FLASH_WRITE_SIZE,
	}

	if callback != nil {
		callback.emitLog("⚠️ РУЧНОЙ РЕЖИМ АКТИВИРОВАН")
		callback.emitLog("🔧 Убедитесь, что ESP32 в режиме bootloader:")
		callback.emitLog("   • Удерживайте BOOT при нажатии RESET")
		callback.emitLog("   • Или замкните GPIO0 на GND при сбросе")
		callback.emitLog("   • Должно быть сообщение 'waiting for download'")
		callback.emitLog("")
	}

	if !flasher.testSync() {
		port.Close()
		return nil, fmt.Errorf("ESP32 не в режиме bootloader. Переведите вручную и попробуйте снова")
	}

	if callback != nil {
		callback.emitLog("✅ ESP32 в режиме bootloader!")
	}

	return flasher, nil
}

// Close закрывает соединение
func (f *ESP32Flasher) Close() error {
	return f.port.Close()
}

// enterBootloader переводит ESP32 в режим загрузки
func (f *ESP32Flasher) enterBootloader() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Перевод ESP32 в режим загрузки...")
	}

	f.port.ResetInputBuffer()
	f.port.ResetOutputBuffer()
	time.Sleep(50 * time.Millisecond)

	// Метод 1: Стандартная логика
	if f.callback != nil {
		f.callback.emitLog("🔄 Метод 1: Стандартная логика DTR/RTS...")
	}
	if err := f.hardReset(); err == nil {
		if f.isInBootloader() {
			if f.callback != nil {
				f.callback.emitLog("✅ ESP32 в режиме bootloader (стандартная логика)")
			}
			return nil
		}
	}

	// Метод 2: Инвертированная логика
	if f.callback != nil {
		f.callback.emitLog("🔄 Метод 2: Инвертированная логика DTR/RTS...")
	}
	if err := f.hardResetInverted(); err == nil {
		if f.isInBootloader() {
			if f.callback != nil {
				f.callback.emitLog("✅ ESP32 в режиме bootloader (инвертированная логика)")
			}
			return nil
		}
	}

	// Метод 3: Альтернативная последовательность
	if f.callback != nil {
		f.callback.emitLog("🔄 Метод 3: Альтернативная последовательность...")
	}
	if err := f.alternativeReset(); err == nil {
		if f.isInBootloader() {
			if f.callback != nil {
				f.callback.emitLog("✅ ESP32 в режиме bootloader (альтернативная логика)")
			}
			return nil
		}
	}

	// Метод 4: Множественные попытки
	if f.callback != nil {
		f.callback.emitLog("🔄 Метод 4: Множественные попытки...")
	}
	for attempt := 0; attempt < 3; attempt++ {
		if f.callback != nil {
			f.callback.emitLog(fmt.Sprintf("   Попытка %d/3...", attempt+1))
		}

		if err := f.aggressiveReset(); err == nil {
			if f.isInBootloader() {
				if f.callback != nil {
					f.callback.emitLog("✅ ESP32 в режиме bootloader (агрессивный метод)")
				}
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Финальная проверка
	if f.callback != nil {
		f.callback.emitLog("🔍 Финальная проверка...")
	}
	if f.testSync() {
		if f.callback != nil {
			f.callback.emitLog("✅ ESP32 уже в режиме bootloader!")
		}
		return nil
	}

	// Инструкции для ручного режима
	if f.callback != nil {
		f.callback.emitLog("❌ Автоматический вход в bootloader не удался")
		f.callback.emitLog("")
		f.callback.emitLog("🔧 ПОПРОБУЙТЕ РУЧНОЙ РЕЖИМ:")
		f.callback.emitLog("   1. Отключите кабель USB от ESP32")
		f.callback.emitLog("   2. Удерживайте кнопку BOOT (GPIO0)")
		f.callback.emitLog("   3. Подключите кабель USB (не отпуская BOOT)")
		f.callback.emitLog("   4. Отпустите кнопку BOOT")
		f.callback.emitLog("   5. Запустите прошивку снова")
		f.callback.emitLog("")
		f.callback.emitLog("🔧 ИЛИ:")
		f.callback.emitLog("   1. Удерживайте BOOT")
		f.callback.emitLog("   2. Нажмите и отпустите RESET")
		f.callback.emitLog("   3. Отпустите BOOT")
		f.callback.emitLog("")
		f.callback.emitLog("💡 Используйте NewESP32FlasherManual() для ручного режима")
	}

	return fmt.Errorf("failed to enter bootloader mode")
}

// hardReset стандартная последовательность сброса
func (f *ESP32Flasher) hardReset() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Стандартный сброс...")
	}

	f.port.SetDTR(true)  // GPIO0 = LOW
	f.port.SetRTS(false) // EN = HIGH
	time.Sleep(10 * time.Millisecond)

	f.port.SetRTS(true) // EN = LOW (reset)
	time.Sleep(SERIAL_FLASHER_RESET_HOLD_TIME_MS * time.Millisecond)

	f.port.SetRTS(false) // EN = HIGH
	time.Sleep(SERIAL_FLASHER_BOOT_HOLD_TIME_MS * time.Millisecond)

	f.port.SetDTR(false) // GPIO0 = HIGH
	time.Sleep(200 * time.Millisecond)

	return nil
}

// hardResetInverted инвертированная логика сброса
func (f *ESP32Flasher) hardResetInverted() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Инвертированный сброс...")
	}

	f.port.SetDTR(false) // GPIO0 = LOW (инвертированная)
	f.port.SetRTS(true)  // EN = HIGH (инвертированная)
	time.Sleep(10 * time.Millisecond)

	f.port.SetRTS(false) // EN = LOW (инвертированная)
	time.Sleep(SERIAL_FLASHER_RESET_HOLD_TIME_MS * time.Millisecond)

	f.port.SetRTS(true) // EN = HIGH (инвертированная)
	time.Sleep(SERIAL_FLASHER_BOOT_HOLD_TIME_MS * time.Millisecond)

	f.port.SetDTR(true) // GPIO0 = HIGH (инвертированная)
	time.Sleep(200 * time.Millisecond)

	return nil
}

// alternativeReset альтернативная последовательность
func (f *ESP32Flasher) alternativeReset() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Альтернативный сброс...")
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

// aggressiveReset агрессивная попытка сброса
func (f *ESP32Flasher) aggressiveReset() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Агрессивный сброс...")
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

	f.port.SetDTR(true) // GPIO0 = LOW снова
	time.Sleep(50 * time.Millisecond)
	f.port.SetDTR(false) // GPIO0 = HIGH
	time.Sleep(200 * time.Millisecond)

	return nil
}

// flashData отправляет блок данных для прошивки с улучшенной обработкой
func (f *ESP32Flasher) flashData(data []byte, seq uint32) error {
	header := make([]byte, 16)
	binary.LittleEndian.PutUint32(header[0:4], uint32(len(data)))
	binary.LittleEndian.PutUint32(header[4:8], seq)
	binary.LittleEndian.PutUint32(header[8:12], 0)
	binary.LittleEndian.PutUint32(header[12:16], 0)

	payload := append(header, data...)
	checksum := calculateChecksum(data)

	for attempt := 0; attempt < 3; attempt++ {
		if err := f.sendCommand(ESP_FLASH_DATA, payload, checksum); err != nil {
			if attempt == 2 {
				return fmt.Errorf("failed to send flash data after 3 attempts: %w", err)
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}

		response, err := f.readResponseRobust(5 * time.Second)
		if err != nil {
			if attempt == 2 {
				return fmt.Errorf("flash data timeout at seq %d: %w", seq, err)
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if len(response) < 8 || response[0] != 0x01 || response[1] != ESP_FLASH_DATA {
			if attempt == 2 {
				return fmt.Errorf("invalid flash data response at seq %d", seq)
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if len(response) >= 12 {
			status := response[len(response)-4]
			if status != 0x00 {
				return fmt.Errorf("flash data failed at seq %d with status: %d", seq, status)
			}
		}

		return nil
	}

	return fmt.Errorf("flash data failed after 3 attempts at seq %d", seq)
}

// flashBegin начинает процесс прошивки с улучшенной обработкой
func (f *ESP32Flasher) flashBegin(size, offset uint32) error {
	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("📋 Начало прошивки: размер %d байт, адрес 0x%x", size, offset))
	}

	numBlocks := (size + f.blockSize - 1) / f.blockSize
	eraseSize := ((size + ESP_FLASH_SECTOR - 1) / ESP_FLASH_SECTOR) * ESP_FLASH_SECTOR

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("🧮 Параметры: %d блоков по %d байт, стирание %d байт",
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

	response, err := f.readResponseRobust(20 * time.Second) // Увеличенный таймаут для стирания
	if err != nil {
		return fmt.Errorf("flash begin timeout: %w", err)
	}

	if len(response) < 8 || response[0] != 0x01 || response[1] != ESP_FLASH_BEGIN {
		return fmt.Errorf("invalid flash begin response")
	}

	if len(response) >= 12 {
		status := response[len(response)-4]
		if status != 0x00 {
			return fmt.Errorf("flash begin failed with status: %d", status)
		}
	}

	if f.callback != nil {
		f.callback.emitLog("✅ Flash стерт и готов к записи")
	}

	return nil
}

// flashEnd завершает процесс прошивки с улучшенной обработкой
func (f *ESP32Flasher) flashEnd() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Завершение прошивки...")
	}

	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, 0) // Reboot = false

	if err := f.sendCommand(ESP_FLASH_END, data, 0); err != nil {
		return fmt.Errorf("failed to send flash end: %w", err)
	}

	response, err := f.readResponseRobust(5 * time.Second)
	if err != nil {
		return fmt.Errorf("flash end timeout: %w", err)
	}

	if len(response) < 8 || response[0] != 0x01 || response[1] != ESP_FLASH_END {
		return fmt.Errorf("invalid flash end response")
	}

	if f.callback != nil {
		f.callback.emitLog("✅ Прошивка завершена успешно")
	}

	return nil
}

// FlashData основная функция прошивки
func (f *ESP32Flasher) FlashData(data []byte, offset uint32, portName string) error {
	// 1. Синхронизация
	if f.callback != nil {
		f.callback.emitProgress(10, "Синхронизация...")
	}
	if err := f.sync(); err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	// 2. Определение чипа
	if f.callback != nil {
		f.callback.emitProgress(20, "Определение чипа...")
	}
	if err := f.detectChip(); err != nil {
		return fmt.Errorf("chip detection failed: %w", err)
	}

	// 3. Подключение SPI
	if f.callback != nil {
		f.callback.emitProgress(30, "Подключение SPI...")
	}
	if err := f.spiAttach(); err != nil {
		return fmt.Errorf("SPI attach failed: %w", err)
	}

	// 4. Начало прошивки
	if f.callback != nil {
		f.callback.emitProgress(40, "Стирание Flash...")
	}
	if err := f.flashBegin(uint32(len(data)), offset); err != nil {
		return fmt.Errorf("flash begin failed: %w", err)
	}

	// 5. Отправка данных
	totalBlocks := (len(data) + int(f.blockSize) - 1) / int(f.blockSize)

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("📤 Передача данных (%d блоков по %d байт)...", totalBlocks, f.blockSize))
		f.callback.emitProgress(50, "Передача данных...")
	}

	for seq := uint32(0); seq < uint32(totalBlocks); seq++ {
		start := int(seq) * int(f.blockSize)
		end := start + int(f.blockSize)
		if end > len(data) {
			end = len(data)
		}

		block := make([]byte, f.blockSize)
		copy(block, data[start:end])

		// Заполняем оставшееся место 0xFF
		for i := end - start; i < int(f.blockSize); i++ {
			block[i] = 0xFF
		}

		if err := f.flashData(block, seq); err != nil {
			return fmt.Errorf("flash data failed at block %d/%d: %w", seq+1, totalBlocks, err)
		}

		// Обновляем прогресс
		if f.callback != nil {
			progress := 50 + int(float64(seq+1)/float64(totalBlocks)*40) // 50-90%
			percent := float64(seq+1) / float64(totalBlocks) * 100
			f.callback.emitProgress(progress, fmt.Sprintf("Запись %.1f%% (%d/%d блоков)", percent, seq+1, totalBlocks))

			if (seq+1)%10 == 0 || seq == uint32(totalBlocks-1) {
				f.callback.emitLog(fmt.Sprintf("📦 Записан блок %d/%d (%.1f%%, %d байт)", seq+1, totalBlocks, percent, end-start))
			}
		}
	}

	// 6. Завершение
	if f.callback != nil {
		f.callback.emitProgress(95, "Завершение...")
	}
	if err := f.flashEnd(); err != nil {
		return fmt.Errorf("flash end failed: %w", err)
	}

	if f.callback != nil {
		f.callback.emitProgress(100, "Готово!")
		f.callback.emitLog("🎉 Прошивка завершена успешно!")
	}

	return nil
}

// SetBaudRate изменяет скорость передачи
func (f *ESP32Flasher) SetBaudRate(baudRate int) error {
	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("🔄 Изменение скорости на %d bps...", baudRate))
	}

	data := make([]byte, 8)
	binary.LittleEndian.PutUint32(data[0:4], uint32(baudRate))
	binary.LittleEndian.PutUint32(data[4:8], 0)

	if err := f.sendCommand(0x0F, data, 0); err != nil {
		return fmt.Errorf("failed to send baudrate change command: %w", err)
	}

	response, err := f.readResponse(1 * time.Second)
	if err != nil {
		return fmt.Errorf("baudrate change timeout: %w", err)
	}

	if len(response) < 8 || response[0] != 0x01 {
		return fmt.Errorf("invalid baudrate change response")
	}

	f.port.Close()

	mode := &serial.Mode{
		BaudRate: baudRate,
		Parity:   serial.NoParity,
		DataBits: 8,
		StopBits: serial.OneStopBit,
	}

	newPort, err := serial.Open(f.portName, mode)
	if err != nil {
		return fmt.Errorf("failed to reopen port with new baudrate: %w", err)
	}

	f.port = newPort
	time.Sleep(100 * time.Millisecond)

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("✅ Скорость изменена на %d bps", baudRate))
	}

	return nil
}

// RebootTarget перезагружает ESP32
func (f *ESP32Flasher) RebootTarget() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Перезагрузка ESP32...")
	}

	f.port.SetDTR(false) // GPIO0 = HIGH (normal mode)
	f.port.SetRTS(true)  // EN = LOW (reset)
	time.Sleep(100 * time.Millisecond)
	f.port.SetRTS(false) // EN = HIGH (release reset)

	if f.callback != nil {
		f.callback.emitLog("✅ ESP32 перезагружен")
	}

	return nil
}

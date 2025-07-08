package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"go.bug.st/serial"
)

// ESP32 протокол команд
const (
	ESP_FLASH_BEGIN = 0x02
	ESP_FLASH_DATA  = 0x03
	ESP_FLASH_END   = 0x04
	ESP_MEM_BEGIN   = 0x05
	ESP_MEM_END     = 0x06
	ESP_MEM_DATA    = 0x07
	ESP_SYNC        = 0x08
	ESP_WRITE_REG   = 0x09
	ESP_READ_REG    = 0x0a
	ESP_SPI_ATTACH  = 0x0d

	// SLIP протокол
	SLIP_END     = 0xc0
	SLIP_ESC     = 0xdb
	SLIP_ESC_END = 0xdc
	SLIP_ESC_ESC = 0xdd

	// Размеры
	ESP_FLASH_SECTOR = 4096
	ESP_FLASH_BLOCK  = 65536

	// Константы тайминга сброса из официального esp-serial-flasher
	// https://github.com/espressif/esp-serial-flasher
	SERIAL_FLASHER_RESET_HOLD_TIME_MS = 100 // время удержания RESET в миллисекундах
	SERIAL_FLASHER_BOOT_HOLD_TIME_MS  = 50  // время удержания BOOT (GPIO0) в миллисекундах
)

// ProgressCallback интерфейс для коллбеков прогресса
type ProgressCallback interface {
	emitProgress(progress int, message string)
	emitLog(message string)
}

// ESP32Flasher - структура для работы с ESP32
type ESP32Flasher struct {
	port     serial.Port
	portName string
	callback ProgressCallback
}

// NewESP32Flasher создает новый экземпляр флешера
func NewESP32Flasher(portName string) (*ESP32Flasher, error) {
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

	return &ESP32Flasher{
		port:     port,
		portName: portName,
	}, nil
}

// NewESP32FlasherWithProgress создает новый экземпляр флешера с коллбеками прогресса
func NewESP32FlasherWithProgress(portName string, callback ProgressCallback) (*ESP32Flasher, error) {
	// Начинаем с низкой скорости для надежной синхронизации
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

	// Пытаемся перевести ESP32 в режим загрузки
	if err := flasher.enterBootloader(); err != nil {
		if callback != nil {
			callback.emitLog("⚠️ Не удалось автоматически перевести ESP32 в режим загрузки")
			callback.emitLog("Убедитесь, что ESP32 находится в режиме загрузки (boot mode)")
		}
		return nil, fmt.Errorf("ESP32 не в режиме bootloader: %w", err)
	}

	return flasher, nil
}

// enterBootloader переводит ESP32 в режим загрузки, используя эталонную реализацию Espressif
func (f *ESP32Flasher) enterBootloader() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Перевод ESP32 в режим загрузки...")
		f.callback.emitLog("📘 Используется эталонная реализация esp-serial-flasher v0.3.0")
	}

	// Очищаем буферы перед началом
	f.port.ResetInputBuffer()
	f.port.ResetOutputBuffer()
	time.Sleep(50 * time.Millisecond)

	// Выполняем эталонную последовательность USB-UART конвертера
	if err := f.usbSerialConverterEnterBootloader(); err == nil {
		// Проверяем режим bootloader
		if f.testBootloaderMode() {
			if f.callback != nil {
				f.callback.emitLog("✅ ESP32 успешно переведен в режим bootloader")
			}
			return nil
		}
	}

	// Если не удалось, возможно нужна инвертированная логика
	if f.callback != nil {
		f.callback.emitLog("⚠️ Стандартная логика не сработала, пробуем инвертированную...")
	}

	if err := f.usbSerialConverterEnterBootloaderInverted(); err == nil {
		if f.testBootloaderMode() {
			if f.callback != nil {
				f.callback.emitLog("✅ ESP32 успешно переведен в режим bootloader (инвертированная логика)")
			}
			return nil
		}
	}

	// Если оба варианта не сработали
	if f.callback != nil {
		f.callback.emitLog("❌ Автоматический перевод в bootloader не удался")
		f.callback.emitLog("📋 Возможные причины:")
		f.callback.emitLog("   • GPIO0 не подключен к DTR")
		f.callback.emitLog("   • EN (RESET) не подключен к RTS")
		f.callback.emitLog("   • Сильные pull-up резисторы на GPIO0")
		f.callback.emitLog("   • Неподдерживаемый USB-UART чип")
		f.callback.emitLog("💡 Попробуйте РУЧНОЙ режим:")
		f.callback.emitLog("   1. Удерживайте кнопку BOOT (GPIO0)")
		f.callback.emitLog("   2. Нажмите и отпустите кнопку RESET (EN)")
		f.callback.emitLog("   3. Отпустите кнопку BOOT")
		f.callback.emitLog("   4. Запустите прошивку снова")
	}

	return fmt.Errorf("failed to enter bootloader mode")
}

// Close закрывает соединение
func (f *ESP32Flasher) Close() error {
	return f.port.Close()
}

// slipEncode кодирует данные в SLIP протокол
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

// slipDecode декодирует SLIP пакет
func slipDecode(data []byte) ([]byte, error) {
	if len(data) < 2 || data[0] != SLIP_END || data[len(data)-1] != SLIP_END {
		return nil, fmt.Errorf("invalid SLIP packet")
	}

	var buf bytes.Buffer
	escaped := false

	for i := 1; i < len(data)-1; i++ {
		b := data[i]
		if escaped {
			switch b {
			case SLIP_ESC_END:
				buf.WriteByte(SLIP_END)
			case SLIP_ESC_ESC:
				buf.WriteByte(SLIP_ESC)
			default:
				return nil, fmt.Errorf("invalid escape sequence")
			}
			escaped = false
		} else if b == SLIP_ESC {
			escaped = true
		} else {
			buf.WriteByte(b)
		}
	}

	return buf.Bytes(), nil
}

// sendCommand отправляет команду в ESP32
func (f *ESP32Flasher) sendCommand(cmd byte, data []byte, checksum uint32) error {
	// Создаем пакет команды
	packet := make([]byte, 8+len(data))
	packet[0] = 0x00                                              // Direction (request)
	packet[1] = cmd                                               // Command
	binary.LittleEndian.PutUint16(packet[2:4], uint16(len(data))) // Size
	binary.LittleEndian.PutUint32(packet[4:8], checksum)          // Checksum
	copy(packet[8:], data)                                        // Data

	// Кодируем в SLIP и отправляем
	encoded := slipEncode(packet)
	_, err := f.port.Write(encoded)
	return err
}

// readResponse читает ответ от ESP32 с улучшенной обработкой
func (f *ESP32Flasher) readResponse(timeout time.Duration) ([]byte, error) {
	f.port.SetReadTimeout(timeout)

	var allData bytes.Buffer
	buffer := make([]byte, 1024)

	start := time.Now()

	// Сначала читаем все доступные данные
	for time.Since(start) < timeout {
		n, err := f.port.Read(buffer)
		if err != nil && n == 0 {
			if allData.Len() > 0 {
				break // Если уже что-то прочитали, прекращаем ждать
			}
			time.Sleep(1 * time.Millisecond)
			continue
		}

		if n > 0 {
			allData.Write(buffer[:n])
			// Даем немного времени на получение остальных данных
			time.Sleep(10 * time.Millisecond)
		}
	}

	if allData.Len() == 0 {
		return nil, fmt.Errorf("timeout reading response after %v", timeout)
	}

	rawData := allData.Bytes()

	// Логируем сырые данные для отладки
	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("🔍 Сырые данные (%d байт): %x", len(rawData), rawData))
	}

	// Ищем SLIP пакет в данных
	for i := 0; i < len(rawData); i++ {
		if rawData[i] == SLIP_END {
			// Нашли начало пакета, ищем конец
			for j := i + 1; j < len(rawData); j++ {
				if rawData[j] == SLIP_END {
					// Нашли конец пакета
					slipPacket := rawData[i : j+1]
					if f.callback != nil {
						f.callback.emitLog(fmt.Sprintf("🔍 Найден SLIP пакет (%d байт): %x", len(slipPacket), slipPacket))
					}

					decoded, err := slipDecode(slipPacket)
					if err != nil {
						if f.callback != nil {
							f.callback.emitLog(fmt.Sprintf("⚠️ Ошибка декодирования SLIP: %v", err))
						}
						continue
					}

					if f.callback != nil {
						f.callback.emitLog(fmt.Sprintf("✅ Декодированный пакет (%d байт): %x", len(decoded), decoded))
					}

					return decoded, nil
				}
			}
		}
	}

	// Если SLIP пакет не найден, возвращаем сырые данные
	if f.callback != nil {
		f.callback.emitLog("⚠️ SLIP пакет не найден, возвращаем сырые данные")
	}

	return rawData, nil
}

// sync синхронизируется с ESP32 точно как esptool.py
// sync синхронизируется с ESP32 (упрощенная версия без повторных попыток reset)
func (f *ESP32Flasher) sync() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Синхронизация с ESP32...")
	}

	// Sync команда точно как в esptool.py: 0x07 0x07 0x12 0x20 + 32 байта 0x55
	syncData := make([]byte, 36)
	syncData[0] = 0x07
	syncData[1] = 0x07
	syncData[2] = 0x12
	syncData[3] = 0x20
	for i := 4; i < 36; i++ {
		syncData[i] = 0x55
	}

	// Очищаем буферы
	f.port.ResetInputBuffer()
	f.port.ResetOutputBuffer()
	time.Sleep(100 * time.Millisecond)

	// Отправляем SYNC команду
	if err := f.sendCommand(ESP_SYNC, syncData, 0); err != nil {
		return fmt.Errorf("failed to send sync command: %w", err)
	}

	if f.callback != nil {
		f.callback.emitLog("📤 SYNC команда отправлена, ожидание ответа...")
	}

	// Читаем ответ
	response, err := f.readResponse(3 * time.Second)
	if err != nil {
		return fmt.Errorf("timeout reading sync response: %w", err)
	}

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("📥 Получен ответ длиной %d байт: %x", len(response), response))
	}

	// Проверяем правильность ответа на SYNC
	if len(response) < 8 {
		return fmt.Errorf("sync response too short: %d bytes", len(response))
	}

	// Проверяем заголовок: direction=0x01, command=ESP_SYNC
	if response[0] != 0x01 || response[1] != ESP_SYNC {
		return fmt.Errorf("invalid sync response header: dir=0x%02x, cmd=0x%02x", response[0], response[1])
	}

	// ESP32 ROM loader использует 4 байта для статуса (в отличие от stub loader с 2 байтами)
	if len(response) >= 12 {
		status := response[len(response)-4] // Статус находится в 4-м байте с конца
		if status != 0x00 {
			errorCode := response[len(response)-3] // Ошибка в 3-м байте с конца
			return fmt.Errorf("sync failed: status=%d, error=%d", status, errorCode)
		}
	}

	if f.callback != nil {
		f.callback.emitLog("✅ Синхронизация успешна!")
	}

	// После успешной SYNC команды, отправляем ещё 7 пустых команд как в esptool.py
	if f.callback != nil {
		f.callback.emitLog("🔄 Отправка дополнительных команд для очистки...")
	}

	for i := 0; i < 7; i++ {
		f.sendCommand(ESP_SYNC, []byte{}, 0)
		time.Sleep(10 * time.Millisecond)
		// Читаем и игнорируем ответы
		f.readResponse(100 * time.Millisecond)
	}

	return nil
}

// spiAttach подключает SPI flash
func (f *ESP32Flasher) spiAttach() error {
	if f.callback != nil {
		f.callback.emitLog("🔗 Подключение к SPI Flash...")
	}

	// Для ESP32 ROM loader нужно 8 байт: первое слово = 0 (default SPI), второе слово = 0
	data := make([]byte, 8)
	binary.LittleEndian.PutUint32(data[0:4], 0) // Default SPI interface
	binary.LittleEndian.PutUint32(data[4:8], 0) // Reserved, должно быть 0

	if f.callback != nil {
		f.callback.emitLog("📤 Отправка команды SPI_ATTACH...")
	}

	if err := f.sendCommand(ESP_SPI_ATTACH, data, 0); err != nil {
		return fmt.Errorf("failed to send SPI attach command: %w", err)
	}

	if f.callback != nil {
		f.callback.emitLog("⏳ Ожидание ответа на SPI_ATTACH...")
	}

	response, err := f.readResponse(3 * time.Second)
	if err != nil {
		return fmt.Errorf("timeout waiting for SPI attach response: %w", err)
	}

	if len(response) < 8 || response[0] != 0x01 || response[1] != ESP_SPI_ATTACH {
		return fmt.Errorf("invalid SPI attach response (len=%d, dir=0x%02x, cmd=0x%02x)", len(response), response[0], response[1])
	}

	// Проверяем статус (ESP32 ROM loader использует 4 байта)
	if len(response) >= 12 {
		status := response[len(response)-4]
		if status != 0x00 {
			errorCode := response[len(response)-3]
			return fmt.Errorf("SPI attach failed: status=%d, error=%d", status, errorCode)
		}
	}

	if f.callback != nil {
		f.callback.emitLog("✅ SPI Flash подключен успешно")
	}

	return nil
}

// calculateChecksum вычисляет контрольную сумму для данных
func calculateChecksum(data []byte) uint32 {
	checksum := uint32(0xEF)
	for _, b := range data {
		checksum ^= uint32(b)
	}
	return checksum & 0xFF
}

// flashBegin начинает процесс прошивки
func (f *ESP32Flasher) flashBegin(size, offset uint32) error {
	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("📋 Начало прошивки: размер %d байт, адрес 0x%x", size, offset))
	}

	// Рассчитываем количество секторов для стирания
	sectors := (size + ESP_FLASH_SECTOR - 1) / ESP_FLASH_SECTOR
	eraseSize := sectors * ESP_FLASH_SECTOR

	// Количество пакетов данных (блоки по 4KB)
	blockSize := uint32(4096)
	numPackets := (size + blockSize - 1) / blockSize

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("🧮 Расчеты: %d секторов (%d байт) для стирания, %d пакетов по %d байт", sectors, eraseSize, numPackets, blockSize))
	}

	data := make([]byte, 16)
	binary.LittleEndian.PutUint32(data[0:4], eraseSize)  // Size to erase
	binary.LittleEndian.PutUint32(data[4:8], numPackets) // Number of data packets
	binary.LittleEndian.PutUint32(data[8:12], blockSize) // Packet size (4KB)
	binary.LittleEndian.PutUint32(data[12:16], offset)   // Flash offset

	if f.callback != nil {
		f.callback.emitLog("📤 Отправка команды FLASH_BEGIN...")
	}

	if err := f.sendCommand(ESP_FLASH_BEGIN, data, 0); err != nil {
		return fmt.Errorf("failed to send flash begin command: %w", err)
	}

	if f.callback != nil {
		f.callback.emitLog("⏳ Ожидание ответа на FLASH_BEGIN (может занять до 15 секунд для стирания)...")
	}

	response, err := f.readResponse(15 * time.Second) // Увеличиваем таймаут для стирания
	if err != nil {
		return fmt.Errorf("flash begin timeout: %w", err)
	}

	if len(response) < 8 || response[0] != 0x01 || response[1] != ESP_FLASH_BEGIN {
		return fmt.Errorf("flash begin failed: invalid response (len=%d, dir=0x%02x, cmd=0x%02x)", len(response), response[0], response[1])
	}

	// Проверяем статус (ESP32 ROM loader использует 4 байта)
	if len(response) >= 12 {
		status := response[len(response)-4]
		if status != 0x00 {
			errorCode := response[len(response)-3]
			return fmt.Errorf("flash begin failed: status=%d, error=%d", status, errorCode)
		}
	}

	if f.callback != nil {
		f.callback.emitLog("✅ Flash стерт, готов к передаче данных")
	}

	return nil
}

// flashData отправляет блок данных для прошивки
func (f *ESP32Flasher) flashData(data []byte, seq uint32) error {
	// Заголовок данных
	header := make([]byte, 16)
	binary.LittleEndian.PutUint32(header[0:4], uint32(len(data))) // Data size
	binary.LittleEndian.PutUint32(header[4:8], seq)               // Sequence number
	binary.LittleEndian.PutUint32(header[8:12], 0)                // Reserved
	binary.LittleEndian.PutUint32(header[12:16], 0)               // Reserved

	// Объединяем заголовок и данные
	payload := append(header, data...)
	checksum := calculateChecksum(data)

	// Повторяем попытки при ошибках
	for attempt := 0; attempt < 3; attempt++ {
		if err := f.sendCommand(ESP_FLASH_DATA, payload, checksum); err != nil {
			if attempt == 2 {
				return fmt.Errorf("failed to send flash data after 3 attempts: %w", err)
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}

		response, err := f.readResponse(5 * time.Second)
		if err != nil {
			if attempt == 2 {
				return fmt.Errorf("timeout reading flash data response at seq %d: %w", seq, err)
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if len(response) < 8 || response[0] != 0x01 || response[1] != ESP_FLASH_DATA {
			if attempt == 2 {
				return fmt.Errorf("invalid flash data response at sequence %d", seq)
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Проверяем статус (ESP32 ROM loader использует 4 байта)
		if len(response) >= 12 {
			status := response[len(response)-4]
			if status != 0x00 {
				errorCode := response[len(response)-3]
				return fmt.Errorf("flash data failed at seq %d: status=%d, error=%d", seq, status, errorCode)
			}
		}

		return nil // Успех
	}

	return fmt.Errorf("flash data failed at sequence %d after 3 attempts", seq)
}

// flashEnd завершает процесс прошивки
func (f *ESP32Flasher) flashEnd() error {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, 0) // Reboot

	if err := f.sendCommand(ESP_FLASH_END, data, 0); err != nil {
		return err
	}

	response, err := f.readResponse(3 * time.Second)
	if err != nil {
		return err
	}

	if len(response) < 8 || response[0] != 0x01 || response[1] != ESP_FLASH_END {
		return fmt.Errorf("flash end failed")
	}

	return nil
}

// FlashData прошивает данные в ESP32
func (f *ESP32Flasher) FlashData(data []byte, offset uint32, portName string) error {
	// 0. Пробуждение ESP32
	f.wakeupESP32()

	// 0.5. Тестирование связи
	if err := f.testConnection(); err != nil {
		if f.callback != nil {
			f.callback.emitLog("⚠️ ESP32 не отвечает, но продолжаем...")
		}
	}

	// 1. Синхронизация
	if f.callback != nil {
		f.callback.emitLog("🔄 Синхронизация с ESP32...")
		f.callback.emitProgress(30, "Синхронизация...")
	}

	if err := f.sync(); err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	// 2. Подключение SPI
	if f.callback != nil {
		f.callback.emitLog("🔗 Подключение к SPI Flash...")
		f.callback.emitProgress(40, "Подключение SPI...")
	}
	if err := f.spiAttach(); err != nil {
		return fmt.Errorf("SPI attach failed: %w", err)
	}

	// 3. Начало прошивки
	if f.callback != nil {
		f.callback.emitLog("🗑️ Стирание секторов Flash...")
		f.callback.emitProgress(50, "Стирание Flash...")
	}
	if err := f.flashBegin(uint32(len(data)), offset); err != nil {
		return fmt.Errorf("flash begin failed: %w", err)
	}

	// 4. Отправка данных блоками
	blockSize := 4096
	seq := uint32(0)
	totalBlocks := (len(data) + blockSize - 1) / blockSize

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("📤 Начинаем передачу данных (%d блоков по %d байт)...", totalBlocks, blockSize))
		f.callback.emitProgress(60, "Передача данных...")
	}

	for i := 0; i < len(data); i += blockSize {
		end := i + blockSize
		if end > len(data) {
			end = len(data)
		}

		block := make([]byte, blockSize)
		copy(block, data[i:end])
		// Заполняем оставшееся место 0xFF
		for j := end - i; j < blockSize; j++ {
			block[j] = 0xFF
		}

		if err := f.flashData(block, seq); err != nil {
			return fmt.Errorf("flash data failed at block %d/%d: %w", seq+1, totalBlocks, err)
		}

		// Обновляем прогресс
		if f.callback != nil {
			progress := 60 + int(float64(seq+1)/float64(totalBlocks)*30) // 60-90%
			percent := float64(seq+1) / float64(totalBlocks) * 100
			f.callback.emitProgress(progress, fmt.Sprintf("Запись %.1f%% (%d/%d блоков)", percent, seq+1, totalBlocks))

			// Более частое логирование для лучшей обратной связи
			if seq%5 == 0 || seq == uint32(totalBlocks-1) { // Логируем каждый 5-й блок или последний
				f.callback.emitLog(fmt.Sprintf("📦 Записан блок %d/%d (%.1f%%, %d байт)", seq+1, totalBlocks, percent, end-i))
			}
		}

		seq++
	}

	// 5. Завершение прошивки
	if f.callback != nil {
		f.callback.emitLog("🔄 Завершение прошивки...")
		f.callback.emitProgress(95, "Завершение...")
	}
	if err := f.flashEnd(); err != nil {
		return fmt.Errorf("flash end failed: %w", err)
	}

	return nil
}

// wakeupESP32 отправляет пробные данные для "пробуждения" ESP32
func (f *ESP32Flasher) wakeupESP32() {
	if f.callback != nil {
		f.callback.emitLog("📡 Отправка пробных данных для пробуждения ESP32...")
	}

	// Отправляем несколько символов для "пробуждения" ESP32
	wakeupData := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	f.port.Write(wakeupData)
	time.Sleep(10 * time.Millisecond)

	// Очищаем буферы
	f.port.ResetInputBuffer()
	f.port.ResetOutputBuffer()
	time.Sleep(50 * time.Millisecond)
}

// testConnection пытается просто отправить данные и прочитать что-то обратно
func (f *ESP32Flasher) testConnection() error {
	if f.callback != nil {
		f.callback.emitLog("🔍 Тестирование связи с ESP32...")
	}

	// Попытка 1: отправляем просто нули
	testData := []byte{0x00, 0x00, 0x00, 0x00}
	f.port.Write(testData)
	time.Sleep(100 * time.Millisecond)

	// Читаем что угодно
	buffer := make([]byte, 256)
	n, err := f.port.Read(buffer)
	if err == nil && n > 0 {
		if f.callback != nil {
			f.callback.emitLog(fmt.Sprintf("📡 Получено %d байт от ESP32", n))
		}
		return nil
	}

	// Попытка 2: отправляем AT команду
	f.port.ResetInputBuffer()
	f.port.ResetOutputBuffer()
	atCmd := []byte("AT\r\n")
	f.port.Write(atCmd)
	time.Sleep(100 * time.Millisecond)

	n, err = f.port.Read(buffer)
	if err == nil && n > 0 {
		if f.callback != nil {
			f.callback.emitLog(fmt.Sprintf("📡 Получено %d байт от ESP32 на AT команду", n))
		}
		return nil
	}

	// Попытка 3: пробуем отправить SLIP END символы
	slipEnd := []byte{SLIP_END, SLIP_END, SLIP_END}
	f.port.Write(slipEnd)
	time.Sleep(100 * time.Millisecond)

	n, err = f.port.Read(buffer)
	if err == nil && n > 0 {
		if f.callback != nil {
			f.callback.emitLog(fmt.Sprintf("📡 Получено %d байт от ESP32 на SLIP", n))
		}
		return nil
	}

	return fmt.Errorf("ESP32 не отвечает ни на какие команды")
}

// testBootloaderMode проверяет, находится ли ESP32 в режиме bootloader
func (f *ESP32Flasher) testBootloaderMode() bool {
	if f.callback != nil {
		f.callback.emitLog("🔍 Проверка режима bootloader...")
	}

	// Очищаем буферы
	f.port.ResetInputBuffer()
	f.port.ResetOutputBuffer()
	time.Sleep(100 * time.Millisecond)

	// Ждем завершения вывода bootloader'а
	if f.callback != nil {
		f.callback.emitLog("⏳ Ожидание завершения диагностики bootloader...")
	}

	isInBootloader := false
	buffer := make([]byte, 4096)

	// Читаем данные несколько раз, пока ESP32 выводит диагностику
	for attempt := 0; attempt < 10; attempt++ {
		f.port.SetReadTimeout(500 * time.Millisecond)
		n, err := f.port.Read(buffer)

		if err != nil || n == 0 {
			// Если нет данных уже 500мс, bootloader закончил вывод
			if f.callback != nil {
				f.callback.emitLog("🔍 Bootloader завершил диагностику, канал чист")
			}
			break
		}

		data := string(buffer[:n])
		if f.callback != nil {
			f.callback.emitLog(fmt.Sprintf("🔍 Bootloader output (#%d): %s", attempt+1, strings.TrimSpace(data)))
		}

		// Определяем, в каком режиме ESP32
		if strings.Contains(data, "waiting for download") ||
			strings.Contains(data, "download mode") {
			isInBootloader = true
			if f.callback != nil {
				f.callback.emitLog("✅ ESP32 явно сообщает о режиме download")
			}
		}

		// Признаки bootloader режима (с ошибками загрузки)
		if strings.Contains(data, "rst:0x10") ||
			strings.Contains(data, "boot:0x13") ||
			strings.Contains(data, "csum err") ||
			strings.Contains(data, "ets_main.c") {
			isInBootloader = true
			if f.callback != nil {
				f.callback.emitLog("✅ Обнаружены признаки bootloader режима")
			}
		}

		// Признаки успешной загрузки приложения (плохо!)
		if strings.Contains(data, "WiFi") ||
			strings.Contains(data, "IP") ||
			strings.Contains(data, "ESP-NOW") ||
			strings.Contains(data, "HTTP") ||
			strings.Contains(data, "TCP") ||
			strings.Contains(data, "app_main") {
			if f.callback != nil {
				f.callback.emitLog("❌ ESP32 загрузил приложение - НЕ в bootloader режиме")
			}
			return false
		}
	}

	// Теперь канал должен быть чист, пробуем SYNC команду
	if f.callback != nil {
		f.callback.emitLog("🔄 Канал чист, отправляем SYNC команду...")
	}

	// Окончательно очищаем буферы перед SYNC
	f.port.ResetInputBuffer()
	f.port.ResetOutputBuffer()
	time.Sleep(100 * time.Millisecond)

	// Отправляем SYNC команду
	syncData := make([]byte, 36)
	copy(syncData[:4], []byte{0x07, 0x07, 0x12, 0x20})
	for i := 4; i < 36; i++ {
		syncData[i] = 0x55
	}

	if err := f.sendCommand(ESP_SYNC, syncData, 0); err != nil {
		if f.callback != nil {
			f.callback.emitLog("⚠️ Ошибка отправки SYNC команды")
		}
		return isInBootloader // Возвращаем результат анализа диагностики
	}

	// Читаем ответ с увеличенным таймаутом
	response, err := f.readResponse(2 * time.Second)
	if err != nil {
		if f.callback != nil {
			f.callback.emitLog("⚠️ Нет ответа на SYNC команду")
		}
		return isInBootloader // Возвращаем результат анализа диагностики
	}

	// Проверяем структуру ответа
	if len(response) >= 8 &&
		response[0] == 0x01 &&
		response[1] == ESP_SYNC {
		if f.callback != nil {
			f.callback.emitLog("✅ SYNC ответ получен - ESP32 точно в режиме bootloader")
		}
		return true
	}

	if f.callback != nil {
		f.callback.emitLog("⚠️ Неверный ответ на SYNC команду")
		if isInBootloader {
			f.callback.emitLog("✅ Но диагностика показала bootloader режим - продолжаем")
		}
	}

	return isInBootloader
}

// espressifReferenceReset выполняет точную последовательность сброса из esp-serial-flasher
// Реализует usb_serial_converter_enter_bootloader() из официального репозитория Espressif
func (f *ESP32Flasher) espressifReferenceReset() {
	if f.callback != nil {
		f.callback.emitLog("🔄 Эталонная последовательность сброса (esp-serial-flasher)...")
	}

	// ТОЧНАЯ последовательность из usb_serial_converter_enter_bootloader():
	// 1. cdc_acm_host_set_control_line_state(device, true, false);  (DTR=1,RTS=0 -> GPIO0=LOW,EN=HIGH)
	// 2. usb_serial_converter_reset_target():
	//    - cdc_acm_host_set_control_line_state(device, true, true);   (DTR=1,RTS=1 -> GPIO0=LOW,EN=LOW)
	//    - loader_port_delay_ms(SERIAL_FLASHER_RESET_HOLD_TIME_MS);   (100ms)
	//    - cdc_acm_host_set_control_line_state(device, true, false);  (DTR=1,RTS=0 -> GPIO0=LOW,EN=HIGH)
	// 3. loader_port_delay_ms(SERIAL_FLASHER_BOOT_HOLD_TIME_MS);      (50ms)
	// 4. cdc_acm_host_set_control_line_state(device, false, false);   (DTR=0,RTS=0 -> GPIO0=HIGH,EN=HIGH)

	// В esp-serial-flasher DTR/RTS напрямую управляют GPIO0/EN (без инвертации)
	// DTR=true -> GPIO0=LOW, DTR=false -> GPIO0=HIGH
	// RTS=true -> EN=LOW,   RTS=false -> EN=HIGH

	// Очищаем буферы перед сбросом (эквивалент xStreamBufferReset)
	if f.callback != nil {
		f.callback.emitLog("  📋 Очистка буферов...")
	}
	f.port.ResetInputBuffer()
	f.port.ResetOutputBuffer()

	if f.callback != nil {
		f.callback.emitLog("  📋 Шаг 1: set_control_line_state(true, false) -> GPIO0=LOW, EN=HIGH")
	}
	f.port.SetDTR(true)               // GPIO0 = LOW (boot mode)
	f.port.SetRTS(false)              // EN = HIGH (не в сбросе)
	time.Sleep(10 * time.Millisecond) // Микро-задержка для стабилизации

	if f.callback != nil {
		f.callback.emitLog("  📋 Шаг 2a: set_control_line_state(true, true) -> GPIO0=LOW, EN=LOW (reset)")
	}
	f.port.SetDTR(true) // GPIO0 = LOW (остается в boot mode)
	f.port.SetRTS(true) // EN = LOW (assert reset)

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("  📋 Шаг 2b: Ожидание %dмс (reset hold)", SERIAL_FLASHER_RESET_HOLD_TIME_MS))
	}
	time.Sleep(SERIAL_FLASHER_RESET_HOLD_TIME_MS * time.Millisecond)

	if f.callback != nil {
		f.callback.emitLog("  📋 Шаг 2c: set_control_line_state(true, false) -> GPIO0=LOW, EN=HIGH (deassert reset)")
	}
	f.port.SetDTR(true)  // GPIO0 = LOW (остается в boot mode)
	f.port.SetRTS(false) // EN = HIGH (deassert reset)

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("  📋 Шаг 3: Удерживаем boot %dмс", SERIAL_FLASHER_BOOT_HOLD_TIME_MS))
	}
	time.Sleep(SERIAL_FLASHER_BOOT_HOLD_TIME_MS * time.Millisecond)

	if f.callback != nil {
		f.callback.emitLog("  📋 Шаг 4: set_control_line_state(false, false) -> GPIO0=HIGH, EN=HIGH (release boot)")
	}
	f.port.SetDTR(false) // GPIO0 = HIGH (exit boot mode)
	f.port.SetRTS(false) // EN = HIGH (остается не в сбросе)

	// Ещё раз очищаем буферы после сброса
	f.port.ResetInputBuffer()
	f.port.ResetOutputBuffer()

	// Дополнительная задержка для стабилизации ESP32 после reset sequence
	time.Sleep(200 * time.Millisecond)

	if f.callback != nil {
		f.callback.emitLog("  ✅ Эталонная последовательность сброса завершена")
	}
}

// espressifReferenceResetInverted выполняет ту же последовательность, но с инвертированной логикой DTR/RTS
// Некоторые USB-UART адаптеры (особенно дешевые) инвертируют сигналы
func (f *ESP32Flasher) espressifReferenceResetInverted() {
	if f.callback != nil {
		f.callback.emitLog("🔄 Эталонная последовательность с ИНВЕРТИРОВАННОЙ логикой DTR/RTS...")
	}

	// Инвертированная логика: DTR=false -> GPIO0=LOW, RTS=false -> EN=LOW

	// Очищаем буферы перед сбросом
	if f.callback != nil {
		f.callback.emitLog("  📋 Очистка буферов...")
	}
	f.port.ResetInputBuffer()
	f.port.ResetOutputBuffer()

	if f.callback != nil {
		f.callback.emitLog("  📋 Шаг 1: set_control_line_state(false, true) -> GPIO0=LOW, EN=HIGH (инвертированная логика)")
	}
	f.port.SetDTR(false)              // GPIO0 = LOW (boot mode) - инвертированная логика
	f.port.SetRTS(true)               // EN = HIGH (не в сбросе) - инвертированная логика
	time.Sleep(10 * time.Millisecond) // Микро-задержка для стабилизации

	if f.callback != nil {
		f.callback.emitLog("  📋 Шаг 2a: set_control_line_state(false, false) -> GPIO0=LOW, EN=LOW (reset) (инвертированная логика)")
	}
	f.port.SetDTR(false) // GPIO0 = LOW (остается в boot mode) - инвертированная логика
	f.port.SetRTS(false) // EN = LOW (assert reset) - инвертированная логика

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("  📋 Шаг 2b: Ожидание %dмс (reset hold)", SERIAL_FLASHER_RESET_HOLD_TIME_MS))
	}
	time.Sleep(SERIAL_FLASHER_RESET_HOLD_TIME_MS * time.Millisecond)

	if f.callback != nil {
		f.callback.emitLog("  📋 Шаг 2c: set_control_line_state(false, true) -> GPIO0=LOW, EN=HIGH (deassert reset) (инвертированная логика)")
	}
	f.port.SetDTR(false) // GPIO0 = LOW (остается в boot mode) - инвертированная логика
	f.port.SetRTS(true)  // EN = HIGH (deassert reset) - инвертированная логика

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("  📋 Шаг 3: Удерживаем boot %dмс", SERIAL_FLASHER_BOOT_HOLD_TIME_MS))
	}
	time.Sleep(SERIAL_FLASHER_BOOT_HOLD_TIME_MS * time.Millisecond)

	if f.callback != nil {
		f.callback.emitLog("  📋 Шаг 4: set_control_line_state(true, true) -> GPIO0=HIGH, EN=HIGH (release boot) (инвертированная логика)")
	}
	f.port.SetDTR(true) // GPIO0 = HIGH (exit boot mode) - инвертированная логика
	f.port.SetRTS(true) // EN = HIGH (остается не в сбросе) - инвертированная логика

	// Ещё раз очищаем буферы после сброса
	f.port.ResetInputBuffer()
	f.port.ResetOutputBuffer()

	// Дополнительная задержка для стабилизации ESP32 после reset sequence
	time.Sleep(200 * time.Millisecond)

	if f.callback != nil {
		f.callback.emitLog("  ✅ Инвертированная последовательность сброса завершена")
	}
}

// usbSerialConverterEnterBootloader выполняет эталонную последовательность USB-UART конвертера
// Реализует usb_serial_converter_enter_bootloader() из esp-serial-flasher
func (f *ESP32Flasher) usbSerialConverterEnterBootloader() error {
	f.espressifReferenceReset()
	return nil
}

// usbSerialConverterEnterBootloaderInverted выполняет эталонную последовательность с инвертированной логикой
// Для USB-UART адаптеров с инвертированными сигналами DTR/RTS
func (f *ESP32Flasher) usbSerialConverterEnterBootloaderInverted() error {
	f.espressifReferenceResetInverted()
	return nil
}

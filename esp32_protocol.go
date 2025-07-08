package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

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
	if len(data) < 2 {
		return nil, fmt.Errorf("packet too short: %d bytes", len(data))
	}

	// Ищем начало и конец SLIP пакета
	start := -1
	end := -1

	for i := 0; i < len(data); i++ {
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

// sendCommand отправляет команду в ESP32
func (f *ESP32Flasher) sendCommand(cmd byte, data []byte, checksum uint32) error {
	packet := make([]byte, 8+len(data))
	packet[0] = 0x00                                              // Direction (request)
	packet[1] = cmd                                               // Command
	binary.LittleEndian.PutUint16(packet[2:4], uint16(len(data))) // Size
	binary.LittleEndian.PutUint32(packet[4:8], checksum)          // Checksum
	copy(packet[8:], data)                                        // Data

	encoded := slipEncode(packet)

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("📤 Отправка команды 0x%02x (%d байт данных)", cmd, len(data)))
	}

	_, err := f.port.Write(encoded)
	return err
}

// readResponse читает и декодирует ответ от ESP32
func (f *ESP32Flasher) readResponse(timeout time.Duration) ([]byte, error) {
	f.port.SetReadTimeout(timeout)

	var allData bytes.Buffer
	buffer := make([]byte, 1024)
	start := time.Now()

	for time.Since(start) < timeout {
		n, err := f.port.Read(buffer)
		if err != nil && n == 0 {
			if allData.Len() > 0 {
				break
			}
			time.Sleep(1 * time.Millisecond)
			continue
		}

		if n > 0 {
			allData.Write(buffer[:n])

			// Проверяем наличие полного SLIP пакета
			data := allData.Bytes()
			if len(data) >= 2 {
				slipStart := -1
				slipEnd := -1
				for i := 0; i < len(data); i++ {
					if data[i] == SLIP_END {
						if slipStart == -1 {
							slipStart = i
						} else {
							slipEnd = i
							break
						}
					}
				}

				if slipStart != -1 && slipEnd != -1 {
					break
				}
			}
		}
	}

	if allData.Len() == 0 {
		return nil, fmt.Errorf("timeout reading response after %v", timeout)
	}

	decoded, err := slipDecode(allData.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to decode SLIP packet: %w", err)
	}

	return decoded, nil
}

// sync выполняет синхронизацию с ESP32 с обработкой цикла перезагрузок
func (f *ESP32Flasher) sync() error {
	if f.callback != nil {
		f.callback.emitLog("🔄 Синхронизация с ESP32 (обнаружен цикл перезагрузок)...")
	}

	// ESP32 в цикле перезагрузок - нужно ловить момент между reset'ами
	syncData := make([]byte, 36)
	syncData[0] = 0x07
	syncData[1] = 0x07
	syncData[2] = 0x12
	syncData[3] = 0x20
	for i := 4; i < 36; i++ {
		syncData[i] = 0x55
	}

	// Пробуем SYNC команду много раз с короткими интервалами
	for attempt := 0; attempt < 15; attempt++ { // Увеличиваем количество попыток
		if f.callback != nil && attempt > 0 {
			f.callback.emitLog(fmt.Sprintf("🔄 SYNC попытка %d/15 (ловим момент между reset'ами)...", attempt+1))
		}

		// Очищаем буферы агрессивно
		f.port.ResetInputBuffer()
		f.port.ResetOutputBuffer()
		time.Sleep(200 * time.Millisecond) // Короткая задержка

		// Отправляем команду быстро
		if err := f.sendCommand(ESP_SYNC, syncData, 0); err != nil {
			if f.callback != nil {
				f.callback.emitLog(fmt.Sprintf("⚠️ Ошибка отправки SYNC: %v", err))
			}
			continue
		}

		// Читаем ответ с коротким таймаутом
		response, err := f.readResponseRobust(1 * time.Second) // Короткий таймаут
		if err != nil {
			if f.callback != nil && attempt%3 == 0 { // Логируем только каждую 3-ю ошибку
				f.callback.emitLog(fmt.Sprintf("⚠️ SYNC попытка %d: %v", attempt+1, err))
			}
			continue
		}

		// Проверяем правильность ответа
		if len(response) >= 8 && response[0] == 0x01 && response[1] == ESP_SYNC {
			if f.callback != nil {
				f.callback.emitLog("✅ SYNC успешен! Bootloader стабилизирован.")
			}

			// Отправляем дополнительные команды для стабилизации
			for i := 0; i < 7; i++ {
				f.sendCommand(ESP_SYNC, []byte{}, 0)
				time.Sleep(10 * time.Millisecond)
				f.readResponseRobust(100 * time.Millisecond) // Игнорируем ответы
			}

			return nil
		}

		if f.callback != nil && len(response) > 0 {
			f.callback.emitLog(fmt.Sprintf("⚠️ Неверный SYNC ответ (len=%d): %x", len(response), response[:min(8, len(response))]))
		}

		// Короткая задержка перед следующей попыткой
		time.Sleep(150 * time.Millisecond)
	}

	// Если не удалось - пробуем принудительную стабилизацию
	if f.callback != nil {
		f.callback.emitLog("🔄 Попытка принудительной стабилизации bootloader...")
	}

	return f.forceBootloaderStabilization()
}

// forceBootloaderStabilization принудительно стабилизирует bootloader
func (f *ESP32Flasher) forceBootloaderStabilization() error {
	if f.callback != nil {
		f.callback.emitLog("🔧 Принудительная стабилизация - несколько сбросов подряд...")
	}

	// Делаем несколько быстрых сбросов для стабилизации
	for i := 0; i < 3; i++ {
		f.port.SetDTR(true) // GPIO0 = LOW (boot mode)
		f.port.SetRTS(true) // EN = LOW (reset)
		time.Sleep(50 * time.Millisecond)

		f.port.SetRTS(false) // EN = HIGH (release reset)
		time.Sleep(100 * time.Millisecond)

		// Очищаем мусор
		f.port.ResetInputBuffer()
		f.port.ResetOutputBuffer()
		time.Sleep(200 * time.Millisecond)
	}

	f.port.SetDTR(false)               // GPIO0 = HIGH (release boot mode)
	time.Sleep(500 * time.Millisecond) // Длинная пауза для стабилизации

	// Пробуем SYNC еще раз после стабилизации
	syncData := make([]byte, 36)
	syncData[0] = 0x07
	syncData[1] = 0x07
	syncData[2] = 0x12
	syncData[3] = 0x20
	for i := 4; i < 36; i++ {
		syncData[i] = 0x55
	}

	for attempt := 0; attempt < 5; attempt++ {
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
				f.callback.emitLog("✅ Принудительная стабилизация успешна!")
			}
			return nil
		}
	}

	return fmt.Errorf("failed to stabilize bootloader after all attempts")
}

// min helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// readResponseRobust улучшенная функция чтения с обработкой мусорных данных
func (f *ESP32Flasher) readResponseRobust(timeout time.Duration) ([]byte, error) {
	f.port.SetReadTimeout(timeout)

	var allData bytes.Buffer
	buffer := make([]byte, 1024)
	start := time.Now()

	// Читаем все данные в течение таймаута
	for time.Since(start) < timeout {
		n, err := f.port.Read(buffer)
		if err != nil && n == 0 {
			if allData.Len() > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}

		if n > 0 {
			allData.Write(buffer[:n])
			time.Sleep(10 * time.Millisecond) // Небольшая задержка
		}
	}

	if allData.Len() == 0 {
		return nil, fmt.Errorf("timeout reading response after %v", timeout)
	}

	rawData := allData.Bytes()

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("🔍 Получено %d байт: %x", len(rawData), rawData))
	}

	// Ищем все возможные SLIP пакеты в данных
	for startIdx := 0; startIdx < len(rawData); startIdx++ {
		if rawData[startIdx] != SLIP_END {
			continue
		}

		// Нашли начало, ищем конец
		for endIdx := startIdx + 1; endIdx < len(rawData); endIdx++ {
			if rawData[endIdx] != SLIP_END {
				continue
			}

			// Попробуем декодировать этот сегмент
			segment := rawData[startIdx : endIdx+1]
			decoded, err := slipDecode(segment)
			if err != nil {
				continue // Пробуем следующий сегмент
			}

			if f.callback != nil {
				f.callback.emitLog(fmt.Sprintf("✅ Декодирован SLIP пакет (%d байт): %x", len(decoded), decoded))
			}

			return decoded, nil
		}
	}

	// Если не нашли валидный SLIP пакет, проверяем на простые паттерны
	if len(rawData) >= 8 {
		// Ищем паттерн ответа без SLIP обертки
		for i := 0; i <= len(rawData)-8; i++ {
			if rawData[i] == 0x01 && rawData[i+1] == ESP_SYNC {
				response := rawData[i:]
				if len(response) >= 8 {
					if f.callback != nil {
						f.callback.emitLog(fmt.Sprintf("✅ Найден ответ без SLIP: %x", response[:8]))
					}
					return response, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("no valid SLIP packet or response found in %d bytes", len(rawData))
}

// testSync проверяет связь через SYNC команду с улучшенной обработкой
func (f *ESP32Flasher) testSync() bool {
	// Очищаем буферы и ждем стабилизации
	f.port.ResetInputBuffer()
	f.port.ResetOutputBuffer()
	time.Sleep(300 * time.Millisecond)

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

	response, err := f.readResponseRobust(2 * time.Second)
	if err != nil {
		return false
	}

	return len(response) >= 8 && response[0] == 0x01 && response[1] == ESP_SYNC
}

// detectChip определяет тип ESP32 чипа с улучшенной обработкой
func (f *ESP32Flasher) detectChip() error {
	if f.callback != nil {
		f.callback.emitLog("🔍 Определение типа чипа...")
	}

	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, CHIP_DETECT_MAGIC_REG_ADDR)

	if err := f.sendCommand(ESP_READ_REG, data, 0); err != nil {
		return fmt.Errorf("failed to read chip detect register: %w", err)
	}

	response, err := f.readResponseRobust(2 * time.Second)
	if err != nil {
		return fmt.Errorf("failed to read chip detect response: %w", err)
	}

	if len(response) < 12 {
		return fmt.Errorf("invalid chip detect response length: %d", len(response))
	}

	regValue := binary.LittleEndian.Uint32(response[8:12])

	switch regValue {
	case ESP32_CHIP_MAGIC:
		f.chipType = CHIP_ESP32
	case ESP32S2_CHIP_MAGIC:
		f.chipType = CHIP_ESP32S2
	case ESP32S3_CHIP_MAGIC:
		f.chipType = CHIP_ESP32S3
	case ESP32C3_CHIP_MAGIC:
		f.chipType = CHIP_ESP32C3
	default:
		f.chipType = CHIP_ESP32 // По умолчанию ESP32
	}

	if f.callback != nil {
		f.callback.emitLog(fmt.Sprintf("✅ Обнаружен чип: %s (0x%08x)", f.chipType.String(), regValue))
	}

	return nil
}

// spiAttach подключает SPI flash с улучшенной обработкой
func (f *ESP32Flasher) spiAttach() error {
	if f.callback != nil {
		f.callback.emitLog("🔗 Подключение к SPI Flash...")
	}

	data := make([]byte, 8)
	binary.LittleEndian.PutUint32(data[0:4], 0)
	binary.LittleEndian.PutUint32(data[4:8], 0)

	if err := f.sendCommand(ESP_SPI_ATTACH, data, 0); err != nil {
		return fmt.Errorf("failed to send SPI attach: %w", err)
	}

	response, err := f.readResponseRobust(5 * time.Second)
	if err != nil {
		return fmt.Errorf("SPI attach timeout: %w", err)
	}

	if len(response) < 8 || response[0] != 0x01 || response[1] != ESP_SPI_ATTACH {
		return fmt.Errorf("invalid SPI attach response")
	}

	if f.callback != nil {
		f.callback.emitLog("✅ SPI Flash подключен")
	}

	return nil
}

// calculateChecksum вычисляет контрольную сумму данных
func calculateChecksum(data []byte) uint32 {
	checksum := uint32(0xEF)
	for _, b := range data {
		checksum ^= uint32(b)
	}
	return checksum & 0xFF
}

// isInBootloader проверяет, находится ли ESP32 в режиме bootloader
func (f *ESP32Flasher) isInBootloader() bool {
	f.port.ResetInputBuffer()
	f.port.ResetOutputBuffer()
	time.Sleep(300 * time.Millisecond)

	buffer := make([]byte, 2048)
	bootloaderOutput := ""

	for i := 0; i < 8; i++ {
		f.port.SetReadTimeout(300 * time.Millisecond)
		n, _ := f.port.Read(buffer)
		if n > 0 {
			bootloaderOutput += string(buffer[:n])
		}
		time.Sleep(50 * time.Millisecond)
	}

	if f.callback != nil && bootloaderOutput != "" {
		f.callback.emitLog(fmt.Sprintf("🔍 Bootloader output: %s", strings.TrimSpace(bootloaderOutput)))
	}

	// Проверяем признаки bootloader режима
	isBootloader := strings.Contains(bootloaderOutput, "waiting for download") ||
		strings.Contains(bootloaderOutput, "download mode") ||
		strings.Contains(bootloaderOutput, "Brownout") ||
		strings.Contains(bootloaderOutput, "rst:0x10") ||
		strings.Contains(bootloaderOutput, "boot:0x13") ||
		strings.Contains(bootloaderOutput, "ets_main.c")

	// Признаки приложения (плохо!)
	if strings.Contains(bootloaderOutput, "ESP32 RC Transmitter") ||
		strings.Contains(bootloaderOutput, "WiFi") ||
		strings.Contains(bootloaderOutput, "app_main") ||
		strings.Contains(bootloaderOutput, "Версия прош") {
		if f.callback != nil {
			f.callback.emitLog("❌ ESP32 загрузил приложение - НЕ в bootloader режиме")
		}
		return false
	}

	f.port.ResetInputBuffer()
	f.port.ResetOutputBuffer()
	time.Sleep(200 * time.Millisecond)

	return isBootloader || f.testSync()
}

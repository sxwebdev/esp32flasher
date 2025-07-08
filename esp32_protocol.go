package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
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
)

// ProgressCallback интерфейс для коллбеков прогресса
type ProgressCallback interface {
	emitProgress(progress int, message string)
	emitLog(message string)
}

// ESP32Flasher - структура для работы с ESP32
type ESP32Flasher struct {
	port     serial.Port
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

	return &ESP32Flasher{port: port}, nil
}

// NewESP32FlasherWithProgress создает новый экземпляр флешера с коллбеками прогресса
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

	return &ESP32Flasher{port: port, callback: callback}, nil
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

// readResponse читает ответ от ESP32
func (f *ESP32Flasher) readResponse(timeout time.Duration) ([]byte, error) {
	f.port.SetReadTimeout(timeout)

	var buf bytes.Buffer
	temp := make([]byte, 1)
	inPacket := false

	for {
		n, err := f.port.Read(temp)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if n == 0 {
			continue
		}

		b := temp[0]
		buf.WriteByte(b)

		if b == SLIP_END {
			if inPacket {
				// Конец пакета
				break
			} else {
				// Начало пакета
				inPacket = true
			}
		}
	}

	return slipDecode(buf.Bytes())
}

// sync синхронизируется с ESP32
func (f *ESP32Flasher) sync() error {
	// Sync команда: 0x07 0x07 0x12 0x20 + 32 байта 0x55
	syncData := make([]byte, 36)
	syncData[0] = 0x07
	syncData[1] = 0x07
	syncData[2] = 0x12
	syncData[3] = 0x20
	for i := 4; i < 36; i++ {
		syncData[i] = 0x55
	}

	// Пытаемся синхронизироваться несколько раз
	for i := 0; i < 10; i++ {
		if err := f.sendCommand(ESP_SYNC, syncData, 0); err != nil {
			return err
		}

		response, err := f.readResponse(500 * time.Millisecond)
		if err == nil && len(response) >= 8 {
			// Проверяем успешный ответ
			if response[0] == 0x01 && response[1] == ESP_SYNC {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("failed to sync with ESP32")
}

// spiAttach подключает SPI flash
func (f *ESP32Flasher) spiAttach() error {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, 0) // Default SPI interface

	if err := f.sendCommand(ESP_SPI_ATTACH, data, 0); err != nil {
		return err
	}

	response, err := f.readResponse(3 * time.Second)
	if err != nil {
		return err
	}

	if len(response) < 8 || response[0] != 0x01 || response[1] != ESP_SPI_ATTACH {
		return fmt.Errorf("SPI attach failed")
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
	// Рассчитываем количество секторов для стирания
	sectors := (size + ESP_FLASH_SECTOR - 1) / ESP_FLASH_SECTOR
	eraseSize := sectors * ESP_FLASH_SECTOR

	data := make([]byte, 16)
	binary.LittleEndian.PutUint32(data[0:4], eraseSize)             // Size to erase
	binary.LittleEndian.PutUint32(data[4:8], (size+0xFFFF)&^0xFFFF) // Number of packets (aligned)
	binary.LittleEndian.PutUint32(data[8:12], 0x1000)               // Packet size (4KB)
	binary.LittleEndian.PutUint32(data[12:16], offset)              // Flash offset

	if err := f.sendCommand(ESP_FLASH_BEGIN, data, 0); err != nil {
		return err
	}

	response, err := f.readResponse(10 * time.Second)
	if err != nil {
		return err
	}

	if len(response) < 8 || response[0] != 0x01 || response[1] != ESP_FLASH_BEGIN {
		return fmt.Errorf("flash begin failed")
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

	if err := f.sendCommand(ESP_FLASH_DATA, payload, checksum); err != nil {
		return err
	}

	response, err := f.readResponse(5 * time.Second)
	if err != nil {
		return err
	}

	if len(response) < 8 || response[0] != 0x01 || response[1] != ESP_FLASH_DATA {
		return fmt.Errorf("flash data failed at sequence %d", seq)
	}

	return nil
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
func (f *ESP32Flasher) FlashData(data []byte, offset uint32) error {
	// 1. Синхронизация
	if f.callback != nil {
		f.callback.emitLog("🔄 Синхронизация с ESP32...")
		f.callback.emitProgress(25, "Синхронизация...")
	}
	if err := f.sync(); err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	// 2. Подключение SPI
	if f.callback != nil {
		f.callback.emitLog("🔗 Подключение к SPI Flash...")
		f.callback.emitProgress(35, "Подключение SPI...")
	}
	if err := f.spiAttach(); err != nil {
		return fmt.Errorf("SPI attach failed: %w", err)
	}

	// 3. Начало прошивки
	if f.callback != nil {
		f.callback.emitLog("🗑️ Стирание секторов Flash...")
		f.callback.emitProgress(45, "Стирание Flash...")
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
			return fmt.Errorf("flash data failed: %w", err)
		}

		// Обновляем прогресс
		if f.callback != nil {
			progress := 50 + int(float64(seq+1)/float64(totalBlocks)*40) // 50-90%
			f.callback.emitProgress(progress, fmt.Sprintf("Запись блока %d/%d", seq+1, totalBlocks))
			if seq%10 == 0 || seq == uint32(totalBlocks-1) { // Логируем каждый 10-й блок или последний
				f.callback.emitLog(fmt.Sprintf("📦 Записан блок %d/%d (%.1f%%)", seq+1, totalBlocks, float64(seq+1)/float64(totalBlocks)*100))
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

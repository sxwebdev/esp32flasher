package main

import (
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

	// Размеры блоков
	ESP_FLASH_SECTOR     = 4096
	ESP_FLASH_BLOCK      = 65536
	ESP_FLASH_WRITE_SIZE = 0x400 // 1024 bytes

	// Магические константы для определения чипа
	ESP32_CHIP_MAGIC   = 0x00f01d83
	ESP32S2_CHIP_MAGIC = 0x000007c6
	ESP32S3_CHIP_MAGIC = 0x00000009
	ESP32C3_CHIP_MAGIC = 0x6921506f

	// Регистры
	CHIP_DETECT_MAGIC_REG_ADDR = 0x40001000

	// Константы тайминга
	SERIAL_FLASHER_RESET_HOLD_TIME_MS = 100
	SERIAL_FLASHER_BOOT_HOLD_TIME_MS  = 50
)

// ChipType представляет тип ESP32 чипа
type ChipType int

const (
	CHIP_UNKNOWN ChipType = iota
	CHIP_ESP32
	CHIP_ESP32S2
	CHIP_ESP32S3
	CHIP_ESP32C3
)

func (c ChipType) String() string {
	switch c {
	case CHIP_ESP32:
		return "ESP32"
	case CHIP_ESP32S2:
		return "ESP32-S2"
	case CHIP_ESP32S3:
		return "ESP32-S3"
	case CHIP_ESP32C3:
		return "ESP32-C3"
	default:
		return "Unknown"
	}
}

// ProgressCallback интерфейс для коллбеков прогресса
type ProgressCallback interface {
	emitProgress(progress int, message string)
	emitLog(message string)
}

// ESP32Flasher - структура для работы с ESP32
type ESP32Flasher struct {
	port      serial.Port
	portName  string
	callback  ProgressCallback
	chipType  ChipType
	blockSize uint32
}

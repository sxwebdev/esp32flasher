package esp32

import (
	"time"

	"go.bug.st/serial"
)

// ESP32 ROM protocol commands.
const (
	ESP_FLASH_BEGIN    = 0x02
	ESP_FLASH_DATA     = 0x03
	ESP_FLASH_END      = 0x04
	ESP_MEM_BEGIN      = 0x05
	ESP_MEM_END        = 0x06
	ESP_MEM_DATA       = 0x07
	ESP_SYNC           = 0x08
	ESP_WRITE_REG      = 0x09
	ESP_READ_REG       = 0x0a
	ESP_SPI_SET_PARAMS = 0x0b
	ESP_SPI_ATTACH     = 0x0d
	ESP_SPI_FLASH_MD5  = 0x13

	// SLIP framing.
	SLIP_END     = 0xc0
	SLIP_ESC     = 0xdb
	SLIP_ESC_END = 0xdc
	SLIP_ESC_ESC = 0xdd

	// Conservative block sizes for reliable ROM transfers.
	ESP_FLASH_SECTOR     = 4096
	ESP_FLASH_BLOCK      = 65536
	ESP_FLASH_WRITE_SIZE = 0x400 // 1024 bytes.

	// Chip detection magic values.
	ESP32_CHIP_MAGIC   = 0x00f01d83
	ESP32S2_CHIP_MAGIC = 0x000007c6
	ESP32S3_CHIP_MAGIC = 0x00000009
	ESP32C3_CHIP_MAGIC = 0x6921506f

	// Registers.
	CHIP_DETECT_MAGIC_REG_ADDR = 0x40001000

	// Reset timing.
	SERIAL_FLASHER_RESET_HOLD_TIME_MS = 100
	SERIAL_FLASHER_BOOT_HOLD_TIME_MS  = 50
	SERIAL_APPLICATION_STARTUP_WAIT   = 4 * time.Second
	ESP_SYNC_ATTEMPTS                 = 5
)

// ChipType identifies an ESP32 family member.
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

// Callbacks connects the low-level flasher to an application UI.
// Nil functions are valid, so the flasher can also run headlessly.
type Callbacks struct {
	Progress func(progress int, message ProgressMessage)
	Log      func(message string)
}

// ProgressMessage describes UI progress without coupling the flasher to a
// particular human language. The application renders Key with its own catalog.
type ProgressMessage struct {
	Key    string         `json:"key"`
	Values map[string]any `json:"values,omitempty"`
}

type modemControl interface {
	Set(dtr, rts bool) error
	Close() error
}

func (c *Callbacks) emitProgress(progress int, message ProgressMessage) {
	if c != nil && c.Progress != nil {
		c.Progress(progress, message)
	}
}

func (c *Callbacks) emitLog(message string) {
	if c != nil && c.Log != nil {
		c.Log(message)
	}
}

// ESP32Flasher communicates with the ESP32 ROM bootloader.
type ESP32Flasher struct {
	port               serial.Port
	modemControl       modemControl
	refreshDTRAfterRTS bool
	callback           *Callbacks
	chipType           ChipType
	blockSize          uint32
	allowSpeedIncrease bool // Allows high-speed baud negotiation.
	rxBuffer           []byte
}

# ESP32 Flasher

A self-contained ESP32 flasher written in Go with a Wails desktop UI. It implements the ESP32 ROM bootloader protocol directly and does not require `esptool.py` at runtime.

## Features

- Single native desktop application with no external flashing tools
- ESP32 ROM protocol support: SLIP, SYNC, READ_REG, SPI_ATTACH, SPI_SET_PARAMS, FLASH_BEGIN, FLASH_DATA, FLASH_END, and SPI_FLASH_MD5
- Automatic entry into download mode through standard DevKit and direct DTR/RTS wiring
- Automatic 115200 → 921600 baud negotiation with 460800 and 115200 fallbacks
- Automatic classification of merged and application-only images
- ROM-side MD5 verification before reboot
- Built-in serial monitor from 9600 to 921600 baud
- Progress reporting, bounded block retries, and detailed error messages

## Quick start

### Flash firmware

1. Start the application.
2. Select the ESP32 serial port.
3. Select a `.bin` firmware image.
4. Click **Flash firmware**.
5. The application enters the ROM bootloader, writes and verifies the image, then reboots the board.

A full merged image is written at `0x0`. An application-only image is written at `0x10000`.

### Monitor serial output

1. Select the ESP32 serial port and baud rate.
2. Click **Monitor**.
3. Click **Stop** before starting a flash operation.

## Wiring

The flasher first tries the standard Espressif DevKit auto-reset circuit. It then falls back to direct wiring:

```text
ESP32 GPIO0 <--[1 kΩ]-- DTR (USB-to-UART)
ESP32 EN    <--[1 kΩ]-- RTS (USB-to-UART)
```

For direct wiring, GPIO0 is held low while EN is reset, EN is released, and GPIO0 is released 50 ms later.

## Development

```bash
go test ./...
go vet ./...
cd frontend && npm run build
wails build
```

The hardware integration test is opt-in:

```bash
ESP32_FLASH_PORT=/dev/cu.usbserial-0001 \
ESP32_FLASH_IMAGE=testdata/esp32_rx_hardworker_latest.merged.bin \
go test ./internal/esp32 -run TestFlashESP32Hardware -v
```

See [Flasher knowledge base](docs/flasher-knowledge-base.md) for architecture, protocol details, failure modes, and development guidance.

## Releases

Push a tag to build macOS and Windows binaries for AMD64 and ARM64 and attach them to a GitHub release:

```bash
make release TAG=v1.2.3
```

## License

MIT License

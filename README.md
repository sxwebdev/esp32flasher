# ESP32 Flasher

A standalone single-binary ESP32 flasher written in Go with Wails GUI. Implements native ESP32 ROM bootloader protocol without dependency on esptool.py.

## Features

- **Single Binary**: No external dependencies, no Python required
- **Native Protocol**: Custom ESP32 ROM bootloader implementation (SLIP framing, command protocol)
- **Fast Flashing**: Zlib compression + 460800 baud rate for ~30s flash time on 1.8MB firmware
- **Auto Reset**: Automatic DTR/RTS reset sequence to enter bootloader mode
- **Serial Monitor**: Built-in serial monitor for ESP32 debugging (9600-921600 baud)
- **Progress Tracking**: Real-time progress bar and detailed logs with millisecond timestamps
- **Multiple Offsets**: Support for different flash addresses (0x0, 0x1000, 0x8000, 0x10000, custom)

## Quick Start

### Flashing

1. Launch the application
2. Select COM port and refresh if needed
3. Choose firmware file (.bin)
4. Select flash address (0x10000 for app, 0x0 for merged binary)
5. Click "Flash ESP32"
6. ESP32 will automatically enter bootloader and flash

### Serial Monitor

1. Select COM port
2. Choose baud rate (default: 115200)
3. Click "Monitor"
4. View real-time ESP32 output
5. Click "Stop" to disconnect

## Technical Details

### Reset Sequence

Uses classic DTR/RTS reset sequence:

```text
DTR=false (IO0=HIGH)
RTS=true  (EN=LOW, reset)
wait 100ms
DTR=true  (IO0=LOW, bootloader mode)
RTS=false (EN=HIGH, run)
wait 50ms
DTR=false (IO0=HIGH)
```

### Flash Protocol

- Initial baud: 115200, switches to 460800 for transfer
- Block size: 16KB for optimal speed
- Compression: Zlib deflate (typically 50-70% compression)
- Erase timeout: 30-60s depending on file size

### Flash Addresses

| Address | Description              |
| ------- | ------------------------ |
| 0x0     | Full/merged binary image |
| 0x1000  | Bootloader               |
| 0x8000  | Partition table          |
| 0x10000 | Application (default)    |

## Building

Requires Go 1.21+ and Wails CLI:

```bash
# Install Wails CLI
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# Build application
wails build
```

Output: `build/bin/espflasher.app` (macOS) or `build/bin/espflasher.exe` (Windows)

## Dependencies

- [Wails v2](https://wails.io/) - Go/HTML5 desktop framework
- [go.bug.st/serial](https://github.com/bugst/go-serial) - Serial port library

## License

MIT License

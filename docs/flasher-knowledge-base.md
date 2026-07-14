# ESP32 Flasher Knowledge Base

This document describes how the flasher is structured, how it communicates with the ESP32 ROM bootloader, and which constraints must be preserved during future development.

## Scope

The application is a native Wails desktop program. It contains its own ESP32 serial flashing implementation and does not invoke `esptool.py`. The current protocol layer recognizes ESP32, ESP32-S2, ESP32-S3, and ESP32-C3 chip magic values. Hardware behavior has been validated with a classic ESP32 and a 4 MiB merged image.

## Repository layout

```text
.
├── app.go                         Wails API for port discovery and flashing
├── monitor.go                     Wails API for serial monitoring
├── main.go                        Wails application entry point
├── internal/
│   ├── esp32/
│   │   ├── flasher.go             boot/reset, erase, write, verify, reboot
│   │   ├── protocol.go            SLIP framing and ROM command transport
│   │   ├── types.go               constants, chip types, callbacks, state
│   │   └── *_test.go              unit and opt-in hardware tests
│   └── firmware/
│       ├── image.go               image classification and flash offset
│       └── image_test.go
├── frontend/                      Wails web frontend
├── testdata/                      local hardware-test firmware
├── docs/                          development knowledge base
└── .github/workflows/release.yml tag-driven release builds
```

`App` must remain in Go package `main`. Wails generates the frontend binding as `window.go.main.App`, and the current frontend imports `wailsjs/go/main/App.js`. Moving `App` into an internal package changes that namespace and breaks the generated API unless the frontend and generated bindings are migrated together.

## Layer responsibilities

### Wails application layer

`app.go` validates the selected file, loads it, classifies it through `internal/firmware`, creates the flasher, relays callbacks to Wails events, and reboots only after a successful write. It emits:

- `flash-progress` with a percentage and message;
- `flash-log` with a diagnostic line.

`monitor.go` owns the user-facing serial monitor. Flashing and monitoring must never use the same port at the same time.

### Firmware classification

`firmware.Detect` returns an offset and whether the image is complete:

- a filename containing `merged` with at least `0x10000` bytes is considered a full image;
- a binary with an ESP image magic byte at `0x1000` and the partition-table magic `0xAA50` at `0x8000` is considered a full image;
- every other image is treated as an application image and written at `0x10000`.

The classifier is intentionally conservative. A random 4 MiB file is not automatically treated as a merged image. Add a positive format check before supporting another layout.

### ESP32 transport layer

`internal/esp32` owns the serial port and ROM protocol. UI code interacts with it only through constructors, `Flash`, `RebootTarget`, `Close`, and callbacks.

`Callbacks` contains optional `Progress` and `Log` functions. Keeping these callbacks as functions avoids coupling the internal package to Wails and allows hardware tests or future CLI tools to reuse the flasher.

## End-to-end flash sequence

The main operation follows this order:

1. Open the serial port at 115200 baud, 8 data bits, no parity, one stop bit.
2. Test whether the ROM bootloader already responds to SYNC.
3. Try the standard DevKit reset sequence once.
4. Try the direct DTR-to-GPIO0 and RTS-to-EN sequence up to three times.
5. Synchronize and drain duplicate SYNC responses.
6. Read the chip magic register at `0x40001000`.
7. Negotiate 921600 baud, then 460800 if needed; validate each rate with SYNC.
8. Send `SPI_ATTACH`.
9. Send `SPI_SET_PARAMS` for a 4, 8, or 16 MiB Flash range.
10. Send `FLASH_BEGIN`, which erases the aligned target range.
11. Send 1024-byte `FLASH_DATA` blocks, padding the final block with `0xFF`.
12. Ask the ROM to calculate MD5 over the exact, unpadded image range.
13. Compare the ROM digest with the local image digest.
14. Send `FLASH_END` with the stay-in-loader flag.
15. Restore the host UART to 115200 baud.
16. Perform one hardware reset into normal execution mode.

Do not reorder SPI configuration after `FLASH_BEGIN`. On the tested ESP32 ROM, omitting `SPI_SET_PARAMS` caused ROM status `1` with reason `0x06`.

## Reset and boot mode

The ESP32 samples GPIO0 while EN rises. GPIO0 low selects the ROM download bootloader; GPIO0 high starts normal execution.

### Standard DevKit circuit

The DevKit transistor circuit combines DTR and RTS and is designed so asserting both does not permanently hold the chip in reset. The implemented sequence is based on Espressif behavior:

1. release DTR;
2. assert RTS for 100 ms;
3. assert DTR so GPIO0 is low;
4. release RTS so EN rises;
5. keep GPIO0 low for 50 ms;
6. release DTR.

### Direct wiring

For an adapter wired directly as DTR → GPIO0 and RTS → EN:

1. release both lines;
2. assert DTR to hold GPIO0 low;
3. assert RTS to hold EN low for 100 ms;
4. release RTS;
5. keep GPIO0 low for another 50 ms;
6. release DTR.

Serial libraries expose logical DTR/RTS states, while USB-to-UART hardware is commonly active-low. Always test control-line changes with a fake port before changing reset timing or polarity.

If automatic entry fails, the user can hold BOOT, press and release EN/RESET, release BOOT, and retry.

### Reset after a successful flash

Do not ask `FLASH_END` to reboot and then immediately perform a hardware reset. Those two mechanisms can race, and a successful serial control-line call does not prove that the application started. The completion sequence intentionally keeps the ROM loader active, restores the host UART to 115200, and performs one hard reset:

1. release GPIO0 and EN;
2. wait 50 ms for a stable neutral state;
3. hold EN low for 100 ms;
4. release EN while GPIO0 remains high;
5. keep the port open for four seconds while the ROM and application initialize;
6. leave both DTR and RTS released.

This matches the normal-boot intent of Arduino IDE/esptool: verification completes before reset, and the final reset is an RTS/EN pulse with the boot pin released.

The four-second stabilization period is intentional. On tested macOS USB-to-UART hardware, closing a high-speed flashing session after only 500 ms could make the next monitor session receive the final application log fragment continuously (for example, `на!` at about 80 KiB/s). A raw hardware regression test proved that keeping the port open through application startup prevents the adapter/driver replay state. Do not shorten this delay without rerunning the complete flash-close-reopen lifecycle test.

## ROM packet format and SLIP

Commands use an eight-byte little-endian header followed by command data:

```text
direction | command | data length (uint16) | checksum/value (uint32) | data
```

Packets are framed with SLIP delimiters (`0xC0`). Within a frame, `0xC0` becomes `0xDB 0xDC` and `0xDB` becomes `0xDB 0xDD`.

Serial reads are stream-oriented. One read may contain boot messages, a partial frame, or several complete frames. `extractSLIPFrame` therefore returns the first complete non-empty frame and preserves the remainder in `rxBuffer`. Do not replace this with a one-read/one-response assumption.

One SYNC request can generate multiple SYNC responses. The implementation drains them before sending `READ_REG`, and command-specific response reads ignore frames for other command IDs.

## Commands used

| Command           |     ID | Purpose                                    |
| ----------------- | -----: | ------------------------------------------ |
| `FLASH_BEGIN`     | `0x02` | Erase the range and begin a Flash transfer |
| `FLASH_DATA`      | `0x03` | Write one data block                       |
| `FLASH_END`       | `0x04` | Finish the Flash operation                 |
| `SYNC`            | `0x08` | Establish or validate communication        |
| `READ_REG`        | `0x0A` | Read the chip-detection register           |
| `SPI_SET_PARAMS`  | `0x0B` | Tell the ROM the Flash geometry            |
| `SPI_ATTACH`      | `0x0D` | Attach the SPI Flash peripheral            |
| `CHANGE_BAUDRATE` | `0x0F` | Change ROM serial speed                    |
| `SPI_FLASH_MD5`   | `0x13` | Calculate a digest over a Flash range      |

`READ_REG` is special: its returned register value is in the 32-bit header value field at response bytes `4:8`, not in the trailing status bytes.

ROM command responses end with status and reason bytes. Always validate both the response command ID and ROM status. Preserving the reason code in errors is essential for diagnosing chip-specific failures.

## Flash geometry and block handling

The transfer block size is deliberately 1024 bytes. Larger blocks can improve throughput but may exceed ROM or adapter expectations. Any block-size change must be checked on every supported chip and adapter.

The Flash geometry sent to the ROM uses:

- 64 KiB erase blocks;
- 4 KiB erase sectors;
- 256-byte program pages;
- a `0x0000FFFF` status mask.

The configured Flash size is the smallest supported size that contains the complete `offset + image length` range: 4, 8, or 16 MiB. Larger ranges are rejected.

Every `FLASH_DATA` block uses the ESP ROM XOR checksum initialized to `0xEF`. The final block is padded to 1024 bytes with `0xFF`, but MD5 verification covers only the original image length.

Each block has up to three attempts and a five-second response timeout. Before a retry, the receive buffer and host input buffer are cleared. Do not retry indefinitely; an unbounded retry loop can keep a device erased and make failures impossible to diagnose.

## Partial serial writes

`serial.Port.Write` is allowed to accept fewer bytes than requested without returning an error. `writeAll` loops until the complete SLIP frame has been sent. A single `Write` call caused intermittent CRC failures and response timeouts in hardware testing, so this loop must not be removed.

## Baud-rate negotiation

The connection starts at 115200 baud. Automatic mode tries 921600 and then 460800. After the ROM acknowledges a baud change, the already-open host port is reconfigured with `SetMode`, and SYNC validates the new rate.

If validation fails, the host returns to 115200 and re-enters the ROM bootloader before trying the next speed. Manual mode leaves speed negotiation disabled because it is intended for recovery scenarios where the Flash or reset circuit may be unreliable.

Reopening the port solely to change baud rate can toggle DTR/RTS and reset the board unexpectedly. Prefer changing the mode on the existing port.

## Optional stub-loader fast path

The historical `master` implementation embedded an esptool ESP32 RAM stub, compressed firmware with zlib, and sent 16 KiB deflate blocks. This is the main optimization worth revisiting because it can reduce both transferred bytes and per-block acknowledgements.

Do not copy that implementation directly into the ROM path. Its embedded stub is specific to the classic ESP32, it writes each SLIP frame with one unchecked serial `Write`, and it treats an MD5 command failure as a warning. A production stub path must:

- select a pinned stub by detected chip family and revision;
- preserve `writeAll` and the streaming SLIP response parser;
- validate the stub handshake before changing protocol semantics;
- keep command retries bounded;
- make every erase, deflate-end, and MD5 error fatal;
- fall back to the existing ROM implementation if stub upload or startup fails;
- retain ROM mode as the recovery path.

The ROM transfer block must remain 1024 bytes. A 16 KiB block is appropriate only after the RAM stub is running and explicitly supports the deflate protocol.

## Verification and NVS

Verification must happen while the ROM bootloader still owns the chip. Once the application boots, it may initialize or update NVS immediately. A full-chip read performed after reboot can therefore differ from the original merged image even when flashing was correct.

The verified range is exactly `offset` through `offset + image length`. The ROM-provided MD5 must match the local MD5 before `FLASH_END` and reboot are allowed.

## Serial monitor

The monitor must open the port with `InitialStatusBits` set to `DTR=false` and `RTS=false`, then explicitly release both lines again after opening. The serial package otherwise defaults both outputs to asserted. That can drive GPIO0 and EN on adapters wired directly to the ESP32 and leave the target in the wrong boot/reset state.

The serial monitor reads with a 50 ms timeout and keeps incomplete lines in a byte buffer. Complete UTF-8 text lines are emitted in batches no more often than every 75 ms. A line is split after 4096 bytes so an unterminated device stream cannot grow memory indefinitely.

Invalid UTF-8 and control-heavy data is treated as binary. Instead of forwarding replacement characters for every byte, the backend reports one compact warning per second with the byte count and a short hexadecimal sample. This usually indicates an incorrect baud rate or a device protocol that is binary rather than a text console.

Consecutive identical text lines are also coalesced. The first line is emitted normally, then the backend reports `Previous line repeated N times` no more than once per second. This is a safety boundary for faulty firmware: a hardware capture found a target emitting more than 22 KiB of the literal line `g mode` in 250 ms. Do not silently discard the repetition count because it is evidence of a device-side log loop.

Each monitor run owns an isolated session with its own port, stop signal, and completion signal. Port closure is idempotent, monitor start/stop operations are serialized, and Stop closes the port to unblock an active read instead of sleeping for a fixed duration.

The frontend renders pending logs at most once every 50 ms. It retains at most 500 lines, 160,000 characters in total, and 2048 characters per displayed line. The terminal has `aria-live` disabled because repeatedly announcing a high-volume console can overload the webview accessibility layer.

## Testing

Run the safe test suite with:

```bash
go test ./... -count=1 -race
go vet ./...
```

Unit tests cover reset control-line ordering, Flash-size selection, baud mode changes, SLIP stream extraction, chip magic values, ROM status parsing, SPI parameters, partial serial writes, and image classification.

The hardware test is guarded by `ESP32_FLASH_PORT`; ordinary test runs skip it. A full hardware write is destructive to the selected board:

```bash
ESP32_FLASH_PORT=/dev/cu.usbserial-0001 \
ESP32_FLASH_IMAGE=testdata/esp32_rx_hardworker_latest.merged.bin \
go test ./internal/esp32 -run TestFlashESP32Hardware -v -count=1
```

To validate an application-only update while preserving the existing bootloader and partition table:

```bash
ESP32_FLASH_PORT=/dev/cu.usbserial-0001 \
ESP32_FLASH_IMAGE=testdata/esp32_rx_hardworker_latest.bin \
go test ./internal/esp32 -run TestFlashESP32Hardware -v -count=1
```

Optional hardware-test modes:

- `ESP32_PROBE_ONLY=1` enters the bootloader, detects the chip, and configures SPI without writing;
- `ESP32_REBOOT_ONLY=1` only resets the target after establishing a bootloader connection;
- `ESP32_VERIFY_BOOT=1` requires exactly one `rst:`, normal SPI Flash boot, and the fixture application banner after reset;
- `ESP32_VERIFY_UPTIME=1` extends boot verification to 35 seconds and reports any `g mode` lines without accepting additional resets.

The monitor control-line test is also opt-in and performs one reset without writing Flash:

```bash
ESP32_MONITOR_PORT=/dev/cu.usbserial-0001 \
go test . -run TestMonitorESP32Hardware -v -count=1
```

For a short, unprocessed UART diagnostic sample without reset or Flash writes:

```bash
ESP32_MONITOR_CAPTURE_PORT=/dev/cu.usbserial-0001 \
go test . -run TestMonitorESP32RawCapture -v -count=1
```

To reproduce the production application-only lifecycle—destructive flash at `0x10000`, reset, close, wait, and reopen the monitor—run:

```bash
ESP32_LIFECYCLE_PORT=/dev/cu.usbserial-0001 \
go test . -run TestProductionFlashMonitorLifecycleHardware -v -count=1
```

The lifecycle test fails if one raw serial line is received more than ten times after reopening. It uses `testdata/esp32_rx_hardworker_latest.bin`, not the merged image.

Relative image paths are resolved from either the package directory or the repository root. When the image path is omitted, the test uses `testdata/esp32_rx_hardworker_latest.merged.bin`.

## Release builds

`.github/workflows/release.yml` runs on every pushed tag. It builds and publishes:

- macOS AMD64;
- macOS ARM64;
- Windows AMD64;
- Windows ARM64.

The macOS output is a zipped `.app`; Windows output is an `.exe`. The release job creates a GitHub release or replaces artifacts on an existing release for the same tag.

## Development rules

- Keep the ROM protocol independent from Wails and frontend packages.
- Preserve command-specific response matching and the persistent receive buffer.
- Configure SPI before beginning a Flash operation.
- Handle partial writes and bounded retries explicitly.
- Verify before reboot, never after application startup.
- Add unit tests for protocol framing, offsets, status parsing, and reset sequences before hardware testing.
- Run probe-only mode before a destructive hardware test when modifying reset, chip detection, or SPI setup.
- Test full merged images and application-only images separately when changing classification.
- Keep logs actionable: include the command, block number, ROM status, and reason code when available.

## Troubleshooting

### No ROM bootloader response

Check that the selected port is correct, no serial monitor owns it, GPIO0 is low while EN rises, and the adapter exposes functional DTR/RTS lines. Try manual BOOT/RESET entry.

### `FLASH_BEGIN` returns a ROM error

Confirm `SPI_ATTACH` and `SPI_SET_PARAMS` both succeeded and that the configured Flash size contains the complete image range.

### Random block CRC errors or timeouts

Verify that the complete encoded frame is written with `writeAll`, reduce baud rate, replace the USB cable, and check power stability. Do not assume a successful partial `Write` sent the complete block.

### MD5 mismatch after reboot

Do not compare the complete chip after application startup without accounting for mutable partitions such as NVS. Use the ROM-side pre-reboot digest produced by the flasher.

### Flashing is slower than Arduino IDE

Confirm that 921600 baud negotiation succeeds. Throughput also depends on 1024-byte ROM blocks, per-block acknowledgements, USB-to-UART latency, and conservative retry behavior. Increase block size only after cross-chip hardware validation.

### The monitor repeats the tail of the final startup line

If the monitor receives a short fragment such as `на!` thousands of times immediately after flashing, first reproduce it with `TestProductionFlashMonitorLifecycleHardware`. This pattern was caused by closing the high-speed flashing port during early application UART output, not by merged-image detection or frontend rendering. Preserve the post-reset stabilization period and verify that both DTR and RTS remain released when the port closes and reopens.

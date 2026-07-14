package esp32

import (
	"bytes"
	"strings"
	"testing"
)

type partialWritePort struct {
	fakeSerialPort
	maxWrite int
	written  []byte
}

func (p *partialWritePort) Write(data []byte) (int, error) {
	n := min(len(data), p.maxWrite)
	p.written = append(p.written, data[:n]...)
	return n, nil
}

func TestExtractSLIPFramePreservesFollowingPacket(t *testing.T) {
	t.Context()
	first := []byte{0x01, ESP_SYNC, 0xc0, 0xdb, 0x00, 0x00, 0x00, 0x00}
	second := []byte{0x01, ESP_READ_REG, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00}
	stream := append([]byte("boot noise"), SLIP_END)
	stream = append(stream, slipEncode(first)...)
	stream = append(stream, slipEncode(second)...)

	gotFirst, rest, ok := extractSLIPFrame(stream)
	if !ok {
		t.Fatal("extractSLIPFrame() did not find the first packet")
	}
	if !bytes.Equal(gotFirst, first) {
		t.Fatalf("first packet = %x, want %x", gotFirst, first)
	}

	gotSecond, rest, ok := extractSLIPFrame(rest)
	if !ok {
		t.Fatal("extractSLIPFrame() did not preserve the second packet")
	}
	if !bytes.Equal(gotSecond, second) {
		t.Fatalf("second packet = %x, want %x", gotSecond, second)
	}
	if len(rest) != 0 {
		t.Fatalf("remaining bytes = %x, want empty", rest)
	}
}

func TestExtractSLIPFramePreservesPartialPacket(t *testing.T) {
	t.Context()
	partial := []byte{SLIP_END, 0x01, ESP_SYNC}
	stream := append([]byte("application output"), partial...)

	_, rest, ok := extractSLIPFrame(stream)
	if ok {
		t.Fatal("extractSLIPFrame() found an incomplete packet")
	}
	if !bytes.Equal(rest, partial) {
		t.Fatalf("partial packet = %x, want %x", rest, partial)
	}
}

func TestChipTypeFromMagic(t *testing.T) {
	t.Context()
	tests := []struct {
		name  string
		magic uint32
		want  ChipType
	}{
		{name: "ESP32", magic: ESP32_CHIP_MAGIC, want: CHIP_ESP32},
		{name: "ESP32-S2", magic: ESP32S2_CHIP_MAGIC, want: CHIP_ESP32S2},
		{name: "ESP32-S3", magic: ESP32S3_CHIP_MAGIC, want: CHIP_ESP32S3},
		{name: "ESP32-C3", magic: ESP32C3_CHIP_MAGIC, want: CHIP_ESP32C3},
		{name: "unknown", magic: 0xdeadbeef, want: CHIP_UNKNOWN},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := chipTypeFromMagic(tt.magic); got != tt.want {
				t.Fatalf("chipTypeFromMagic(0x%08x) = %v, want %v", tt.magic, got, tt.want)
			}
		})
	}
}

func TestCheckROMStatus(t *testing.T) {
	t.Context()

	if err := checkROMStatus([]byte{1, ESP_FLASH_BEGIN, 4, 0, 0, 0, 0, 0, 0, 0, 0, 0}); err != nil {
		t.Fatalf("checkROMStatus(success) error = %v", err)
	}

	err := checkROMStatus([]byte{1, ESP_FLASH_BEGIN, 4, 0, 0, 0, 0, 0, 1, 5, 0, 0})
	if err == nil {
		t.Fatal("checkROMStatus(error) returned nil")
	}
	if got := err.Error(); !strings.Contains(got, "reason=0x05") {
		t.Fatalf("checkROMStatus(error) = %q, want reason code", got)
	}

	responseWithData := append([]byte{1, ESP_SPI_FLASH_MD5, 36, 0, 0, 0, 0, 0}, bytes.Repeat([]byte{'a'}, 32)...)
	responseWithData = append(responseWithData, 0, 0, 0, 0)
	if err := checkROMStatusAfter(responseWithData, 32); err != nil {
		t.Fatalf("checkROMStatusAfter(success) error = %v", err)
	}
}

func TestMakeSPIParameters(t *testing.T) {
	t.Context()
	got := makeSPIParameters(4 * 1024 * 1024)
	want := []byte{
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x40, 0x00,
		0x00, 0x00, 0x01, 0x00,
		0x00, 0x10, 0x00, 0x00,
		0x00, 0x01, 0x00, 0x00,
		0xff, 0xff, 0x00, 0x00,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("makeSPIParameters() = %x, want %x", got, want)
	}
}

func TestWriteAllHandlesPartialSerialWrites(t *testing.T) {
	t.Context()
	port := &partialWritePort{maxWrite: 7}
	flasher := &ESP32Flasher{port: port}
	want := bytes.Repeat([]byte{0xc0, 0xdb, 0x55}, 100)

	if err := flasher.writeAll(want); err != nil {
		t.Fatalf("writeAll() error = %v", err)
	}
	if !bytes.Equal(port.written, want) {
		t.Fatalf("writeAll() wrote %d bytes, want %d", len(port.written), len(want))
	}
}

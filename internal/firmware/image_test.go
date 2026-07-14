package firmware

import (
	"bytes"
	"errors"
	"os"
	"testing"
)

func TestClassifyFirmwareImage(t *testing.T) {
	t.Context()

	tests := []struct {
		name     string
		filePath string
		data     func() []byte
		want     Image
	}{
		{
			name:     "merged filename",
			filePath: "/firmware/device.merged.bin",
			data:     func() []byte { return make([]byte, 0x10000) },
			want:     Image{Offset: 0, Full: true},
		},
		{
			name:     "small misleading filename",
			filePath: "/firmware/not-actually-merged.bin",
			data:     func() []byte { return []byte{0xe9} },
			want:     Image{Offset: 0x10000},
		},
		{
			name:     "merged ESP32 layout",
			filePath: "/firmware/release.bin",
			data: func() []byte {
				data := make([]byte, 0x8002)
				data[0x1000] = 0xe9
				data[0x8000] = 0xaa
				data[0x8001] = 0x50
				return data
			},
			want: Image{Offset: 0, Full: true},
		},
		{
			name:     "application image",
			filePath: "/firmware/application.bin",
			data:     func() []byte { return []byte{0xe9, 0x04, 0x00, 0x00} },
			want:     Image{Offset: 0x10000},
		},
		{
			name:     "large file without merged layout",
			filePath: "/firmware/storage.bin",
			data:     func() []byte { return make([]byte, 4*1024*1024) },
			want:     Image{Offset: 0x10000},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Detect(tt.filePath, tt.data())
			if got != tt.want {
				t.Fatalf("Detect() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestHardworkerFirmwareFixtures(t *testing.T) {
	t.Context()

	applicationPath := "../../testdata/esp32_rx_hardworker_latest.bin"
	application, err := os.ReadFile(applicationPath)
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("local application firmware fixture is not available")
	}
	if err != nil {
		t.Fatalf("read application fixture: %v", err)
	}

	mergedPath := "../../testdata/esp32_rx_hardworker_latest.merged.bin"
	merged, err := os.ReadFile(mergedPath)
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("local merged firmware fixture is not available")
	}
	if err != nil {
		t.Fatalf("read merged fixture: %v", err)
	}

	applicationImage := Detect(applicationPath, application)
	if applicationImage != (Image{Offset: 0x10000}) {
		t.Fatalf("application classification = %+v, want offset 0x10000", applicationImage)
	}
	mergedImage := Detect(mergedPath, merged)
	if mergedImage != (Image{Offset: 0, Full: true}) {
		t.Fatalf("merged classification = %+v, want full image at offset 0", mergedImage)
	}

	applicationEnd := int(applicationImage.Offset) + len(application)
	if applicationEnd > len(merged) {
		t.Fatalf("application range ends at 0x%x, merged image size is 0x%x", applicationEnd, len(merged))
	}
	if !bytes.Equal(application, merged[applicationImage.Offset:applicationEnd]) {
		t.Fatal("application fixture does not match the payload at 0x10000 in the merged fixture")
	}
}

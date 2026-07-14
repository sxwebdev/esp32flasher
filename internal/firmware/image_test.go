package firmware

import "testing"

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

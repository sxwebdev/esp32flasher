// Package firmware classifies firmware images and selects their flash offset.
package firmware

import (
	"path/filepath"
	"strings"
)

// Image describes a firmware image and its placement in Flash.
type Image struct {
	Offset uint32
	Full   bool
}

// Detect distinguishes a complete ESP32 merged image from an application image.
func Detect(filePath string, data []byte) Image {
	name := strings.ToLower(filepath.Base(filePath))
	mergedByName := strings.Contains(name, "merged") && len(data) >= 0x10000
	mergedByLayout := len(data) > 0x8001 &&
		data[0x1000] == 0xe9 &&
		data[0x8000] == 0xaa && data[0x8001] == 0x50

	if mergedByName || mergedByLayout {
		return Image{Offset: 0, Full: true}
	}

	return Image{Offset: 0x10000}
}

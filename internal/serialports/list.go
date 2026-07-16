package serialports

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Port describes a serial port shown in the device picker.
type Port struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type source func() ([]Port, error)

func fromNames(names []string, err error) ([]Port, error) {
	if err != nil {
		return nil, err
	}

	ports := make([]Port, 0, len(names))
	for _, name := range names {
		ports = append(ports, Port{Name: name})
	}
	return ports, nil
}

// discover combines independent OS discovery mechanisms. A failure in one
// source must not hide ports returned by another source.
func discover(sources ...source) ([]Port, error) {
	var (
		allPorts        []Port
		sourceSucceeded bool
		sourceErrors    []error
	)

	for _, source := range sources {
		ports, err := source()
		if err != nil {
			sourceErrors = append(sourceErrors, err)
			continue
		}

		sourceSucceeded = true
		allPorts = append(allPorts, ports...)
	}

	ports := normalize(allPorts)
	if sourceSucceeded {
		return ports, nil
	}

	return nil, errors.Join(sourceErrors...)
}

func normalize(ports []Port) []Port {
	byName := make(map[string]int, len(ports))
	result := make([]Port, 0, len(ports))
	for _, port := range ports {
		port.Name = strings.TrimSpace(port.Name)
		port.Description = cleanDescription(port.Description, port.Name)
		if port.Name == "" {
			continue
		}

		key := strings.ToUpper(port.Name)
		if index, exists := byName[key]; exists {
			if result[index].Description == "" && port.Description != "" {
				result[index].Description = port.Description
			}
			continue
		}

		byName[key] = len(result)
		result = append(result, port)
	}

	sort.Slice(result, func(i, j int) bool {
		leftNumber, leftCOM := comPortNumber(result[i].Name)
		rightNumber, rightCOM := comPortNumber(result[j].Name)
		if leftCOM && rightCOM {
			return leftNumber < rightNumber
		}
		return strings.ToUpper(result[i].Name) < strings.ToUpper(result[j].Name)
	})
	return result
}

func cleanDescription(description, portName string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		return ""
	}

	suffix := " (" + portName + ")"
	if strings.HasSuffix(strings.ToUpper(description), strings.ToUpper(suffix)) {
		description = strings.TrimSpace(description[:len(description)-len(suffix)])
	}
	if strings.EqualFold(description, portName) {
		return ""
	}
	return description
}

func comPortNumber(port string) (int, bool) {
	upper := strings.ToUpper(port)
	if !strings.HasPrefix(upper, "COM") {
		return 0, false
	}

	number, err := strconv.Atoi(upper[3:])
	return number, err == nil
}

func splitMultiString(value []uint16) []string {
	var result []string
	for start := 0; start < len(value); {
		end := start
		for end < len(value) && value[end] != 0 {
			end++
		}
		if end == start {
			break
		}

		result = append(result, string(utf16.Decode(value[start:end])))
		start = end + 1
	}
	return result
}

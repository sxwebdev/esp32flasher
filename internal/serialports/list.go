package serialports

import (
	"errors"
	"sort"
	"strconv"
	"strings"
)

type source func() ([]string, error)

// discover combines independent OS discovery mechanisms. A failure in one
// source must not hide ports returned by another source.
func discover(sources ...source) ([]string, error) {
	var (
		allPorts        []string
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

func normalize(ports []string) []string {
	seen := make(map[string]struct{}, len(ports))
	result := make([]string, 0, len(ports))
	for _, port := range ports {
		port = strings.TrimSpace(port)
		if port == "" {
			continue
		}

		key := strings.ToUpper(port)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, port)
	}

	sort.Slice(result, func(i, j int) bool {
		leftNumber, leftCOM := comPortNumber(result[i])
		rightNumber, rightCOM := comPortNumber(result[j])
		if leftCOM && rightCOM {
			return leftNumber < rightNumber
		}
		return strings.ToUpper(result[i]) < strings.ToUpper(result[j])
	})
	return result
}

func comPortNumber(port string) (int, bool) {
	upper := strings.ToUpper(port)
	if !strings.HasPrefix(upper, "COM") {
		return 0, false
	}

	number, err := strconv.Atoi(upper[3:])
	return number, err == nil
}

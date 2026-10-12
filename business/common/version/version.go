package versionhelper

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const versionPrefix = "v"

func Parse(version string) (v string, err error) {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return "0", errors.New("invalid version")
	}
	for _, part := range parts {
		if _, err := strconv.Atoi(part); err != nil {
			return "", fmt.Errorf("invalid version %q: %w", version, err)
		}
	}
	return versionPrefix + parts[0], nil
}

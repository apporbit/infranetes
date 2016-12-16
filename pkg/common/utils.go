package common

import (
	"fmt"
	"strings"
)

func ParseContainer(c string) (string, string, error) {
	splits := strings.Split(c, ":")
	if len(splits) != 2 {
		return "", "", fmt.Errorf("parseContainer: %v not parsable container name", c)
	}

	return splits[0], splits[1], nil
}

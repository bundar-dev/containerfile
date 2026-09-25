package containerfile

import (
	"slices"
	"strconv"
)

func containsString(list []string, s string) bool { return slices.Contains(list, s) }

func itoa(n int) string { return strconv.Itoa(n) }

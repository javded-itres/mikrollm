package domain

import (
	"fmt"
	"strconv"
)

func FormatContext(n int) string {
	if n <= 0 {
		return ""
	}
	if n >= 1_000_000 && n%1_000_000 == 0 {
		return fmt.Sprintf("%dM", n/1_000_000)
	}
	if n >= 1_048_576 && n%1_048_576 == 0 {
		return fmt.Sprintf("%dM", n/1_048_576)
	}
	if n%1000 == 0 && n >= 1000 {
		return fmt.Sprintf("%dk", n/1000)
	}
	if n%1024 == 0 && n >= 1024 {
		return fmt.Sprintf("%dk", n/1024)
	}
	if n >= 1000 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return strconv.Itoa(n)
}

func (m Model) ContextWindow(detected int) int {
	if m.MaxContext > 0 {
		return m.MaxContext
	}
	return detected
}

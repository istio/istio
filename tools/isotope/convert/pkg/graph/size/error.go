package size

import "fmt"

// NegativeSizeError is returned when parsing a negative size.
type NegativeSizeError struct {
	Size int64
}

func (e NegativeSizeError) Error() string {
	return fmt.Sprintf("%v must be non-negative", e.Size)
}

package pct

import "fmt"

// InvalidPercentageStringError is returned when parsing an invalid percentage
// string.
type InvalidPercentageStringError struct {
	String string
}

func (e InvalidPercentageStringError) Error() string {
	return fmt.Sprintf(
		"invalid percentage as string: %v (must be between \"0%%\" and \"100%%\")",
		e.String)
}

// OutOfRangeError is returned when parsing a percentage that is out of range.
type OutOfRangeError struct {
	Float float64
}

func (e OutOfRangeError) Error() string {
	return fmt.Sprintf(
		"percentage %v is out of range (must be between 0.0 and 1.0)",
		e.Float)
}

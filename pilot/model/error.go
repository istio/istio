package model

import "fmt"

// ItemAlreadyExistsError is a typed error that should be used to identify when an item already is
// present in the configuration registry. To overwrite the default error message set the Msg field.
type ItemAlreadyExistsError struct {
	Key string
	Msg string
}

// Error fulfills the basic Error interface for the ItemAlreadyExistsError
// If a message is set it returns that otherwise it returns a default error including the key
func (e *ItemAlreadyExistsError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return fmt.Sprintf("Item with key %+v already exists", e.Key)
}

// ItemNotFoundError is a typed error that should be used to identify when an item cant be found
// in the configuration registry. To overwrite the default error message set the Msg field.
type ItemNotFoundError struct {
	Key string
	Msg string
}

// Error fulfills the basic Error interface for the ItemNotFoundError
// If a message is set it returns that otherwise it returns a default error including the key
func (e *ItemNotFoundError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return fmt.Sprintf("item with key %+v not found", e.Key)
}

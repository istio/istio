package errors

import (
	"errors"
	"fmt"
)

// New error.  Note returns error rather than *ErrorChain so that it matches errors.New signature
func New(msg string) error {
	return errors.New(msg)
}

// Errorf is fmt.Errorf.  Note returns error rather than *ErrorChain so that it matches fmt.Errorf signature
func Errorf(msg string, args ...interface{}) error {
	return fmt.Errorf(msg, args...)
}

// Annotate adds a new error in the chain that is some extra context about the error
func Annotate(err error, msg string) error {
	if err == nil {
		return nil
	}
	return Wrap(errors.New(msg), err)
}

// Annotatef adds a new formatf error in the chain that is some extra context about the error
func Annotatef(err error, msg string, args ...interface{}) error {
	if err == nil {
		return nil
	}
	return Wrap(fmt.Errorf(msg, args...), err)
}

// Wrap a head function giving it the chain defined in next.  Note that wrap may sometimes return head directly
// if next is nil
func Wrap(head error, next error) error {
	if head == nil {
		// The state head==nil && next!=nil is very strange so I leave it as nil
		return nil
	}
	if next == nil {
		return head
	}
	tail := Tail(next)
	return &ErrorChain{
		head: head,
		next: next,
		tail: tail,
	}
}

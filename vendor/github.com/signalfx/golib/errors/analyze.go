package errors

import (
	"bytes"
)

// Tail of the error chain: at the end
func Tail(err error) error {
	if tail, ok := err.(errLinkedList); ok {
		return tail.Tail()
	}
	if causable, ok := err.(causableError); ok {
		return causable.Cause()
	}
	if hasTail, ok := err.(hasInner); ok {
		i := hasTail.GetInner()
		if i == err || i == nil {
			return err
		}
		return Tail(i)
	}
	return err
}

// Next error just below this one, or nil if there is no next error.  Note this may be an error created
// for you if you used annotations.  As a user, you probably don't want to use this.
func Next(err error) error {
	if tail, ok := err.(errLinkedList); ok {
		return tail.Next()
	}
	if u, ok := err.(hasUnderline); ok {
		return u.Underlying()
	}
	if i, ok := err.(hasInner); ok {
		return i.GetInner()
	}
	return nil
}

// Message is the error string at the Head of the linked list
func Message(err error) string {
	if tail, ok := err.(errLinkedList); ok {
		return tail.Head().Error()
	}
	if hasMsg, ok := err.(hasMessage); ok {
		return hasMsg.Message()
	}
	return err.Error()
}

// Details are an easy to read concat of all the error strings in a chain
func Details(err error) string {
	b := bytes.Buffer{}
	PanicIfErr(b.WriteByte('['), "unexpected")
	first := true
	for ; err != nil; err = Next(err) {
		if !first {
			PanicIfErrWrite(b.WriteString(" | "))
		}
		PanicIfErrWrite(b.WriteString(Message(err)))
		first = false
	}
	PanicIfErr(b.WriteByte(']'), "unexpected")
	return b.String()
}

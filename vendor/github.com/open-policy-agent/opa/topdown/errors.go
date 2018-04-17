// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"fmt"

	"github.com/open-policy-agent/opa/ast"
)

// Error is the error type returned by the Eval and Query functions when
// an evaluation error occurs.
type Error struct {
	Code     string        `json:"code"`
	Message  string        `json:"message"`
	Location *ast.Location `json:"location,omitempty"`
}

const (

	// InternalErr represents an unknown evaluation error.
	InternalErr string = "eval_internal_error"

	// CancelErr indicates the evaluation process was cancelled.
	CancelErr string = "eval_cancel_error"

	// ConflictErr indicates a conflict was encountered during evaluation. For
	// instance, a conflict occurs if a rule produces multiple, differing values
	// for the same key in an object. Conflict errors indicate the policy does
	// not account for the data loaded into the policy engine.
	ConflictErr string = "eval_conflict_error"

	// TypeErr indicates evaluation stopped because an expression was applied to
	// a value of an inappropriate type.
	TypeErr string = "eval_type_error"
)

// IsError returns true if the err is an Error.
func IsError(err error) bool {
	_, ok := err.(*Error)
	return ok
}

// IsCancel returns true if err was caused by cancellation.
func IsCancel(err error) bool {
	if e, ok := err.(*Error); ok {
		return e.Code == CancelErr
	}
	return false
}

func (e *Error) Error() string {

	msg := fmt.Sprintf("%v: %v", e.Code, e.Message)

	if e.Location != nil {
		msg = e.Location.String() + ": " + msg
	}

	return msg
}

func functionConflictErr(loc *ast.Location) error {
	return &Error{
		Code:     ConflictErr,
		Location: loc,
		Message:  "functions must not produce multiple outputs for same inputs",
	}
}

func completeDocConflictErr(loc *ast.Location) error {
	return &Error{
		Code:     ConflictErr,
		Location: loc,
		Message:  "complete rules must not produce multiple outputs",
	}
}

func objectDocKeyConflictErr(loc *ast.Location) error {
	return &Error{
		Code:     ConflictErr,
		Location: loc,
		Message:  "object keys must be unique",
	}
}

func documentConflictErr(loc *ast.Location) error {
	return &Error{
		Code:     ConflictErr,
		Location: loc,
		Message:  "base and virtual document keys must be disjoint",
	}
}

func unsupportedBuiltinErr(loc *ast.Location) error {
	return &Error{
		Code:     InternalErr,
		Location: loc,
		Message:  "unsupported built-in",
	}
}

package install

import "fmt"

const (
	StrategyErrReasonComponentMissing   = "ComponentMissing"
	StrategyErrReasonAnnotationsMissing = "AnnotationsMissing"
	StrategyErrReasonWaiting            = "Waiting"
	StrategyErrReasonInvalidStrategy    = "InvalidStrategy"
	StrategyErrReasonTimeout            = "Timeout"
	StrategyErrReasonUnknown            = "Unknown"
)

// unrecoverableErrors are the set of errors that mean we can't recover an install strategy
var unrecoverableErrors = map[string]struct{}{
	StrategyErrReasonInvalidStrategy: {},
	StrategyErrReasonTimeout:         {},
}

// StrategyError is used to represent error types for install strategies
type StrategyError struct {
	Reason  string
	Message string
}

var _ error = StrategyError{}

// Error implements the Error interface.
func (e StrategyError) Error() string {
	return fmt.Sprintf("%s: %s", e.Reason, e.Message)
}

// IsErrorUnrecoverable reports if a given strategy error is one of the predefined unrecoverable types
func IsErrorUnrecoverable(err error) bool {
	if err == nil {
		return false
	}
	_, ok := unrecoverableErrors[reasonForError(err)]
	return ok
}

func reasonForError(err error) string {
	switch t := err.(type) {
	case StrategyError:
		return t.Reason
	case *StrategyError:
		return t.Reason
	}
	return StrategyErrReasonUnknown
}

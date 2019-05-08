package errors

import "strings"

// MultiErr wraps multiple errors into one error string
type MultiErr struct {
	errs []error
}

func (e *MultiErr) Error() string {
	r := make([]string, 0, len(e.errs))
	for _, err := range e.errs {
		r = append(r, err.Error())
	}
	return strings.Join(r, " | ")
}

var _ error = &MultiErr{}

// NewMultiErr will return nil if there are no valid errors in errs, will return the exact, single error
// if errs only contains a single error, and will otherwise return an instance of MultiErr that wraps all
// the errors at once.
func NewMultiErr(errs []error) error {
	retErrs := make([]error, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			retErrs = append(retErrs, err)
		}
	}
	if len(retErrs) == 0 {
		return nil
	}
	if len(retErrs) == 1 {
		return retErrs[0]
	}
	return &MultiErr{
		errs: retErrs,
	}
}

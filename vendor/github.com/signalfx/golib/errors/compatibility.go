package errors

type causableError interface {
	Cause() error
}

type hasUnderline interface {
	Underlying() error
}

// hasInner is used by dropboxgo
type hasInner interface {
	GetInner() error
}

type hasMessage interface {
	Message() string
}

var _ causableError = &ErrorChain{}
var _ hasUnderline = &ErrorChain{}
var _ hasMessage = &ErrorChain{}

// Cause lets me simulate errgo
func (e *ErrorChain) Cause() error {
	return e.Tail()
}

// Message lets me simulate errgo
func (e *ErrorChain) Message() string {
	return e.Head().Error()
}

// GetMessage is used by dropbox
func (e *ErrorChain) GetMessage() string {
	return e.Head().Error()
}

// GetInner is used by dropbox
func (e *ErrorChain) GetInner() error {
	return e.Head()
}

// Underlying lets me simulate errgo/facebook
func (e *ErrorChain) Underlying() error {
	return e.Next()
}

// Cause of the original error at the end of the chain
func Cause(err error) error {
	return Tail(err)
}

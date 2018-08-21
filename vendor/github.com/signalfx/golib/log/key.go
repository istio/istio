package log

// Key is a type that is expected to be the "key" in structured logging key/value pairs
type Key string

// String returns the wrapped value directly
func (k Key) String() string {
	return string(k)
}

var (
	// Err is the suggested Log() key for errors
	Err = Key("err")
	// Msg is the suggested Log() key for messages
	Msg = Key("msg")
)

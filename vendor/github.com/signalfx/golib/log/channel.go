package log

// ChannelLogger creates a logger that sends log messages to a channel.  It's useful for testing and buffering
// logs.
type ChannelLogger struct {
	Out    chan []interface{}
	Err    chan error
	OnFull Logger
}

// NewChannelLogger creates a ChannelLogger for channels with size buffer
func NewChannelLogger(size int, onFull Logger) *ChannelLogger {
	return &ChannelLogger{
		Out:    make(chan []interface{}, size),
		Err:    make(chan error, size),
		OnFull: onFull,
	}
}

func (c *ChannelLogger) logBlocking(kvs ...interface{}) {
	c.Out <- append(make([]interface{}, 0, len(kvs)), kvs...)
}

func (c *ChannelLogger) logNonBlocking(kvs ...interface{}) {
	select {
	case c.Out <- append(make([]interface{}, 0, len(kvs)), kvs...):
	default:
		c.OnFull.Log(kvs...)
	}
}

// ErrorLogger adds the err to ChannelLogger's buffer and returns itself
func (c *ChannelLogger) ErrorLogger(err error) Logger {
	c.Err <- err
	return c
}

// Log blocks adding kvs to it's buffer.  If OnFull is not nil, it does not block.
func (c *ChannelLogger) Log(kvs ...interface{}) {
	if c.OnFull != nil {
		c.logNonBlocking(kvs...)
		return
	}
	c.logBlocking(kvs...)
}

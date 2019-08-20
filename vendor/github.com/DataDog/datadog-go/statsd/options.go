package statsd

import "time"

var (
	// DefaultNamespace is the default value for the Namespace option
	DefaultNamespace = ""
	// DefaultTags is the default value for the Tags option
	DefaultTags = []string{}
	// DefaultBuffered is the default value for the Buffered option
	DefaultBuffered = false
	// DefaultMaxMessagesPerPayload is the default value for the MaxMessagesPerPayload option
	DefaultMaxMessagesPerPayload = 16
	// DefaultAsyncUDS is the default value for the AsyncUDS option
	DefaultAsyncUDS = false
	// DefaultWriteTimeoutUDS is the default value for the WriteTimeoutUDS option
	DefaultWriteTimeoutUDS = 1 * time.Millisecond
)

// Options contains the configuration options for a client.
type Options struct {
	// Namespace to prepend to all metrics, events and service checks name.
	Namespace string
	// Tags are global tags to be applied to every metrics, events and service checks.
	Tags []string
	// Buffered allows to pack multiple DogStatsD messages in one payload. Messages will be buffered
	// until the total size of the payload exceeds MaxMessagesPerPayload metrics, events and/or service
	// checks or after 100ms since the payload startedto be built.
	Buffered bool
	// MaxMessagesPerPayload is the maximum number of metrics, events and/or service checks a single payload will contain.
	// Note that this option only takes effect when the client is buffered.
	MaxMessagesPerPayload int
	// AsyncUDS allows to switch between async and blocking mode for UDS.
	// Blocking mode allows for error checking but does not guarentee that calls won't block the execution.
	AsyncUDS bool
	// WriteTimeoutUDS is the timeout after which a UDS packet is dropped.
	WriteTimeoutUDS time.Duration
}

func resolveOptions(options []Option) (*Options, error) {
	o := &Options{
		Namespace:             DefaultNamespace,
		Tags:                  DefaultTags,
		Buffered:              DefaultBuffered,
		MaxMessagesPerPayload: DefaultMaxMessagesPerPayload,
		AsyncUDS:              DefaultAsyncUDS,
		WriteTimeoutUDS:       DefaultWriteTimeoutUDS,
	}

	for _, option := range options {
		err := option(o)
		if err != nil {
			return nil, err
		}
	}

	return o, nil
}

// Option is a client option. Can return an error if validation fails.
type Option func(*Options) error

// WithNamespace sets the Namespace option.
func WithNamespace(namespace string) Option {
	return func(o *Options) error {
		o.Namespace = namespace
		return nil
	}
}

// WithTags sets the Tags option.
func WithTags(tags []string) Option {
	return func(o *Options) error {
		o.Tags = tags
		return nil
	}
}

// Buffered sets the Buffered option.
func Buffered() Option {
	return func(o *Options) error {
		o.Buffered = true
		return nil
	}
}

// WithMaxMessagesPerPayload sets the MaxMessagesPerPayload option.
func WithMaxMessagesPerPayload(maxMessagesPerPayload int) Option {
	return func(o *Options) error {
		o.MaxMessagesPerPayload = maxMessagesPerPayload
		return nil
	}
}

// WithAsyncUDS sets the AsyncUDS option.
func WithAsyncUDS() Option {
	return func(o *Options) error {
		o.AsyncUDS = true
		return nil
	}
}

// WithWriteTimeoutUDS sets the WriteTimeoutUDS option.
func WithWriteTimeoutUDS(writeTimeoutUDS time.Duration) Option {
	return func(o *Options) error {
		o.WriteTimeoutUDS = writeTimeoutUDS
		return nil
	}
}

// Copyright 2013 Ooyala, Inc.

/*
Package statsd provides a Go dogstatsd client. Dogstatsd extends the popular statsd,
adding tags and histograms and pushing upstream to Datadog.

Refer to http://docs.datadoghq.com/guides/dogstatsd/ for information about DogStatsD.

Example Usage:

    // Create the client
    c, err := statsd.New("127.0.0.1:8125")
    if err != nil {
        log.Fatal(err)
    }
    // Prefix every metric with the app name
    c.Namespace = "flubber."
    // Send the EC2 availability zone as a tag with every metric
    c.Tags = append(c.Tags, "us-east-1a")
    err = c.Gauge("request.duration", 1.2, nil, 1)

statsd is based on go-statsd-client.
*/
package statsd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"
)

/*
OptimalPayloadSize defines the optimal payload size for a UDP datagram, 1432 bytes
is optimal for regular networks with an MTU of 1500 so datagrams don't get
fragmented. It's generally recommended not to fragment UDP datagrams as losing
a single fragment will cause the entire datagram to be lost.

This can be increased if your network has a greater MTU or you don't mind UDP
datagrams getting fragmented. The practical limit is MaxUDPPayloadSize
*/
const OptimalPayloadSize = 1432

/*
MaxUDPPayloadSize defines the maximum payload size for a UDP datagram.
Its value comes from the calculation: 65535 bytes Max UDP datagram size -
8byte UDP header - 60byte max IP headers
any number greater than that will see frames being cut out.
*/
const MaxUDPPayloadSize = 65467

/*
UnixAddressPrefix holds the prefix to use to enable Unix Domain Socket
traffic instead of UDP.
*/
const UnixAddressPrefix = "unix://"

/*
Stat suffixes
*/
var (
	gaugeSuffix        = []byte("|g")
	countSuffix        = []byte("|c")
	histogramSuffix    = []byte("|h")
	distributionSuffix = []byte("|d")
	decrSuffix         = []byte("-1|c")
	incrSuffix         = []byte("1|c")
	setSuffix          = []byte("|s")
	timingSuffix       = []byte("|ms")
)

// A statsdWriter offers a standard interface regardless of the underlying
// protocol. For now UDS and UPD writers are available.
type statsdWriter interface {
	Write(data []byte) (n int, err error)
	SetWriteTimeout(time.Duration) error
	Close() error
}

// A Client is a handle for sending messages to dogstatsd.  It is safe to
// use one Client from multiple goroutines simultaneously.
type Client struct {
	// Writer handles the underlying networking protocol
	writer statsdWriter
	// Namespace to prepend to all statsd calls
	Namespace string
	// Tags are global tags to be added to every statsd call
	Tags []string
	// skipErrors turns off error passing and allows UDS to emulate UDP behaviour
	SkipErrors bool
	// BufferLength is the length of the buffer in commands.
	bufferLength int
	flushTime    time.Duration
	commands     []string
	buffer       bytes.Buffer
	stop         chan struct{}
	sync.Mutex
}

// New returns a pointer to a new Client given an addr in the format "hostname:port" or
// "unix:///path/to/socket".
func New(addr string) (*Client, error) {
	if strings.HasPrefix(addr, UnixAddressPrefix) {
		w, err := newUdsWriter(addr[len(UnixAddressPrefix)-1:])
		if err != nil {
			return nil, err
		}
		return NewWithWriter(w)
	}
	w, err := newUDPWriter(addr)
	if err != nil {
		return nil, err
	}
	return NewWithWriter(w)
}

// NewWithWriter creates a new Client with given writer. Writer is a
// io.WriteCloser + SetWriteTimeout(time.Duration) error
func NewWithWriter(w statsdWriter) (*Client, error) {
	client := &Client{writer: w, SkipErrors: false}
	return client, nil
}

// NewBuffered returns a Client that buffers its output and sends it in chunks.
// Buflen is the length of the buffer in number of commands.
func NewBuffered(addr string, buflen int) (*Client, error) {
	client, err := New(addr)
	if err != nil {
		return nil, err
	}
	client.bufferLength = buflen
	client.commands = make([]string, 0, buflen)
	client.flushTime = time.Millisecond * 100
	client.stop = make(chan struct{}, 1)
	go client.watch()
	return client, nil
}

// format a message from its name, value, tags and rate.  Also adds global
// namespace and tags.
func (c *Client) format(name string, value interface{}, suffix []byte, tags []string, rate float64) string {
	var buf bytes.Buffer
	if c.Namespace != "" {
		buf.WriteString(c.Namespace)
	}
	buf.WriteString(name)
	buf.WriteString(":")

	switch val := value.(type) {
	case float64:
		buf.Write(strconv.AppendFloat([]byte{}, val, 'f', 6, 64))

	case int64:
		buf.Write(strconv.AppendInt([]byte{}, val, 10))

	case string:
		buf.WriteString(val)

	default:
		// do nothing
	}
	buf.Write(suffix)

	if rate < 1 {
		buf.WriteString(`|@`)
		buf.WriteString(strconv.FormatFloat(rate, 'f', -1, 64))
	}

	writeTagString(&buf, c.Tags, tags)

	return buf.String()
}

// SetWriteTimeout allows the user to set a custom UDS write timeout. Not supported for UDP.
func (c *Client) SetWriteTimeout(d time.Duration) error {
	if c == nil {
		return nil
	}
	return c.writer.SetWriteTimeout(d)
}

func (c *Client) watch() {
	ticker := time.NewTicker(c.flushTime)

	for {
		select {
		case <-ticker.C:
			c.Lock()
			if len(c.commands) > 0 {
				// FIXME: eating error here
				c.flushLocked()
			}
			c.Unlock()
		case <-c.stop:
			ticker.Stop()
			return
		}
	}
}

func (c *Client) append(cmd string) error {
	c.Lock()
	defer c.Unlock()
	c.commands = append(c.commands, cmd)
	// if we should flush, lets do it
	if len(c.commands) == c.bufferLength {
		if err := c.flushLocked(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) joinMaxSize(cmds []string, sep string, maxSize int) ([][]byte, []int) {
	c.buffer.Reset() //clear buffer

	var frames [][]byte
	var ncmds []int
	sepBytes := []byte(sep)
	sepLen := len(sep)

	elem := 0
	for _, cmd := range cmds {
		needed := len(cmd)

		if elem != 0 {
			needed = needed + sepLen
		}

		if c.buffer.Len()+needed <= maxSize {
			if elem != 0 {
				c.buffer.Write(sepBytes)
			}
			c.buffer.WriteString(cmd)
			elem++
		} else {
			frames = append(frames, copyAndResetBuffer(&c.buffer))
			ncmds = append(ncmds, elem)
			// if cmd is bigger than maxSize it will get flushed on next loop
			c.buffer.WriteString(cmd)
			elem = 1
		}
	}

	//add whatever is left! if there's actually something
	if c.buffer.Len() > 0 {
		frames = append(frames, copyAndResetBuffer(&c.buffer))
		ncmds = append(ncmds, elem)
	}

	return frames, ncmds
}

func copyAndResetBuffer(buf *bytes.Buffer) []byte {
	tmpBuf := make([]byte, buf.Len())
	copy(tmpBuf, buf.Bytes())
	buf.Reset()
	return tmpBuf
}

// Flush forces a flush of the pending commands in the buffer
func (c *Client) Flush() error {
	if c == nil {
		return nil
	}
	c.Lock()
	defer c.Unlock()
	return c.flushLocked()
}

// flush the commands in the buffer.  Lock must be held by caller.
func (c *Client) flushLocked() error {
	frames, flushable := c.joinMaxSize(c.commands, "\n", OptimalPayloadSize)
	var err error
	cmdsFlushed := 0
	for i, data := range frames {
		_, e := c.writer.Write(data)
		if e != nil {
			err = e
			break
		}
		cmdsFlushed += flushable[i]
	}

	// clear the slice with a slice op, doesn't realloc
	if cmdsFlushed == len(c.commands) {
		c.commands = c.commands[:0]
	} else {
		//this case will cause a future realloc...
		// drop problematic command though (sorry).
		c.commands = c.commands[cmdsFlushed+1:]
	}
	return err
}

func (c *Client) sendMsg(msg string) error {
	// return an error if message is bigger than MaxUDPPayloadSize
	if len(msg) > MaxUDPPayloadSize {
		return errors.New("message size exceeds MaxUDPPayloadSize")
	}

	// if this client is buffered, then we'll just append this
	if c.bufferLength > 0 {
		return c.append(msg)
	}

	_, err := c.writer.Write([]byte(msg))

	if c.SkipErrors {
		return nil
	}
	return err
}

// send handles sampling and sends the message over UDP. It also adds global namespace prefixes and tags.
func (c *Client) send(name string, value interface{}, suffix []byte, tags []string, rate float64) error {
	if c == nil {
		return nil
	}
	if rate < 1 && rand.Float64() > rate {
		return nil
	}
	data := c.format(name, value, suffix, tags, rate)
	return c.sendMsg(data)
}

// Gauge measures the value of a metric at a particular time.
func (c *Client) Gauge(name string, value float64, tags []string, rate float64) error {
	return c.send(name, value, gaugeSuffix, tags, rate)
}

// Count tracks how many times something happened per second.
func (c *Client) Count(name string, value int64, tags []string, rate float64) error {
	return c.send(name, value, countSuffix, tags, rate)
}

// Histogram tracks the statistical distribution of a set of values on each host.
func (c *Client) Histogram(name string, value float64, tags []string, rate float64) error {
	return c.send(name, value, histogramSuffix, tags, rate)
}

// Distribution tracks the statistical distribution of a set of values across your infrastructure.
func (c *Client) Distribution(name string, value float64, tags []string, rate float64) error {
	return c.send(name, value, distributionSuffix, tags, rate)
}

// Decr is just Count of -1
func (c *Client) Decr(name string, tags []string, rate float64) error {
	return c.send(name, nil, decrSuffix, tags, rate)
}

// Incr is just Count of 1
func (c *Client) Incr(name string, tags []string, rate float64) error {
	return c.send(name, nil, incrSuffix, tags, rate)
}

// Set counts the number of unique elements in a group.
func (c *Client) Set(name string, value string, tags []string, rate float64) error {
	return c.send(name, value, setSuffix, tags, rate)
}

// Timing sends timing information, it is an alias for TimeInMilliseconds
func (c *Client) Timing(name string, value time.Duration, tags []string, rate float64) error {
	return c.TimeInMilliseconds(name, value.Seconds()*1000, tags, rate)
}

// TimeInMilliseconds sends timing information in milliseconds.
// It is flushed by statsd with percentiles, mean and other info (https://github.com/etsy/statsd/blob/master/docs/metric_types.md#timing)
func (c *Client) TimeInMilliseconds(name string, value float64, tags []string, rate float64) error {
	return c.send(name, value, timingSuffix, tags, rate)
}

// Event sends the provided Event.
func (c *Client) Event(e *Event) error {
	if c == nil {
		return nil
	}
	stat, err := e.Encode(c.Tags...)
	if err != nil {
		return err
	}
	return c.sendMsg(stat)
}

// SimpleEvent sends an event with the provided title and text.
func (c *Client) SimpleEvent(title, text string) error {
	e := NewEvent(title, text)
	return c.Event(e)
}

// ServiceCheck sends the provided ServiceCheck.
func (c *Client) ServiceCheck(sc *ServiceCheck) error {
	if c == nil {
		return nil
	}
	stat, err := sc.Encode(c.Tags...)
	if err != nil {
		return err
	}
	return c.sendMsg(stat)
}

// SimpleServiceCheck sends an serviceCheck with the provided name and status.
func (c *Client) SimpleServiceCheck(name string, status ServiceCheckStatus) error {
	sc := NewServiceCheck(name, status)
	return c.ServiceCheck(sc)
}

// Close the client connection.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	select {
	case c.stop <- struct{}{}:
	default:
	}

	// if this client is buffered, flush before closing the writer
	if c.bufferLength > 0 {
		if err := c.Flush(); err != nil {
			return err
		}
	}

	return c.writer.Close()
}

// Events support
// EventAlertType and EventAlertPriority became exported types after this issue was submitted: https://github.com/DataDog/datadog-go/issues/41
// The reason why they got exported is so that client code can directly use the types.

// EventAlertType is the alert type for events
type EventAlertType string

const (
	// Info is the "info" AlertType for events
	Info EventAlertType = "info"
	// Error is the "error" AlertType for events
	Error EventAlertType = "error"
	// Warning is the "warning" AlertType for events
	Warning EventAlertType = "warning"
	// Success is the "success" AlertType for events
	Success EventAlertType = "success"
)

// EventPriority is the event priority for events
type EventPriority string

const (
	// Normal is the "normal" Priority for events
	Normal EventPriority = "normal"
	// Low is the "low" Priority for events
	Low EventPriority = "low"
)

// An Event is an object that can be posted to your DataDog event stream.
type Event struct {
	// Title of the event.  Required.
	Title string
	// Text is the description of the event.  Required.
	Text string
	// Timestamp is a timestamp for the event.  If not provided, the dogstatsd
	// server will set this to the current time.
	Timestamp time.Time
	// Hostname for the event.
	Hostname string
	// AggregationKey groups this event with others of the same key.
	AggregationKey string
	// Priority of the event.  Can be statsd.Low or statsd.Normal.
	Priority EventPriority
	// SourceTypeName is a source type for the event.
	SourceTypeName string
	// AlertType can be statsd.Info, statsd.Error, statsd.Warning, or statsd.Success.
	// If absent, the default value applied by the dogstatsd server is Info.
	AlertType EventAlertType
	// Tags for the event.
	Tags []string
}

// NewEvent creates a new event with the given title and text.  Error checking
// against these values is done at send-time, or upon running e.Check.
func NewEvent(title, text string) *Event {
	return &Event{
		Title: title,
		Text:  text,
	}
}

// Check verifies that an event is valid.
func (e Event) Check() error {
	if len(e.Title) == 0 {
		return fmt.Errorf("statsd.Event title is required")
	}
	if len(e.Text) == 0 {
		return fmt.Errorf("statsd.Event text is required")
	}
	return nil
}

// Encode returns the dogstatsd wire protocol representation for an event.
// Tags may be passed which will be added to the encoded output but not to
// the Event's list of tags, eg. for default tags.
func (e Event) Encode(tags ...string) (string, error) {
	err := e.Check()
	if err != nil {
		return "", err
	}
	text := e.escapedText()

	var buffer bytes.Buffer
	buffer.WriteString("_e{")
	buffer.WriteString(strconv.FormatInt(int64(len(e.Title)), 10))
	buffer.WriteRune(',')
	buffer.WriteString(strconv.FormatInt(int64(len(text)), 10))
	buffer.WriteString("}:")
	buffer.WriteString(e.Title)
	buffer.WriteRune('|')
	buffer.WriteString(text)

	if !e.Timestamp.IsZero() {
		buffer.WriteString("|d:")
		buffer.WriteString(strconv.FormatInt(int64(e.Timestamp.Unix()), 10))
	}

	if len(e.Hostname) != 0 {
		buffer.WriteString("|h:")
		buffer.WriteString(e.Hostname)
	}

	if len(e.AggregationKey) != 0 {
		buffer.WriteString("|k:")
		buffer.WriteString(e.AggregationKey)

	}

	if len(e.Priority) != 0 {
		buffer.WriteString("|p:")
		buffer.WriteString(string(e.Priority))
	}

	if len(e.SourceTypeName) != 0 {
		buffer.WriteString("|s:")
		buffer.WriteString(e.SourceTypeName)
	}

	if len(e.AlertType) != 0 {
		buffer.WriteString("|t:")
		buffer.WriteString(string(e.AlertType))
	}

	writeTagString(&buffer, tags, e.Tags)

	return buffer.String(), nil
}

// ServiceCheckStatus support
type ServiceCheckStatus byte

const (
	// Ok is the "ok" ServiceCheck status
	Ok ServiceCheckStatus = 0
	// Warn is the "warning" ServiceCheck status
	Warn ServiceCheckStatus = 1
	// Critical is the "critical" ServiceCheck status
	Critical ServiceCheckStatus = 2
	// Unknown is the "unknown" ServiceCheck status
	Unknown ServiceCheckStatus = 3
)

// An ServiceCheck is an object that contains status of DataDog service check.
type ServiceCheck struct {
	// Name of the service check.  Required.
	Name string
	// Status of service check.  Required.
	Status ServiceCheckStatus
	// Timestamp is a timestamp for the serviceCheck.  If not provided, the dogstatsd
	// server will set this to the current time.
	Timestamp time.Time
	// Hostname for the serviceCheck.
	Hostname string
	// A message describing the current state of the serviceCheck.
	Message string
	// Tags for the serviceCheck.
	Tags []string
}

// NewServiceCheck creates a new serviceCheck with the given name and status.  Error checking
// against these values is done at send-time, or upon running sc.Check.
func NewServiceCheck(name string, status ServiceCheckStatus) *ServiceCheck {
	return &ServiceCheck{
		Name:   name,
		Status: status,
	}
}

// Check verifies that an event is valid.
func (sc ServiceCheck) Check() error {
	if len(sc.Name) == 0 {
		return fmt.Errorf("statsd.ServiceCheck name is required")
	}
	if byte(sc.Status) < 0 || byte(sc.Status) > 3 {
		return fmt.Errorf("statsd.ServiceCheck status has invalid value")
	}
	return nil
}

// Encode returns the dogstatsd wire protocol representation for an serviceCheck.
// Tags may be passed which will be added to the encoded output but not to
// the Event's list of tags, eg. for default tags.
func (sc ServiceCheck) Encode(tags ...string) (string, error) {
	err := sc.Check()
	if err != nil {
		return "", err
	}
	message := sc.escapedMessage()

	var buffer bytes.Buffer
	buffer.WriteString("_sc|")
	buffer.WriteString(sc.Name)
	buffer.WriteRune('|')
	buffer.WriteString(strconv.FormatInt(int64(sc.Status), 10))

	if !sc.Timestamp.IsZero() {
		buffer.WriteString("|d:")
		buffer.WriteString(strconv.FormatInt(int64(sc.Timestamp.Unix()), 10))
	}

	if len(sc.Hostname) != 0 {
		buffer.WriteString("|h:")
		buffer.WriteString(sc.Hostname)
	}

	writeTagString(&buffer, tags, sc.Tags)

	if len(message) != 0 {
		buffer.WriteString("|m:")
		buffer.WriteString(message)
	}

	return buffer.String(), nil
}

func (e Event) escapedText() string {
	return strings.Replace(e.Text, "\n", "\\n", -1)
}

func (sc ServiceCheck) escapedMessage() string {
	msg := strings.Replace(sc.Message, "\n", "\\n", -1)
	return strings.Replace(msg, "m:", `m\:`, -1)
}

func removeNewlines(str string) string {
	return strings.Replace(str, "\n", "", -1)
}

func writeTagString(w io.Writer, tagList1, tagList2 []string) {
	// the tag lists may be shared with other callers, so we cannot modify
	// them in any way (which means we cannot append to them either)
	// therefore we must make an entirely separate copy just for this call
	totalLen := len(tagList1) + len(tagList2)
	if totalLen == 0 {
		return
	}
	tags := make([]string, 0, totalLen)
	tags = append(tags, tagList1...)
	tags = append(tags, tagList2...)

	io.WriteString(w, "|#")
	io.WriteString(w, removeNewlines(tags[0]))
	for _, tag := range tags[1:] {
		io.WriteString(w, ",")
		io.WriteString(w, removeNewlines(tag))
	}
}

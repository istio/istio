package statsd

import (
	"fmt"
	"net"
	"time"
)

// asyncUdsWriter is an internal class wrapping around management of UDS connection
type asyncUdsWriter struct {
	// Address to send metrics to, needed to allow reconnection on error
	addr net.Addr
	// Established connection object, or nil if not connected yet
	conn net.Conn
	// write timeout
	writeTimeout time.Duration
	// datagramQueue is the queue of datagrams ready to be sent
	datagramQueue chan []byte
	stopChan      chan struct{}
}

// New returns a pointer to a new asyncUdsWriter given a socket file path as addr.
func newAsyncUdsWriter(addr string) (*asyncUdsWriter, error) {
	udsAddr, err := net.ResolveUnixAddr("unixgram", addr)
	if err != nil {
		return nil, err
	}

	writer := &asyncUdsWriter{
		addr:         udsAddr,
		conn:         nil,
		writeTimeout: defaultUDSTimeout,
		// 8192 * 8KB = 65.5MB
		datagramQueue: make(chan []byte, 8192),
		stopChan:      make(chan struct{}, 1),
	}

	go writer.sendLoop()
	return writer, nil
}

func (w *asyncUdsWriter) sendLoop() {
	for {
		select {
		case datagram := <-w.datagramQueue:
			w.write(datagram)
		case <-w.stopChan:
			return
		}
	}
}

// SetWriteTimeout allows the user to set a custom write timeout
func (w *asyncUdsWriter) SetWriteTimeout(d time.Duration) error {
	w.writeTimeout = d
	return nil
}

// Write data to the UDS connection with write timeout and minimal error handling:
// create the connection if nil, and destroy it if the statsd server has disconnected
func (w *asyncUdsWriter) Write(data []byte) (int, error) {
	select {
	case w.datagramQueue <- data:
		return len(data), nil
	default:
		return 0, fmt.Errorf("uds datagram queue is full (the agent might not be able to keep up)")
	}
}

// write writes the given data to the UDS.
// This function is **not** thread safe.
func (w *asyncUdsWriter) write(data []byte) (int, error) {
	conn, err := w.ensureConnection()
	if err != nil {
		return 0, err
	}

	conn.SetWriteDeadline(time.Now().Add(w.writeTimeout))
	n, err := conn.Write(data)

	if e, isNetworkErr := err.(net.Error); !isNetworkErr || !e.Temporary() {
		// err is not temporary, Statsd server disconnected, retry connecting at next packet
		w.unsetConnection()
		return 0, e
	}

	return n, err
}

func (w *asyncUdsWriter) Close() error {
	close(w.stopChan)
	if w.conn != nil {
		return w.conn.Close()
	}
	return nil
}

func (w *asyncUdsWriter) ensureConnection() (net.Conn, error) {
	if w.conn != nil {
		return w.conn, nil
	}

	newConn, err := net.Dial(w.addr.Network(), w.addr.String())
	if err != nil {
		return nil, err
	}
	w.conn = newConn
	return newConn, nil
}

func (w *asyncUdsWriter) unsetConnection() {
	w.conn = nil
}

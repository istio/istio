/*
 * Copyright 2018 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package test

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

type listenerWrapper struct {
	net.Listener
	mu  sync.Mutex
	rcw *rawConnWrapper
}

func listenWithConnControl(network, address string) (net.Listener, error) {
	l, err := net.Listen(network, address)
	if err != nil {
		return nil, err
	}
	return &listenerWrapper{Listener: l}, nil
}

// Accept blocks until Dial is called, then returns a net.Conn for the server
// half of the connection.
func (l *listenerWrapper) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	l.rcw = newRawConnWrapperFromConn(c)
	l.mu.Unlock()
	return c, nil
}

func (l *listenerWrapper) getLastConn() *rawConnWrapper {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rcw
}

type dialerWrapper struct {
	c   net.Conn
	rcw *rawConnWrapper
}

func (d *dialerWrapper) dialer(target string, t time.Duration) (net.Conn, error) {
	c, err := net.DialTimeout("tcp", target, t)
	d.c = c
	d.rcw = newRawConnWrapperFromConn(c)
	return c, err
}

func (d *dialerWrapper) getRawConnWrapper() *rawConnWrapper {
	return d.rcw
}

type rawConnWrapper struct {
	cc io.ReadWriteCloser
	fr *http2.Framer

	// writing headers:
	headerBuf bytes.Buffer
	hpackEnc  *hpack.Encoder

	// reading frames:
	frc       chan http2.Frame
	frErrc    chan error
	readTimer *time.Timer
}

func newRawConnWrapperFromConn(cc io.ReadWriteCloser) *rawConnWrapper {
	rcw := &rawConnWrapper{
		cc:     cc,
		frc:    make(chan http2.Frame, 1),
		frErrc: make(chan error, 1),
	}
	rcw.hpackEnc = hpack.NewEncoder(&rcw.headerBuf)
	rcw.fr = http2.NewFramer(cc, cc)
	rcw.fr.ReadMetaHeaders = hpack.NewDecoder(4096 /*initialHeaderTableSize*/, nil)

	return rcw
}

func (rcw *rawConnWrapper) Close() error {
	return rcw.cc.Close()
}

func (rcw *rawConnWrapper) readFrame() (http2.Frame, error) {
	go func() {
		fr, err := rcw.fr.ReadFrame()
		if err != nil {
			rcw.frErrc <- err
		} else {
			rcw.frc <- fr
		}
	}()
	t := time.NewTimer(2 * time.Second)
	defer t.Stop()
	select {
	case f := <-rcw.frc:
		return f, nil
	case err := <-rcw.frErrc:
		return nil, err
	case <-t.C:
		return nil, fmt.Errorf("timeout waiting for frame")
	}
}

// greet initiates the client's HTTP/2 connection into a state where
// frames may be sent.
func (rcw *rawConnWrapper) greet() error {
	rcw.writePreface()
	rcw.writeInitialSettings()
	rcw.wantSettings()
	rcw.writeSettingsAck()
	for {
		f, err := rcw.readFrame()
		if err != nil {
			return err
		}
		switch f := f.(type) {
		case *http2.WindowUpdateFrame:
			// grpc's transport/http2_server sends this
			// before the settings ack. The Go http2
			// server uses a setting instead.
		case *http2.SettingsFrame:
			if f.IsAck() {
				return nil
			}
			return fmt.Errorf("during greet, got non-ACK settings frame")
		default:
			return fmt.Errorf("during greet, unexpected frame type %T", f)
		}
	}
}

func (rcw *rawConnWrapper) writePreface() error {
	n, err := rcw.cc.Write([]byte(http2.ClientPreface))
	if err != nil {
		return fmt.Errorf("error writing client preface: %v", err)
	}
	if n != len(http2.ClientPreface) {
		return fmt.Errorf("writing client preface, wrote %d bytes; want %d", n, len(http2.ClientPreface))
	}
	return nil
}

func (rcw *rawConnWrapper) writeInitialSettings() error {
	if err := rcw.fr.WriteSettings(); err != nil {
		return fmt.Errorf("error writing initial SETTINGS frame from client to server: %v", err)
	}
	return nil
}

func (rcw *rawConnWrapper) writeSettingsAck() error {
	if err := rcw.fr.WriteSettingsAck(); err != nil {
		return fmt.Errorf("error writing ACK of server's SETTINGS: %v", err)
	}
	return nil
}

func (rcw *rawConnWrapper) wantSettings() (*http2.SettingsFrame, error) {
	f, err := rcw.readFrame()
	if err != nil {
		return nil, fmt.Errorf("error while expecting a SETTINGS frame: %v", err)
	}
	sf, ok := f.(*http2.SettingsFrame)
	if !ok {
		return nil, fmt.Errorf("got a %T; want *SettingsFrame", f)
	}
	return sf, nil
}

func (rcw *rawConnWrapper) wantSettingsAck() error {
	f, err := rcw.readFrame()
	if err != nil {
		return err
	}
	sf, ok := f.(*http2.SettingsFrame)
	if !ok {
		return fmt.Errorf("wanting a settings ACK, received a %T", f)
	}
	if !sf.IsAck() {
		return fmt.Errorf("settings Frame didn't have ACK set")
	}
	return nil
}

// wait for any activity from the server
func (rcw *rawConnWrapper) wantAnyFrame() (http2.Frame, error) {
	f, err := rcw.fr.ReadFrame()
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (rcw *rawConnWrapper) encodeHeaderField(k, v string) error {
	err := rcw.hpackEnc.WriteField(hpack.HeaderField{Name: k, Value: v})
	if err != nil {
		return fmt.Errorf("HPACK encoding error for %q/%q: %v", k, v, err)
	}
	return nil
}

// encodeRawHeader is for usage on both client and server side to construct header based on the input
// key, value pairs.
func (rcw *rawConnWrapper) encodeRawHeader(headers ...string) []byte {
	if len(headers)%2 == 1 {
		panic("odd number of kv args")
	}

	rcw.headerBuf.Reset()

	pseudoCount := map[string]int{}
	var keys []string
	vals := map[string][]string{}

	for len(headers) > 0 {
		k, v := headers[0], headers[1]
		headers = headers[2:]
		if _, ok := vals[k]; !ok {
			keys = append(keys, k)
		}
		if strings.HasPrefix(k, ":") {
			pseudoCount[k]++
			if pseudoCount[k] == 1 {
				vals[k] = []string{v}
			} else {
				// Allows testing of invalid headers w/ dup pseudo fields.
				vals[k] = append(vals[k], v)
			}
		} else {
			vals[k] = append(vals[k], v)
		}
	}
	for _, k := range keys {
		for _, v := range vals[k] {
			rcw.encodeHeaderField(k, v)
		}
	}
	return rcw.headerBuf.Bytes()
}

// encodeHeader is for usage on client side to write request header.
//
// encodeHeader encodes headers and returns their HPACK bytes. headers
// must contain an even number of key/value pairs.  There may be
// multiple pairs for keys (e.g. "cookie").  The :method, :path, and
// :scheme headers default to GET, / and https.
func (rcw *rawConnWrapper) encodeHeader(headers ...string) []byte {
	if len(headers)%2 == 1 {
		panic("odd number of kv args")
	}

	rcw.headerBuf.Reset()

	if len(headers) == 0 {
		// Fast path, mostly for benchmarks, so test code doesn't pollute
		// profiles when we're looking to improve server allocations.
		rcw.encodeHeaderField(":method", "GET")
		rcw.encodeHeaderField(":path", "/")
		rcw.encodeHeaderField(":scheme", "https")
		return rcw.headerBuf.Bytes()
	}

	if len(headers) == 2 && headers[0] == ":method" {
		// Another fast path for benchmarks.
		rcw.encodeHeaderField(":method", headers[1])
		rcw.encodeHeaderField(":path", "/")
		rcw.encodeHeaderField(":scheme", "https")
		return rcw.headerBuf.Bytes()
	}

	pseudoCount := map[string]int{}
	keys := []string{":method", ":path", ":scheme"}
	vals := map[string][]string{
		":method": {"GET"},
		":path":   {"/"},
		":scheme": {"https"},
	}
	for len(headers) > 0 {
		k, v := headers[0], headers[1]
		headers = headers[2:]
		if _, ok := vals[k]; !ok {
			keys = append(keys, k)
		}
		if strings.HasPrefix(k, ":") {
			pseudoCount[k]++
			if pseudoCount[k] == 1 {
				vals[k] = []string{v}
			} else {
				// Allows testing of invalid headers w/ dup pseudo fields.
				vals[k] = append(vals[k], v)
			}
		} else {
			vals[k] = append(vals[k], v)
		}
	}
	for _, k := range keys {
		for _, v := range vals[k] {
			rcw.encodeHeaderField(k, v)
		}
	}
	return rcw.headerBuf.Bytes()
}

// writeHeadersGRPC is for usage on client side to write request header.
func (rcw *rawConnWrapper) writeHeadersGRPC(streamID uint32, path string) {
	rcw.writeHeaders(http2.HeadersFrameParam{
		StreamID: streamID,
		BlockFragment: rcw.encodeHeader(
			":method", "POST",
			":path", path,
			"content-type", "application/grpc",
			"te", "trailers",
		),
		EndStream:  false,
		EndHeaders: true,
	})
}

func (rcw *rawConnWrapper) writeHeaders(p http2.HeadersFrameParam) error {
	if err := rcw.fr.WriteHeaders(p); err != nil {
		return fmt.Errorf("error writing HEADERS: %v", err)
	}
	return nil
}

func (rcw *rawConnWrapper) writeData(streamID uint32, endStream bool, data []byte) error {
	if err := rcw.fr.WriteData(streamID, endStream, data); err != nil {
		return fmt.Errorf("error writing DATA: %v", err)
	}
	return nil
}

func (rcw *rawConnWrapper) writeRSTStream(streamID uint32, code http2.ErrCode) error {
	if err := rcw.fr.WriteRSTStream(streamID, code); err != nil {
		return fmt.Errorf("error writing RST_STREAM: %v", err)
	}
	return nil
}

func (rcw *rawConnWrapper) writeDataPadded(streamID uint32, endStream bool, data, padding []byte) error {
	if err := rcw.fr.WriteDataPadded(streamID, endStream, data, padding); err != nil {
		return fmt.Errorf("error writing DATA with padding: %v", err)
	}
	return nil
}

func (rcw *rawConnWrapper) writeGoAway(maxStreamID uint32, code http2.ErrCode, debugData []byte) error {
	if err := rcw.fr.WriteGoAway(maxStreamID, code, debugData); err != nil {
		return fmt.Errorf("error writing GoAway: %v", err)
	}
	return nil
}

func (rcw *rawConnWrapper) writeRawFrame(t http2.FrameType, flags http2.Flags, streamID uint32, payload []byte) error {
	if err := rcw.fr.WriteRawFrame(t, flags, streamID, payload); err != nil {
		return fmt.Errorf("error writing Raw Frame: %v", err)
	}
	return nil
}

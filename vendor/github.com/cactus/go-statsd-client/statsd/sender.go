// Copyright (c) 2012-2016 Eli Janssen
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package statsd

import (
	"errors"
	"net"
)

// The Sender interface wraps a Send and Close
type Sender interface {
	Send(data []byte) (int, error)
	Close() error
}

// SimpleSender provides a socket send interface.
type SimpleSender struct {
	// underlying connection
	c net.PacketConn
	// resolved udp address
	ra *net.UDPAddr
}

// Send sends the data to the server endpoint.
func (s *SimpleSender) Send(data []byte) (int, error) {
	// no need for locking here, as the underlying fdNet
	// already serialized writes
	n, err := s.c.(*net.UDPConn).WriteToUDP(data, s.ra)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return n, errors.New("Wrote no bytes")
	}
	return n, nil
}

// Close closes the SimpleSender
func (s *SimpleSender) Close() error {
	err := s.c.Close()
	return err
}

// NewSimpleSender returns a new SimpleSender for sending to the supplied
// addresss.
//
// addr is a string of the format "hostname:port", and must be parsable by
// net.ResolveUDPAddr.
func NewSimpleSender(addr string) (Sender, error) {
	c, err := net.ListenPacket("udp", ":0")
	if err != nil {
		return nil, err
	}

	ra, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		c.Close()
		return nil, err
	}

	sender := &SimpleSender{
		c:  c,
		ra: ra,
	}

	return sender, nil
}

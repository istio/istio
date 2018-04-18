// Package httpunix provides a HTTP transport (net/http.RoundTripper)
// that uses Unix domain sockets instead of HTTP.
//
// This is useful for non-browser connections within the same host, as
// it allows using the file system for credentials of both client
// and server, and guaranteeing unique names.
//
// The URLs look like this:
//
//     http+unix://LOCATION/PATH_ETC
//
// where LOCATION is translated to a file system path with
// Transport.RegisterLocation, and PATH_ETC follow normal http: scheme
// conventions.
package httpunix

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

// Scheme is the URL scheme used for HTTP over UNIX domain sockets.
const Scheme = "http+unix"

// Transport is a http.RoundTripper that connects to Unix domain
// sockets.
type Transport struct {
	DialTimeout           time.Duration
	RequestTimeout        time.Duration
	ResponseHeaderTimeout time.Duration

	mu sync.Mutex
	// map a URL "hostname" to a UNIX domain socket path
	loc map[string]string
}

// RegisterLocation registers an URL location and maps it to the given
// file system path.
//
// Calling RegisterLocation twice for the same location is a
// programmer error, and causes a panic.
func (t *Transport) RegisterLocation(loc string, path string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.loc == nil {
		t.loc = make(map[string]string)
	}
	if _, exists := t.loc[loc]; exists {
		panic("location " + loc + " already registered")
	}
	t.loc[loc] = path
}

var _ http.RoundTripper = (*Transport)(nil)

// RoundTrip executes a single HTTP transaction. See
// net/http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil {
		return nil, errors.New("http+unix: nil Request.URL")
	}
	if req.URL.Scheme != Scheme {
		return nil, errors.New("unsupported protocol scheme: " + req.URL.Scheme)
	}
	if req.URL.Host == "" {
		return nil, errors.New("http+unix: no Host in request URL")
	}
	t.mu.Lock()
	path, ok := t.loc[req.URL.Host]
	t.mu.Unlock()
	if !ok {
		return nil, errors.New("unknown location: " + req.Host)
	}

	c, err := net.DialTimeout("unix", path, t.DialTimeout)
	if err != nil {
		return nil, err
	}
	r := bufio.NewReader(c)
	if t.RequestTimeout > 0 {
		c.SetWriteDeadline(time.Now().Add(t.RequestTimeout))
	}
	if err := req.Write(c); err != nil {
		return nil, err
	}
	if t.ResponseHeaderTimeout > 0 {
		c.SetReadDeadline(time.Now().Add(t.ResponseHeaderTimeout))
	}
	resp, err := http.ReadResponse(r, req)
	return resp, err
}

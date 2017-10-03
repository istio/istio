// Copyright 2017 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package fortio

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// Fetcher is the Url content fetcher that the different client implements.
type Fetcher interface {
	// Fetch returns http code, data, offset of body (for client which returns
	// headers)
	Fetch() (int, []byte, int)
}

var (
	// ExtraHeaders to be added to each request.
	extraHeaders http.Header
	// Host is treated specially, remember that one separately.
	hostOverride string
	// BufferSizeKb size of the buffer (max data) for optimized client in kilobytes defaults to 32k.
	BufferSizeKb = 32
	// CheckConnectionClosedHeader indicates whether to check for server side connection closed headers.
	CheckConnectionClosedHeader = false
	// case doesn't matter for those 3
	contentLengthHeader   = []byte("\r\ncontent-length:")
	connectionCloseHeader = []byte("\r\nconnection: close")
	chunkedHeader         = []byte("\r\nTransfer-Encoding: chunked")
)

func init() {
	extraHeaders = make(http.Header)
	extraHeaders.Add("User-Agent", userAgent)
}

// Version is the fortio package version (TODO:auto gen/extract).
const (
	Version       = "0.2.7"
	userAgent     = "istio/fortio-" + Version
	retcodeOffset = len("HTTP/1.X ")
)

// AddAndValidateExtraHeader collects extra headers (see main.go for example).
func AddAndValidateExtraHeader(h string) error {
	s := strings.SplitN(h, ":", 2)
	if len(s) != 2 {
		return fmt.Errorf("invalid extra header '%s', expecting Key: Value", h)
	}
	key := strings.TrimSpace(s[0])
	value := strings.TrimSpace(s[1])
	if strings.EqualFold(key, "host") {
		Infof("Will be setting special Host header to %s", value)
		hostOverride = value
	} else {
		Infof("Setting regular extra header %s: %s", key, value)
		extraHeaders.Add(key, value)
	}
	return nil
}

// newHttpRequest makes a new http GET request for url with User-Agent.
func newHTTPRequest(url string) *http.Request {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		Errf("Unable to make request for %s : %v", url, err)
		return nil
	}
	req.Header = extraHeaders
	if hostOverride != "" {
		req.Host = hostOverride
	}
	if !Log(Debug) {
		return req
	}
	bytes, err := httputil.DumpRequestOut(req, false)
	if err != nil {
		Errf("Unable to dump request %v", err)
	} else {
		Debugf("For URL %s, sending:\n%s", url, bytes)
	}
	return req
}

// Client object for making repeated requests of the same URL using the same
// http client (net/http)
type Client struct {
	url    string
	req    *http.Request
	client *http.Client
}

// FetchURL fetches URL content and does error handling/logging.
// Version not reusing the client.
func FetchURL(url string) (int, []byte, int) {
	client := NewStdClient(url, 1, true)
	if client == nil {
		return http.StatusBadRequest, []byte("bad url"), 0
	}
	return client.Fetch()
}

// Fetch fetches the byte and code for pre created client
func (c *Client) Fetch() (int, []byte, int) {
	resp, err := c.client.Do(c.req)
	if err != nil {
		Errf("Unable to send request for %s : %v", c.url, err)
		return http.StatusBadRequest, []byte(err.Error()), 0
	}
	var data []byte
	if Log(Debug) {
		if data, err = httputil.DumpResponse(resp, false); err != nil {
			Errf("Unable to dump response %v", err)
		} else {
			Debugf("For URL %s, received:\n%s", c.url, data)
		}
	}
	data, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close() //nolint(errcheck)
	if err != nil {
		Errf("Unable to read response for %s : %v", c.url, err)
		code := resp.StatusCode
		if code == http.StatusOK {
			code = http.StatusNoContent
			Warnf("Ok code despite read error, switching code to %d", code)
		}
		return code, data, 0
	}
	code := resp.StatusCode
	Debugf("Got %d : %s for %s - response is %d bytes", code, resp.Status, c.url, len(data))
	return code, data, 0
}

// NewStdClient creates a client object that wraps the net/http standard client.
func NewStdClient(url string, numConnections int, compression bool) Fetcher {
	req := newHTTPRequest(url)
	if req == nil {
		return nil
	}
	client := Client{
		url,
		req,
		&http.Client{
			Timeout: 3 * time.Second, // TODO: make configurable
			Transport: &http.Transport{
				MaxIdleConns:        numConnections,
				MaxIdleConnsPerHost: numConnections,
				DisableCompression:  !compression,
				Dial: (&net.Dialer{
					Timeout: 4 * time.Second,
				}).Dial,
				TLSHandshakeTimeout: 4 * time.Second,
			},
			// Lets us see the raw response instead of auto following redirects.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	return &client
}

// BasicClient is a fast, lockfree single purpose http 1.0/1.1 client.
type BasicClient struct {
	buffer       []byte
	req          []byte
	dest         net.TCPAddr
	socket       *net.TCPConn
	size         int
	code         int
	errorCount   int
	headerLen    int
	url          string
	host         string
	hostname     string
	port         string
	http10       bool // http 1.0, simplest: no Host, forced no keepAlive, no parsing
	keepAlive    bool
	parseHeaders bool // don't bother in http/1.0
}

// NewBasicClient makes a basic, efficient http 1.0/1.1 client.
// This function itself doesn't need to be super efficient as it is created at
// the beginning and then reused many times.
func NewBasicClient(urlStr string, proto string, keepAlive bool) Fetcher {
	// Parse the url, extract components.
	url, err := url.Parse(urlStr)
	if err != nil {
		Errf("Bad url '%s' : %v", urlStr, err)
		return nil
	}
	if url.Scheme != "http" {
		Errf("Only http is supported, can't use url %s", urlStr)
		return nil
	}
	// note: Host includes the port
	bc := BasicClient{url: urlStr, host: url.Host, hostname: url.Hostname(), port: url.Port(), http10: (proto == "1.0")}
	bc.buffer = make([]byte, BufferSizeKb*1024)
	if bc.port == "" {
		bc.port = url.Scheme // ie http which turns into 80 later
		LogVf("No port specified, using %s", bc.port)
	}
	addrs, err := net.LookupIP(bc.hostname)
	if err != nil {
		Errf("Unable to lookup '%s' : %v", bc.host, err)
		return nil
	}
	if len(addrs) > 1 && Log(Debug) {
		Debugf("Using only the first of the addresses for %s : %v", bc.host, addrs)
	}
	Debugf("Will go to %s", addrs[0])
	bc.dest.IP = addrs[0]
	bc.dest.Port, err = net.LookupPort("tcp", bc.port)
	if err != nil {
		Errf("Unable to resolve port '%s' : %v", bc.port, err)
		return nil
	}
	// Create the bytes for the request:
	host := bc.host
	if hostOverride != "" {
		host = hostOverride
	}
	var buf bytes.Buffer
	buf.WriteString("GET " + url.RequestURI() + " HTTP/" + proto + "\r\n")
	if !bc.http10 {
		buf.WriteString("Host: " + host + "\r\n")
		bc.parseHeaders = true
		if keepAlive {
			bc.keepAlive = true
		} else {
			buf.WriteString("Connection: close\r\n")
		}
	}
	for h := range extraHeaders {
		buf.WriteString(h)
		buf.WriteString(": ")
		buf.WriteString(extraHeaders.Get(h))
		buf.WriteString("\r\n")
	}
	buf.WriteString("\r\n")
	bc.req = buf.Bytes()
	Debugf("Created client:\n%+v\n%s", bc.dest, bc.req)
	return &bc
}

// Used for the fast case insensitive search
const toUpperMask = ^byte('a' - 'A')

// Slow but correct version
func toUpper(b byte) byte {
	if b >= 'a' && b <= 'z' {
		b -= ('a' - 'A')
	}
	return b
}

// ASCIIToUpper returns a byte array equal to the input string but in lowercase.
// Only wotks for ASCII, not meant for unicode.
func ASCIIToUpper(str string) []byte {
	numChars := utf8.RuneCountInString(str)
	if numChars != len(str) && Log(Verbose) {
		Errf("ASCIIFold(\"%s\") contains %d characters, some non ascii (byte length %d): will mangle", str, numChars, len(str))
	}
	res := make([]byte, numChars)
	// less surprising if we only mangle the extended characters
	i := 0
	for _, c := range str { // Attention: _ here != i for unicode characters
		res[i] = toUpper(byte(c))
		i++
	}
	return res
}

// FoldFind searches the bytes assuming ascii, ignoring the lowercase bit
// for testing. Not intended to work with unicode, meant for http headers
// and to be fast (see benchmark in test file).
func FoldFind(haystack []byte, needle []byte) (bool, int) {
	idx := 0
	found := false
	hackstackLen := len(haystack)
	needleLen := len(needle)
	if needleLen == 0 {
		return true, 0
	}
	if needleLen > hackstackLen { // those 2 ifs also handles haystackLen == 0
		return false, -1
	}
	needleOffset := 0
	for {
		h := haystack[idx]
		n := needle[needleOffset]
		// This line is quite performance sensitive. calling toUpper() for instance
		// is a 30% hit, even if called only on the haystack. The XOR lets us be
		// true for equality and the & with mask also true if the only difference
		// between the 2 is the case bit.
		xor := h ^ n // == 0 if strictly equal
		if (xor&toUpperMask) != 0 || (((h < 32) || (n < 32)) && (xor != 0)) {
			idx -= (needleOffset - 1) // does ++ most of the time
			needleOffset = 0
			if idx >= hackstackLen {
				break
			}
			continue
		}
		if needleOffset == needleLen-1 {
			found = true
			break
		}
		needleOffset++
		idx++
		if idx >= hackstackLen {
			break
		}
	}
	if !found {
		return false, -1
	}
	return true, idx - needleOffset
}

// ParseDecimal extracts the first positive integer number from the input.
// spaces are ignored.
// any character that isn't a digit cause the parsing to stop
func ParseDecimal(inp []byte) int {
	res := -1
	for _, b := range inp {
		if b == ' ' && res == -1 {
			continue
		}
		if b < '0' || b > '9' {
			break
		}
		digit := int(b - '0')
		if res == -1 {
			res = digit
		} else {
			res = 10*res + digit
		}
	}
	return res
}

// ParseChunkSize extracts the chunk size and consumes the line.
// Returns the offset of the data and the size of the chunk,
// 0, -1 when not found.
func ParseChunkSize(inp []byte) (int, int) {
	res := -1
	off := 0
	end := len(inp)
	inDigits := true
	for {
		if off >= end {
			return off, -1
		}
		if inDigits {
			b := toUpper(inp[off])
			var digit int
			if b >= 'A' && b <= 'F' {
				digit = 10 + int(b-'A')
			} else if b >= '0' && b <= '9' {
				digit = int(b - '0')
			} else {
				inDigits = false
				if res == -1 {
					Errf("Didn't find hex number %q", inp)
					return off, res
				}
				continue
			}
			if res == -1 {
				res = digit
			} else {
				res = 16*res + digit
			}
		} else {
			// After digits, skipping ahead to find \r\n
			if inp[off] == '\r' {
				off++
				if off >= end {
					return off, -1
				}
				if inp[off] == '\n' {
					// good case
					return off + 1, res
				}
			}
		}
		off++
	}
}

// return the result from the state.
func (c *BasicClient) returnRes() (int, []byte, int) {
	return c.code, c.buffer[:c.size], c.headerLen
}

// connect to destination.
func (c *BasicClient) connect() *net.TCPConn {
	socket, err := net.DialTCP("tcp", nil, &c.dest)
	if err != nil {
		Errf("Unable to connect to %v : %v", c.dest, err)
		return nil
	}
	// For now those errors are not critical/breaking
	if err = socket.SetNoDelay(true); err != nil {
		Warnf("Unable to connect to set tcp no delay %v %v : %v", socket, c.dest, err)
	}
	if err = socket.SetWriteBuffer(len(c.req)); err != nil {
		Warnf("Unable to connect to set write buffer %d %v %v : %v", len(c.req), socket, c.dest, err)
	}
	if err = socket.SetReadBuffer(len(c.buffer)); err != nil {
		Warnf("Unable to connect to read buffer %d %v %v : %v", len(c.buffer), socket, c.dest, err)
	}
	return socket
}

// Fetch fetches the url content. Returns http code, data, offset of body.
func (c *BasicClient) Fetch() (int, []byte, int) {
	c.code = -1
	c.size = 0
	c.headerLen = 0
	// Connect or reuse existing socket:
	conn := c.socket
	reuse := (conn != nil)
	if !reuse {
		conn = c.connect()
		if conn == nil {
			return c.returnRes()
		}
	} else {
		Debugf("Reusing socket %v", *conn)
	}
	c.socket = nil // because of error returns
	// Send the request:
	n, err := conn.Write(c.req)
	if err != nil {
		if reuse {
			// it's ok for the (idle) socket to die once, auto reconnect:
			Infof("Closing dead socket %v (%v)", *conn, err)
			conn.Close() // nolint: errcheck
			c.errorCount++
			return c.Fetch() // recurse once
		}
		Errf("Unable to write to %v %v : %v", conn, c.dest, err)
		return c.returnRes()
	}
	if n != len(c.req) {
		Errf("Short write to %v %v : %d instead of %d", conn, c.dest, n, len(c.req))
		return c.returnRes()
	}
	if !c.keepAlive {
		if err = conn.CloseWrite(); err != nil {
			Errf("Unable to close write to %v %v : %v", conn, c.dest, err)
			return c.returnRes()
		}
	}
	// Read the response:
	c.readResponse(conn)
	// Return the result:
	return c.returnRes()
}

// EscapeBytes returns printable string. Same as %q format without the
// surrounding/extra "".
func EscapeBytes(buf []byte) string {
	e := fmt.Sprintf("%q", buf)
	return e[1 : len(e)-1]
}

// DebugSummary returns a string with the size and escaped first max/2 and
// last max/2 bytes of a buffer (or the whole escaped buffer if small enough).
func DebugSummary(buf []byte, max int) string {
	l := len(buf)
	if l <= max+3 { //no point in shortening to add ... if we could return those 3
		return EscapeBytes(buf)
	}
	max /= 2
	return fmt.Sprintf("%d: %s...%s", l, EscapeBytes(buf[:max]), EscapeBytes(buf[l-max:]))
}

// Response reading:
// TODO: refactor - unwiedly/ugly atm
func (c *BasicClient) readResponse(conn *net.TCPConn) {
	max := len(c.buffer)
	parsedHeaders := false
	c.code = http.StatusOK // In http 1.0 mode we don't bother parsing anything
	endofHeadersStart := retcodeOffset + 3
	keepAlive := c.keepAlive
	chunkedMode := false
	checkConnectionClosedHeader := CheckConnectionClosedHeader
	for {
		n, err := conn.Read(c.buffer[c.size:])
		if err == io.EOF {
			if c.size == 0 {
				Errf("EOF before reading anything on %v %v", conn, c.dest)
				c.code = -1
			}
			break
		}
		if err != nil {
			Errf("Read error %v %v %d : %v", conn, c.dest, c.size, err)
			c.code = -1
			break
		}
		c.size += n
		if Log(Debug) {
			Debugf("Read ok %d total %d so far (-%d headers = %d data) %s", n, c.size, c.headerLen, c.size-c.headerLen, DebugSummary(c.buffer[c.size-n:c.size], 128))
		}
		if !parsedHeaders && c.parseHeaders {
			// enough to get the code?
			if c.size >= retcodeOffset+3 {
				// even if the bytes are garbage we'll get a non 200 code (bytes are unsigned)
				c.code = ParseDecimal(c.buffer[retcodeOffset : retcodeOffset+3])
				// TODO handle 100 Continue
				if c.code != http.StatusOK {
					Warnf("Parsed non ok code %d (%v)", c.code, string(c.buffer[:retcodeOffset+3]))
					break
				}
				if Log(Debug) {
					Debugf("Code %d, looking for end of headers at %d / %d, last CRLF %d",
						c.code, endofHeadersStart, c.size, c.headerLen)
				}
				// TODO: keep track of list of newlines to efficiently search headers only there
				idx := endofHeadersStart
				for idx < c.size-1 {
					if c.buffer[idx] == '\r' && c.buffer[idx+1] == '\n' {
						if c.headerLen == idx-2 { // found end of headers
							parsedHeaders = true
							break
						}
						c.headerLen = idx
						idx++
					}
					idx++
				}
				endofHeadersStart = c.size // start there next read
				if parsedHeaders {
					// We have headers !
					c.headerLen += 4 // we use this and not endofHeadersStart so http/1.0 does return 0 and not the optimization for search start
					if Log(Debug) {
						Debugf("headers are %d: %s", c.headerLen, c.buffer[:idx])
					}
					// Find the content length or chunked mode
					if keepAlive {
						var contentLength int
						found, offset := FoldFind(c.buffer[:c.headerLen], contentLengthHeader)
						if found {
							// Content-Length mode:
							contentLength = ParseDecimal(c.buffer[offset+len(contentLengthHeader) : c.headerLen])
							if contentLength < 0 {
								Warnf("Warning: content-length unparsable %s", string(c.buffer[offset+2:offset+len(contentLengthHeader)+4]))
								keepAlive = false
								break
							}
							max = c.headerLen + contentLength
							if LogDebug() { // somehow without the if we spend 400ms/10s in LogV (!)
								Debugf("found content length %d", contentLength)
							}
						} else {
							// Chunked mode (or err/missing):
							if found, _ := FoldFind(c.buffer[:c.headerLen], chunkedHeader); found {
								chunkedMode = true
								var dataStart int
								dataStart, contentLength = ParseChunkSize(c.buffer[c.headerLen:])
								max = c.headerLen + dataStart + contentLength + 2 // extra CR LF
								Debugf("chunk-length is %d (%s) setting max to %d",
									contentLength, c.buffer[c.headerLen:c.headerLen+dataStart-2],
									max)
							} else {
								if Log(Verbose) {
									LogVf("Warning: content-length missing in %s", string(c.buffer[:c.headerLen]))
								} else {
									Warnf("Warning: content-length missing (%d bytes headers)", c.headerLen)
								}
								keepAlive = false // can't keep keepAlive
								break
							}
						} // end of content-length section
						if max > len(c.buffer) {
							Warnf("Buffer is too small for headers %d + data %d - change -httpbufferkb flag to at least %d",
								c.headerLen, contentLength, (c.headerLen+contentLength)/1024+1)
							// TODO: just consume the extra instead
							max = len(c.buffer)
						}
						if checkConnectionClosedHeader {
							if found, _ := FoldFind(c.buffer[:c.headerLen], connectionCloseHeader); found {
								Infof("Server wants to close connection, no keep-alive!")
								keepAlive = false
							}
						}
					}
				}
			}
		}
		if c.size >= max {
			if !keepAlive {
				Errf("More data is available but stopping after %d, increase -httpbufferkb", max)
			}
			if !parsedHeaders && c.parseHeaders {
				Errf("Buffer too small (%d) to even finish reading headers, increase -httpbufferkb to get all the data", max)
				keepAlive = false
			}
			if chunkedMode {
				// Next chunk:
				dataStart, nextChunkLen := ParseChunkSize(c.buffer[max:c.size])
				if nextChunkLen == -1 {
					if c.size == max {
						Debugf("Couldn't find next chunk size, reading more %d %d", max, c.size)
					} else {
						Infof("Partial chunk size (%s), reading more %d %d", DebugSummary(c.buffer[max:c.size], 20), max, c.size)
					}
					continue
				} else if nextChunkLen == 0 {
					Debugf("Found last chunk %d %d", max+dataStart, c.size)
					if c.size != max+dataStart+2 || string(c.buffer[c.size-2:c.size]) != "\r\n" {
						Errf("Unexpected mismatch at the end sz=%d expected %d; end of buffer %q", c.size, max+dataStart+2, c.buffer[max:c.size])
					}
				} else {
					max += dataStart + nextChunkLen + 2 // extra CR LF
					Debugf("One more chunk %d -> new max %d", nextChunkLen, max)
					if max > len(c.buffer) {
						Errf("Buffer too small for %d data", max)
					} else {
						continue
					}
				}
			}
			break // we're done!
		}
	}
	// Figure out whether to keep or close the socket:
	if keepAlive && c.code == http.StatusOK {
		c.socket = conn // keep the open socket
	} else {
		if err := conn.Close(); err != nil {
			Errf("Close error %v %v %d : %v", conn, c.dest, c.size, err)
		} else {
			Debugf("Closed ok %v from %v after reading %d bytes", conn, c.dest, c.size)
		}
		// we cleared c.socket in caller already
	}
}

// -- Echo Server --

var (
	// EchoRequests is the number of request received. Only updated in Debug mode.
	EchoRequests int64
)

// EchoHandler is an http server handler echoing back the input.
func EchoHandler(w http.ResponseWriter, r *http.Request) {
	LogVf("%v %v %v %v", r.Method, r.URL, r.Proto, r.RemoteAddr)
	if LogDebug() {
		for name, headers := range r.Header {
			for _, h := range headers {
				Debugf("Header %v: %v\n", name, h)
			}
		}
	}
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		Errf("Error reading %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// echo back the Content-Type and Content-Length in the response
	for _, k := range []string{"Content-Type", "Content-Length"} {
		if v := r.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(http.StatusOK)
	if _, err = w.Write(data); err != nil {
		Errf("Error writing response %v to %v", err, r.RemoteAddr)
	}
	if LogDebug() {
		// TODO: this easily lead to contention - use 'thread local'
		rqNum := atomic.AddInt64(&EchoRequests, 1)
		Debugf("Requests: %v", rqNum)
	}
}

// DynamicHTTPServer listens on an available port, sets up an http or https
// (when secure is true) server on it and returns the listening port.
func DynamicHTTPServer(secure bool) int {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		Fatalf("Unable to listen to dynamic port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	Infof("Using port: %d", port)
	go func(secure bool, port int) {
		var err error
		if secure {
			Errf("Secure setup not yet supported will just close incoming connections for now")
			for {
				var c net.Conn
				c, err = listener.Accept()
				if err != nil {
					Errf("Accept error in dummy server %v", err)
					break
				}
				LogVf("Got connection from %v, closing", c.RemoteAddr())
				err = c.Close()
				if err != nil {
					Errf("Close error in dummy server %v", err)
					break
				}
			}
			//err = http.ServeTLS(listener, nil, "", "") // go 1.9
		} else {
			err = http.Serve(listener, nil)
		}
		if err != nil {
			Fatalf("Unable to serve with secure=%v on %d: %v", secure, port, err)
		}
	}(secure, port)
	return port
}

/*
// DebugHandlerTemplate returns debug/useful info on the http requet.
// slower heavier but nicer source code version of DebugHandler
func DebugHandlerTemplate(w http.ResponseWriter, r *http.Request) {
	LogVf("%v %v %v %v", r.Method, r.URL, r.Proto, r.RemoteAddr)
	hostname, _ := os.Hostname()
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		Errf("Error reading %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Note: this looks nicer but is about 2x slower / less qps / more cpu and 25% bigger executable than doing the writes oneself:
	const templ = `Φορτίο version {{.Version}} echo debug server on {{.Hostname}} - request from {{.R.RemoteAddr}}

{{.R.Method}} {{.R.URL}} {{.R.Proto}}

headers:

{{ range $name, $vals := .R.Header }}{{range $val := $vals}}{{$name}}: {{ $val }}
{{end}}{{end}}
body:

{{.Body}}
{{if .DumpEnv}}
environment:
{{ range $idx, $e := .Env }}
{{$e}}{{end}}
{{end}}`
	t := template.Must(template.New("debugOutput").Parse(templ))
	err = t.Execute(w, &struct {
		R        *http.Request
		Hostname string
		Version  string
		Body     string
		DumpEnv  bool
		Env      []string
	}{r, hostname, Version, DebugSummary(data, 512), r.FormValue("env") == "dump", os.Environ()})
	if err != nil {
		Critf("Template execution failed: %v", err)
	}
	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
}
*/

// DebugHandler returns debug/useful info to http client.
func DebugHandler(w http.ResponseWriter, r *http.Request) {
	LogVf("%v %v %v %v", r.Method, r.URL, r.Proto, r.RemoteAddr)
	var buf bytes.Buffer
	buf.WriteString("Φορτίο version ")
	buf.WriteString(Version)
	buf.WriteString(" echo debug server on ")
	hostname, _ := os.Hostname()
	buf.WriteString(hostname)
	buf.WriteString(" - request from ")
	buf.WriteString(r.RemoteAddr)
	buf.WriteString("\n\n")
	buf.WriteString(r.Method)
	buf.WriteByte(' ')
	buf.WriteString(r.URL.String())
	buf.WriteByte(' ')
	buf.WriteString(r.Proto)
	buf.WriteString("\n\nheaders:\n\n")
	// Host is removed from headers map and put here (!)
	buf.WriteString("Host: ")
	buf.WriteString(r.Host)
	for name, headers := range r.Header {
		buf.WriteByte('\n')
		buf.WriteString(name)
		buf.WriteString(": ")
		first := true
		for _, h := range headers {
			if !first {
				buf.WriteByte(',')
			}
			buf.WriteString(h)
			first = false
		}
	}
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		Errf("Error reading %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	buf.WriteString("\n\nbody:\n\n")
	buf.WriteString(DebugSummary(data, 512))
	buf.WriteByte('\n')
	if r.FormValue("env") == "dump" {
		buf.WriteString("\nenvironment:\n\n")
		for _, v := range os.Environ() {
			buf.WriteString(v)
			buf.WriteByte('\n')
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	if _, err = w.Write(buf.Bytes()); err != nil {
		Errf("Error writing response %v to %v", err, r.RemoteAddr)
	}
}

// EchoServer starts a debug / echo http server on the given port.
func EchoServer(port int, debugPath string) {
	fmt.Printf("Fortio %s echo server listening on port %v\n", Version, port)
	if debugPath != "" {
		http.HandleFunc(debugPath, DebugHandler)
	}
	http.HandleFunc("/", EchoHandler)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		fmt.Println("Error starting server", err)
	}
}

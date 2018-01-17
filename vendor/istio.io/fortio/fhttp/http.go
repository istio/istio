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

package fhttp // import "istio.io/fortio/fhttp"

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"istio.io/fortio/fnet"
	"istio.io/fortio/log"
	"istio.io/fortio/periodic"
	"istio.io/fortio/stats"
)

// Fetcher is the Url content fetcher that the different client implements.
type Fetcher interface {
	// Fetch returns http code, data, offset of body (for client which returns
	// headers)
	Fetch() (int, []byte, int)
}

var (
	// BufferSizeKb size of the buffer (max data) for optimized client in kilobytes defaults to 32k.
	BufferSizeKb = 128
	// CheckConnectionClosedHeader indicates whether to check for server side connection closed headers.
	CheckConnectionClosedHeader = false
	// 'constants', case doesn't matter for those 3
	contentLengthHeader   = []byte("\r\ncontent-length:")
	connectionCloseHeader = []byte("\r\nconnection: close")
	chunkedHeader         = []byte("\r\nTransfer-Encoding: chunked")
	// Start time of the server (used in debug handler for uptime).
	startTime time.Time
)

// NewHTTPOptions creates and initialize a HTTPOptions object.
// It replaces plain % to %25 in the url. If you already have properly
// escaped URLs use o.URL = to set it.
func NewHTTPOptions(url string) *HTTPOptions {
	h := HTTPOptions{}
	return h.Init(url)
}

// Init initializes the headers in an HTTPOptions (User-Agent).
// It replaces plain % to %25 in the url. If you already have properly
// escaped URLs use o.URL = to set it.
func (h *HTTPOptions) Init(url string) *HTTPOptions {
	if h.initDone {
		return h
	}
	h.initDone = true
	// unescape then rescape % to %25 (so if it was already %25 it stays)
	h.URL = strings.Replace(strings.Replace(url, "%25", "%", -1), "%", "%25", -1)
	h.NumConnections = 1
	if h.HTTPReqTimeOut <= 0 {
		h.HTTPReqTimeOut = HTTPReqTimeOutDefaultValue
	}
	h.ResetHeaders()
	h.extraHeaders.Add("User-Agent", userAgent)
	h.URLSchemeCheck()
	return h
}

// URLSchemeCheck makes sure the client will work with the scheme requested.
// it also adds missing http:// to emulate curl's behavior.
func (h *HTTPOptions) URLSchemeCheck() {
	log.LogVf("URLSchemeCheck %+v", h)
	if len(h.URL) == 0 {
		log.Errf("unexpected init with empty url")
		return
	}
	hs := "https://" // longer of the 2 prefixes
	lcURL := h.URL
	if len(lcURL) > len(hs) {
		lcURL = strings.ToLower(h.URL[:len(hs)]) // no need to tolower more than we check
	}
	if strings.HasPrefix(lcURL, hs) {
		if !h.DisableFastClient {
			log.Warnf("https requested, switching to standard go client")
			h.DisableFastClient = true
		}
		return // url is good
	}
	if !strings.HasPrefix(lcURL, "http://") {
		log.Warnf("assuming http:// on missing scheme for '%s'", h.URL)
		h.URL = "http://" + h.URL
	}
}

// Version is the fortio package version (TODO:auto gen/extract).
const (
	userAgent                  = "istio/fortio-" + periodic.Version
	retcodeOffset              = len("HTTP/1.X ")
	HTTPReqTimeOutDefaultValue = 15 * time.Second
)

// HTTPOptions holds the common options of both http clients and the headers.
type HTTPOptions struct {
	URL               string
	NumConnections    int  // num connections (for std client)
	Compression       bool // defaults to no compression, only used by std client
	DisableFastClient bool // defaults to fast client
	HTTP10            bool // defaults to http1.1
	DisableKeepAlive  bool // so default is keep alive
	AllowHalfClose    bool // if not keepalive, whether to half close after request
	initDone          bool
	// ExtraHeaders to be added to each request.
	extraHeaders http.Header
	// Host is treated specially, remember that one separately.
	hostOverride   string
	HTTPReqTimeOut time.Duration // timeout value for http request
}

// ResetHeaders resets all the headers, including the User-Agent one.
func (h *HTTPOptions) ResetHeaders() {
	h.extraHeaders = make(http.Header)
	h.hostOverride = ""
}

// GetHeaders returns the current set of headers.
func (h *HTTPOptions) GetHeaders() http.Header {
	if h.hostOverride == "" {
		return h.extraHeaders
	}
	cp := h.extraHeaders
	cp.Add("Host", h.hostOverride)
	return cp
}

// AddAndValidateExtraHeader collects extra headers (see main.go for example).
func (h *HTTPOptions) AddAndValidateExtraHeader(hdr string) error {
	s := strings.SplitN(hdr, ":", 2)
	if len(s) != 2 {
		return fmt.Errorf("invalid extra header '%s', expecting Key: Value", hdr)
	}
	key := strings.TrimSpace(s[0])
	value := strings.TrimSpace(s[1])
	if strings.EqualFold(key, "host") {
		log.LogVf("Will be setting special Host header to %s", value)
		h.hostOverride = value
	} else {
		log.LogVf("Setting regular extra header %s: %s", key, value)
		h.extraHeaders.Add(key, value)
		log.Debugf("headers now %+v", h.extraHeaders)
	}
	return nil
}

// newHttpRequest makes a new http GET request for url with User-Agent.
func newHTTPRequest(o *HTTPOptions) *http.Request {
	req, err := http.NewRequest("GET", o.URL, nil)
	if err != nil {
		log.Errf("Unable to make request for %s : %v", o.URL, err)
		return nil
	}
	req.Header = o.extraHeaders
	if o.hostOverride != "" {
		req.Host = o.hostOverride
	}
	if !log.LogDebug() {
		return req
	}
	bytes, err := httputil.DumpRequestOut(req, false)
	if err != nil {
		log.Errf("Unable to dump request %v", err)
	} else {
		log.Debugf("For URL %s, sending:\n%s", o.URL, bytes)
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

// ChangeURL only for standard client, allows fetching a different URL
func (c *Client) ChangeURL(urlStr string) (err error) {
	c.url = urlStr
	c.req.URL, err = url.Parse(urlStr)
	return err
}

// Fetch fetches the byte and code for pre created client
func (c *Client) Fetch() (int, []byte, int) {
	// req can't be null (client itself would be null in that case)
	resp, err := c.client.Do(c.req)
	if err != nil {
		log.Errf("Unable to send request for %s : %v", c.url, err)
		return http.StatusBadRequest, []byte(err.Error()), 0
	}
	var data []byte
	if log.LogDebug() {
		if data, err = httputil.DumpResponse(resp, false); err != nil {
			log.Errf("Unable to dump response %v", err)
		} else {
			log.Debugf("For URL %s, received:\n%s", c.url, data)
		}
	}
	data, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close() //nolint(errcheck)
	if err != nil {
		log.Errf("Unable to read response for %s : %v", c.url, err)
		code := resp.StatusCode
		if code == http.StatusOK {
			code = http.StatusNoContent
			log.Warnf("Ok code despite read error, switching code to %d", code)
		}
		return code, data, 0
	}
	code := resp.StatusCode
	log.Debugf("Got %d : %s for %s - response is %d bytes", code, resp.Status, c.url, len(data))
	return code, data, 0
}

// NewClient creates either a standard or fast client (depending on
// the DisableFastClient flag)
func NewClient(o *HTTPOptions) Fetcher {
	o.URLSchemeCheck()
	if o.DisableFastClient {
		return NewStdClient(o)
	}
	return NewBasicClient(o)
}

// NewStdClient creates a client object that wraps the net/http standard client.
func NewStdClient(o *HTTPOptions) *Client {
	req := newHTTPRequest(o)
	if req == nil {
		return nil
	}
	if o.NumConnections < 1 {
		o.NumConnections = 1
	}
	// 0 timeout for stdclient doesn't mean 0 timeout... so just warn and leave it
	if o.HTTPReqTimeOut <= 0 {
		log.Warnf("Std call with client timeout %v", o.HTTPReqTimeOut)
	}
	client := Client{
		o.URL,
		req,
		&http.Client{
			Timeout: o.HTTPReqTimeOut,
			Transport: &http.Transport{
				MaxIdleConns:        o.NumConnections,
				MaxIdleConnsPerHost: o.NumConnections,
				DisableCompression:  !o.Compression,
				DisableKeepAlives:   o.DisableKeepAlive,
				Dial: (&net.Dialer{
					Timeout: o.HTTPReqTimeOut,
				}).Dial,
				TLSHandshakeTimeout: o.HTTPReqTimeOut,
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
	halfClose    bool // allow/do half close when keepAlive is false
	reqTimeout   time.Duration
}

// NewBasicClient makes a basic, efficient http 1.0/1.1 client.
// This function itself doesn't need to be super efficient as it is created at
// the beginning and then reused many times.
func NewBasicClient(o *HTTPOptions) Fetcher {
	proto := "1.1"
	if o.HTTP10 {
		proto = "1.0"
	}
	// Parse the url, extract components.
	url, err := url.Parse(o.URL)
	if err != nil {
		log.Errf("Bad url '%s' : %v", o.URL, err)
		return nil
	}
	if url.Scheme != "http" {
		log.Errf("Only http is supported with the optimized client, use -stdclient for url %s", o.URL)
		return nil
	}
	// note: Host includes the port
	bc := BasicClient{url: o.URL, host: url.Host, hostname: url.Hostname(), port: url.Port(),
		http10: o.HTTP10, halfClose: o.AllowHalfClose}
	bc.buffer = make([]byte, BufferSizeKb*1024)
	if bc.port == "" {
		bc.port = url.Scheme // ie http which turns into 80 later
		log.LogVf("No port specified, using %s", bc.port)
	}
	addrs, err := net.LookupIP(bc.hostname)
	if err != nil {
		log.Errf("Unable to lookup '%s' : %v", bc.host, err)
		return nil
	}
	if len(addrs) > 1 && log.LogDebug() {
		log.Debugf("Using only the first of the addresses for %s : %v", bc.host, addrs)
	}
	log.Debugf("Will go to %s", addrs[0])
	bc.dest.IP = addrs[0]
	bc.dest.Port, err = net.LookupPort("tcp", bc.port)
	if err != nil {
		log.Errf("Unable to resolve port '%s' : %v", bc.port, err)
		return nil
	}
	// Create the bytes for the request:
	host := bc.host
	if o.hostOverride != "" {
		host = o.hostOverride
	}
	var buf bytes.Buffer
	buf.WriteString("GET " + url.RequestURI() + " HTTP/" + proto + "\r\n")
	if !bc.http10 {
		buf.WriteString("Host: " + host + "\r\n")
		bc.parseHeaders = true
		if !o.DisableKeepAlive {
			bc.keepAlive = true
		} else {
			buf.WriteString("Connection: close\r\n")
		}
	}
	if o.HTTPReqTimeOut <= 0 {
		log.Warnf("Invalid timeout %v, setting to %v", o.HTTPReqTimeOut, HTTPReqTimeOutDefaultValue)
		o.HTTPReqTimeOut = HTTPReqTimeOutDefaultValue
	}
	bc.reqTimeout = o.HTTPReqTimeOut
	w := bufio.NewWriter(&buf)
	// This writes multiple valued headers properly (unlike calling Get() to do it ourselves)
	o.extraHeaders.Write(w) // nolint: errcheck,gas
	w.Flush()               // nolint: errcheck,gas
	buf.WriteString("\r\n")
	bc.req = buf.Bytes()
	log.Debugf("Created client:\n%+v\n%s", bc.dest, bc.req)
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
	if numChars != len(str) && log.LogVerbose() {
		log.Errf("ASCIIFold(\"%s\") contains %d characters, some non ascii (byte length %d): will mangle", str, numChars, len(str))
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
	if log.LogDebug() {
		log.Debugf("ParseChunkSize(%s)", DebugSummary(inp, 128))
	}
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
					log.Errf("Didn't find hex number %q", inp)
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
		log.Errf("Unable to connect to %v : %v", c.dest, err)
		return nil
	}
	// For now those errors are not critical/breaking
	if err = socket.SetNoDelay(true); err != nil {
		log.Warnf("Unable to connect to set tcp no delay %v %v : %v", socket, c.dest, err)
	}
	if err = socket.SetWriteBuffer(len(c.req)); err != nil {
		log.Warnf("Unable to connect to set write buffer %d %v %v : %v", len(c.req), socket, c.dest, err)
	}
	if err = socket.SetReadBuffer(len(c.buffer)); err != nil {
		log.Warnf("Unable to connect to read buffer %d %v %v : %v", len(c.buffer), socket, c.dest, err)
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
		log.Debugf("Reusing socket %v", *conn)
	}
	c.socket = nil // because of error returns
	conErr := conn.SetReadDeadline(time.Now().Add(c.reqTimeout))
	// Send the request:
	n, err := conn.Write(c.req)
	if err != nil || conErr != nil {
		if reuse {
			// it's ok for the (idle) socket to die once, auto reconnect:
			log.Infof("Closing dead socket %v (%v)", *conn, err)
			conn.Close() // nolint: errcheck,gas
			c.errorCount++
			return c.Fetch() // recurse once
		}
		log.Errf("Unable to write to %v %v : %v", conn, c.dest, err)
		return c.returnRes()
	}
	if n != len(c.req) {
		log.Errf("Short write to %v %v : %d instead of %d", conn, c.dest, n, len(c.req))
		return c.returnRes()
	}
	if !c.keepAlive && c.halfClose {
		if err = conn.CloseWrite(); err != nil {
			log.Errf("Unable to close write to %v %v : %v", conn, c.dest, err)
			return c.returnRes()
		} // else:
		log.Debugf("Half closed ok after sending request %v %v", conn, c.dest)
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
	// TODO: safer to start with -1 and fix ok for http 1.0
	c.code = http.StatusOK // In http 1.0 mode we don't bother parsing anything
	endofHeadersStart := retcodeOffset + 3
	keepAlive := c.keepAlive
	chunkedMode := false
	checkConnectionClosedHeader := CheckConnectionClosedHeader
	skipRead := false
	for {
		// Ugly way to cover the case where we get more than 1 chunk at the end
		// TODO: need automated tests
		if !skipRead {
			n, err := conn.Read(c.buffer[c.size:])
			if err == io.EOF {
				if c.size == 0 {
					log.Errf("EOF before reading anything on %v %v", conn, c.dest)
					c.code = -1
				}
				break
			}
			if err != nil {
				log.Errf("Read error %v %v %d : %v", conn, c.dest, c.size, err)
				c.code = -1
				break
			}
			c.size += n
			if log.LogDebug() {
				log.Debugf("Read ok %d total %d so far (-%d headers = %d data) %s",
					n, c.size, c.headerLen, c.size-c.headerLen, DebugSummary(c.buffer[c.size-n:c.size], 256))
			}
		}
		skipRead = false
		// Have not yet parsed the headers, need to parse the headers, and have enough data to
		// at least parse the http retcode:
		if !parsedHeaders && c.parseHeaders && c.size >= retcodeOffset+3 {
			// even if the bytes are garbage we'll get a non 200 code (bytes are unsigned)
			c.code = ParseDecimal(c.buffer[retcodeOffset : retcodeOffset+3]) //TODO do that only once...
			// TODO handle 100 Continue
			if c.code != http.StatusOK {
				log.Warnf("Parsed non ok code %d (%v)", c.code, string(c.buffer[:retcodeOffset+3]))
				break
			}
			if log.LogDebug() {
				log.Debugf("Code %d, looking for end of headers at %d / %d, last CRLF %d",
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
				if log.LogDebug() {
					log.Debugf("headers are %d: %s", c.headerLen, c.buffer[:idx])
				}
				// Find the content length or chunked mode
				if keepAlive {
					var contentLength int
					found, offset := FoldFind(c.buffer[:c.headerLen], contentLengthHeader)
					if found {
						// Content-Length mode:
						contentLength = ParseDecimal(c.buffer[offset+len(contentLengthHeader) : c.headerLen])
						if contentLength < 0 {
							log.Warnf("Warning: content-length unparsable %s", string(c.buffer[offset+2:offset+len(contentLengthHeader)+4]))
							keepAlive = false
							break
						}
						max = c.headerLen + contentLength
						if log.LogDebug() { // somehow without the if we spend 400ms/10s in LogV (!)
							log.Debugf("found content length %d", contentLength)
						}
					} else {
						// Chunked mode (or err/missing):
						if found, _ := FoldFind(c.buffer[:c.headerLen], chunkedHeader); found {
							chunkedMode = true
							var dataStart int
							dataStart, contentLength = ParseChunkSize(c.buffer[c.headerLen:c.size])
							max = c.headerLen + dataStart + contentLength + 2 // extra CR LF
							log.Debugf("chunk-length is %d (%s) setting max to %d",
								contentLength, c.buffer[c.headerLen:c.headerLen+dataStart-2],
								max)
						} else {
							if log.LogVerbose() {
								log.LogVf("Warning: content-length missing in %s", string(c.buffer[:c.headerLen]))
							} else {
								log.Warnf("Warning: content-length missing (%d bytes headers)", c.headerLen)
							}
							keepAlive = false // can't keep keepAlive
							break
						}
					} // end of content-length section
					if max > len(c.buffer) {
						log.Warnf("Buffer is too small for headers %d + data %d - change -httpbufferkb flag to at least %d",
							c.headerLen, contentLength, (c.headerLen+contentLength)/1024+1)
						// TODO: just consume the extra instead
						max = len(c.buffer)
					}
					if checkConnectionClosedHeader {
						if found, _ := FoldFind(c.buffer[:c.headerLen], connectionCloseHeader); found {
							log.Infof("Server wants to close connection, no keep-alive!")
							keepAlive = false
							max = len(c.buffer) // reset to read as much as available
						}
					}
				}
			}
		} // end of big if parse header
		if c.size >= max {
			if !keepAlive {
				log.Errf("More data is available but stopping after %d, increase -httpbufferkb", max)
			}
			if !parsedHeaders && c.parseHeaders {
				log.Errf("Buffer too small (%d) to even finish reading headers, increase -httpbufferkb to get all the data", max)
				keepAlive = false
			}
			if chunkedMode {
				// Next chunk:
				dataStart, nextChunkLen := ParseChunkSize(c.buffer[max:c.size])
				if nextChunkLen == -1 {
					if c.size == max {
						log.Debugf("Couldn't find next chunk size, reading more %d %d", max, c.size)
					} else {
						log.Infof("Partial chunk size (%s), reading more %d %d", DebugSummary(c.buffer[max:c.size], 20), max, c.size)
					}
					continue
				} else if nextChunkLen == 0 {
					log.Debugf("Found last chunk %d %d", max+dataStart, c.size)
					if c.size != max+dataStart+2 || string(c.buffer[c.size-2:c.size]) != "\r\n" {
						log.Errf("Unexpected mismatch at the end sz=%d expected %d; end of buffer %q", c.size, max+dataStart+2, c.buffer[max:c.size])
					}
				} else {
					max += dataStart + nextChunkLen + 2 // extra CR LF
					log.Debugf("One more chunk %d -> new max %d", nextChunkLen, max)
					if max > len(c.buffer) {
						log.Errf("Buffer too small for %d data", max)
					} else {
						if max <= c.size {
							log.Debugf("Enough data to reach next chunk, skipping a read")
							skipRead = true
						}
						continue
					}
				}
			}
			break // we're done!
		}
	} // end of big for loop
	// Figure out whether to keep or close the socket:
	if keepAlive && c.code == http.StatusOK {
		c.socket = conn // keep the open socket
	} else {
		if err := conn.Close(); err != nil {
			log.Errf("Close error %v %v %d : %v", conn, c.dest, c.size, err)
		} else {
			log.Debugf("Closed ok %v from %v after reading %d bytes", conn, c.dest, c.size)
		}
		// we cleared c.socket in caller already
	}
}

// -- Echo Server --

var (
	// EchoRequests is the number of request received. Only updated in Debug mode.
	EchoRequests int64
)

func removeTrailingPercent(s string) string {
	if strings.HasSuffix(s, "%") {
		return s[:len(s)-1]
	}
	return s
}

// generateStatus from string, format: status="503" for 100% 503s
// status="503:20,404:10,403:0.5" for 20% 503s, 10% 404s, 0.5% 403s 69.5% 200s
func generateStatus(status string) int {
	lst := strings.Split(status, ",")
	log.Debugf("Parsing status %s -> %v", status, lst)
	// Simple non probabilistic status case:
	if len(lst) == 1 && !strings.ContainsRune(status, ':') {
		s, err := strconv.Atoi(status)
		if err != nil {
			log.Warnf("Bad input status %v, not a number nor comma and colon separated %% list", status)
			return http.StatusBadRequest
		}
		log.Debugf("Parsed status %s -> %d", status, s)
		return s
	}
	weights := make([]float32, len(lst))
	codes := make([]int, len(lst))
	lastPercent := float64(0)
	i := 0
	for _, entry := range lst {
		l2 := strings.Split(entry, ":")
		if len(l2) != 2 {
			log.Warnf("Should have exactly 1 : in status list %s -> %v", status, entry)
			return http.StatusBadRequest
		}
		s, err := strconv.Atoi(l2[0])
		if err != nil {
			log.Warnf("Bad input status %v -> %v, not a number before colon", status, l2[0])
			return http.StatusBadRequest
		}
		percStr := removeTrailingPercent(l2[1])
		p, err := strconv.ParseFloat(percStr, 32)
		if err != nil || p < 0 || p > 100 {
			log.Warnf("Percentage is not a [0. - 100.] number in %v -> %v : %v %f", status, percStr, err, p)
			return http.StatusBadRequest
		}
		lastPercent += p
		// Round() needed to cover 'exactly' 100% and not more or less because of rounding errors
		p32 := float32(stats.Round(lastPercent))
		if p32 > 100. {
			log.Warnf("Sum of percentage is greater than 100 in %v %f %f %f", status, lastPercent, p, p32)
			return http.StatusBadRequest
		}
		weights[i] = p32
		codes[i] = s
		i++
	}
	res := 100. * rand.Float32()
	for i, v := range weights {
		if res <= v {
			log.Debugf("[0.-100.[ for %s roll %f got #%d -> %d", status, res, i, codes[i])
			return codes[i]
		}
	}
	log.Debugf("[0.-100.[ for %s roll %f no hit, defaulting to OK", status, res)
	return http.StatusOK // default/reminder of probability table
}

// MaxDelay is the maximum delay allowed for the echoserver responses.
const MaxDelay = 1 * time.Second

// generateDelay from string, format: delay="100ms" for 100% 100ms delay
// delay="10ms:20,20ms:10,1s:0.5" for 20% 10ms, 10% 20ms, 0.5% 1s and 69.5% 0
// TODO: very similar with generateStatus - refactor?
func generateDelay(delay string) time.Duration {
	lst := strings.Split(delay, ",")
	log.Debugf("Parsing delay %s -> %v", delay, lst)
	if len(delay) == 0 {
		return -1
	}
	// Simple non probabilistic status case:
	if len(lst) == 1 && !strings.ContainsRune(delay, ':') {
		d, err := time.ParseDuration(delay)
		if err != nil {
			log.Warnf("Bad input delay %v, not a duration nor comma and colon separated %% list", delay)
			return -1
		}
		log.Debugf("Parsed delay %s -> %d", delay, d)
		if d > MaxDelay {
			d = MaxDelay
		}
		return d
	}
	weights := make([]float32, len(lst))
	delays := make([]time.Duration, len(lst))
	lastPercent := float64(0)
	i := 0
	for _, entry := range lst {
		l2 := strings.Split(entry, ":")
		if len(l2) != 2 {
			log.Warnf("Should have exactly 1 : in delay list %s -> %v", delay, entry)
			return -1
		}
		d, err := time.ParseDuration(l2[0])
		if err != nil {
			log.Warnf("Bad input delay %v -> %v, not a number before colon", delay, l2[0])
			return -1
		}
		if d > MaxDelay {
			d = MaxDelay
		}
		percStr := removeTrailingPercent(l2[1])
		p, err := strconv.ParseFloat(percStr, 32)
		if err != nil || p < 0 || p > 100 {
			log.Warnf("Percentage is not a [0. - 100.] number in %v -> %v : %v %f", delay, percStr, err, p)
			return -1
		}
		lastPercent += p
		// Round() needed to cover 'exactly' 100% and not more or less because of rounding errors
		p32 := float32(stats.Round(lastPercent))
		if p32 > 100. {
			log.Warnf("Sum of percentage is greater than 100 in %v %f %f %f", delay, lastPercent, p, p32)
			return -1
		}
		weights[i] = p32
		delays[i] = d
		i++
	}
	res := 100. * rand.Float32()
	for i, v := range weights {
		if res <= v {
			log.Debugf("[0.-100.[ for %s roll %f got #%d -> %d", delay, res, i, delays[i])
			return delays[i]
		}
	}
	log.Debugf("[0.-100.[ for %s roll %f no hit, defaulting to 0", delay, res)
	return 0
}

// EchoHandler is an http server handler echoing back the input.
func EchoHandler(w http.ResponseWriter, r *http.Request) {
	log.LogVf("%v %v %v %v", r.Method, r.URL, r.Proto, r.RemoteAddr)
	data, err := ioutil.ReadAll(r.Body) // must be done before calling FormValue
	if err != nil {
		log.Errf("Error reading %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Debugf("Read %d", len(data))
	dur := generateDelay(r.FormValue("delay"))
	if dur > 0 {
		log.LogVf("Sleeping for %v", dur)
		time.Sleep(dur)
	}
	statusStr := r.FormValue("status")
	var status int
	if statusStr != "" {
		status = generateStatus(statusStr)
	} else {
		status = http.StatusOK
	}
	if log.LogDebug() {
		for name, headers := range r.Header {
			for _, h := range headers {
				log.Debugf("Header %v: %v\n", name, h)
			}
		}
	}
	// echo back the Content-Type and Content-Length in the response
	for _, k := range []string{"Content-Type", "Content-Length"} {
		if v := r.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(status)
	if _, err = w.Write(data); err != nil {
		log.Errf("Error writing response %v to %v", err, r.RemoteAddr)
	}
	if log.LogDebug() {
		// TODO: this easily lead to contention - use 'thread local'
		rqNum := atomic.AddInt64(&EchoRequests, 1)
		log.Debugf("Requests: %v", rqNum)
	}
}

func closingServer(listener net.Listener) error {
	var err error
	for {
		var c net.Conn
		c, err = listener.Accept()
		if err != nil {
			log.Errf("Accept error in dummy server %v", err)
			break
		}
		log.LogVf("Got connection from %v, closing", c.RemoteAddr())
		err = c.Close()
		if err != nil {
			log.Errf("Close error in dummy server %v", err)
			break
		}
	}
	return err
}

// DynamicHTTPServer listens on an available port, sets up an http or https
// (when secure is true) server on it and returns the listening port and
// mux to which one can attach handlers to.
func DynamicHTTPServer(secure bool) (int, *http.ServeMux) {
	m := http.NewServeMux()
	s := &http.Server{
		Handler: m,
	}
	listener, err := net.Listen("tcp", ":0") // nolint: gas
	if err != nil {
		log.Fatalf("Unable to listen to dynamic port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	log.Infof("Using port: %d", port)
	go func() {
		var err error
		if secure {
			log.Errf("Secure setup not yet supported. Will just close incoming connections for now")
			//err = http.ServeTLS(listener, nil, "", "") // go 1.9
			err = closingServer(listener)
		} else {
			err = s.Serve(listener)
		}
		if err != nil {
			log.Fatalf("Unable to serve with secure=%v on %d: %v", secure, port, err)
		}
	}()
	return port, m
}

/*
// DebugHandlerTemplate returns debug/useful info on the http requet.
// slower heavier but nicer source code version of DebugHandler
func DebugHandlerTemplate(w http.ResponseWriter, r *http.Request) {
	log.LogVf("%v %v %v %v", r.Method, r.URL, r.Proto, r.RemoteAddr)
	hostname, _ := os.Hostname()
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Errf("Error reading %v", err)
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

// RoundDuration rounds to 10th of second. Only for positive durations.
// TODO: switch to Duration.Round once switched to go 1.9
func RoundDuration(d time.Duration) time.Duration {
	tenthSec := int64(100 * time.Millisecond)
	r := int64(d+50*time.Millisecond) / tenthSec
	return time.Duration(tenthSec * r)
}

// DebugHandler returns debug/useful info to http client.
func DebugHandler(w http.ResponseWriter, r *http.Request) {
	log.LogVf("%v %v %v %v", r.Method, r.URL, r.Proto, r.RemoteAddr)
	var buf bytes.Buffer
	buf.WriteString("Φορτίο version ")
	buf.WriteString(periodic.Version)
	buf.WriteString(" echo debug server up for ")
	buf.WriteString(fmt.Sprint(RoundDuration(time.Since(startTime))))
	buf.WriteString(" on ")
	hostname, _ := os.Hostname() // nolint: gas
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
		log.Errf("Error reading %v", err)
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
		log.Errf("Error writing response %v to %v", err, r.RemoteAddr)
	}
}

// Serve starts a debug / echo http server on the given port.
// TODO: make it work for port 0 and return the port found and also
// add a non blocking mode that makes sure the socket exists before returning
func Serve(port, debugPath string) {
	startTime = time.Now()
	nPort := fnet.NormalizePort(port)
	fmt.Printf("Fortio %s echo server listening on port %s\n", periodic.Version, nPort)
	if debugPath != "" {
		http.HandleFunc(debugPath, DebugHandler)
	}
	http.HandleFunc("/", EchoHandler)
	if err := http.ListenAndServe(nPort, nil); err != nil {
		fmt.Println("Error starting server", err)
	}
}

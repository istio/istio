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

package fhttp // import "fortio.org/fortio/fhttp"

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fortio.org/fortio/fnet"
	"fortio.org/fortio/log"
	"fortio.org/fortio/version"
)

// Fetcher is the Url content fetcher that the different client implements.
type Fetcher interface {
	// Fetch returns http code, data, offset of body (for client which returns
	// headers)
	Fetch() (int, []byte, int)
	// Close() cleans up connections and state - must be paired with NewClient calls.
	// returns how many sockets have been used (Fastclient only)
	Close() int
}

var (
	// BufferSizeKb size of the buffer (max data) for optimized client in kilobytes defaults to 128k.
	BufferSizeKb = 128
	// CheckConnectionClosedHeader indicates whether to check for server side connection closed headers.
	CheckConnectionClosedHeader = false
	// 'constants', case doesn't matter for those 3
	contentLengthHeader   = []byte("\r\ncontent-length:")
	connectionCloseHeader = []byte("\r\nconnection: close")
	chunkedHeader         = []byte("\r\nTransfer-Encoding: chunked")
)

// NewHTTPOptions creates and initialize a HTTPOptions object.
// It replaces plain % to %25 in the url. If you already have properly
// escaped URLs use o.URL = to set it.
func NewHTTPOptions(url string) *HTTPOptions {
	h := HTTPOptions{}
	return h.Init(url)
}

// Init initializes the headers in an HTTPOptions (User-Agent).
func (h *HTTPOptions) Init(url string) *HTTPOptions {
	if h.initDone {
		return h
	}
	h.initDone = true
	h.URL = url
	h.NumConnections = 1
	if h.HTTPReqTimeOut == 0 {
		log.Debugf("Request timeout not set, using default %v", HTTPReqTimeOutDefaultValue)
		h.HTTPReqTimeOut = HTTPReqTimeOutDefaultValue
	}
	if h.HTTPReqTimeOut < 0 {
		log.Warnf("Invalid timeout %v, setting to %v", h.HTTPReqTimeOut, HTTPReqTimeOutDefaultValue)
		h.HTTPReqTimeOut = HTTPReqTimeOutDefaultValue
	}
	h.URLSchemeCheck()
	return h
}

const (
	contentType   = "Content-Type"
	contentLength = "Content-Length"
)

// GenerateHeaders completes the header generation, including Content-Type/Length
// and user credential coming from the http options in addition to extra headers
// coming from flags and AddAndValidateExtraHeader().
// Warning this gets called more than once, do not generate duplicate headers.
func (h *HTTPOptions) GenerateHeaders() http.Header {
	if h.extraHeaders == nil { // not already initialized from flags.
		h.InitHeaders()
	}
	allHeaders := h.extraHeaders
	payloadLen := len(h.Payload)
	// If content-type isn't already specified and we have a payload, let's use the
	// standard for binary content:
	if payloadLen > 0 && len(h.ContentType) == 0 && len(allHeaders.Get(contentType)) == 0 {
		h.ContentType = "application/octet-stream"
	}
	if len(h.ContentType) > 0 {
		allHeaders.Set(contentType, h.ContentType)
	}
	// Add content-length unless already set in custom headers (or we're not doing a POST)
	if (payloadLen > 0 || len(h.ContentType) > 0) && len(allHeaders.Get(contentLength)) == 0 {
		allHeaders.Set(contentLength, strconv.Itoa(payloadLen))
	}
	err := h.ValidateAndAddBasicAuthentication(allHeaders)
	if err != nil {
		log.Errf("User credential is not valid: %v", err)
	}
	return allHeaders
}

// URLSchemeCheck makes sure the client will work with the scheme requested.
// it also adds missing http:// to emulate curl's behavior.
func (h *HTTPOptions) URLSchemeCheck() {
	log.LogVf("URLSchemeCheck %+v", h)
	if len(h.URL) == 0 {
		log.Errf("Unexpected init with empty url")
		return
	}
	hs := "https://" // longer of the 2 prefixes
	lcURL := h.URL
	if len(lcURL) > len(hs) {
		lcURL = strings.ToLower(h.URL[:len(hs)]) // no need to tolower more than we check
	}
	if strings.HasPrefix(lcURL, hs) {
		h.https = true
		if !h.DisableFastClient {
			log.Warnf("https requested, switching to standard go client")
			h.DisableFastClient = true
		}
		return // url is good
	}
	if !strings.HasPrefix(lcURL, "http://") {
		log.Warnf("Assuming http:// on missing scheme for '%s'", h.URL)
		h.URL = "http://" + h.URL
	}
}

var userAgent = "fortio.org/fortio-" + version.Short()

const (
	retcodeOffset = len("HTTP/1.X ")
	// HTTPReqTimeOutDefaultValue is the default timeout value. 15s.
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
	Insecure          bool // do not verify certs for https
	FollowRedirects   bool // For the Std Client only: follow redirects.
	initDone          bool
	https             bool // whether URLSchemeCheck determined this was an https:// call or not
	// ExtraHeaders to be added to each request (UserAgent and headers set through AddAndValidateExtraHeader()).
	extraHeaders http.Header
	// Host is treated specially, remember that virtual header separately.
	hostOverride   string
	HTTPReqTimeOut time.Duration // timeout value for http request

	UserCredentials string // user credentials for authorization
	ContentType     string // indicates request body type, implies POST instead of GET
	Payload         []byte // body for http request, implies POST if not empty.

	UnixDomainSocket string // Path of unix domain socket to use instead of host:port from URL
}

// ResetHeaders resets all the headers, including the User-Agent: one (and the Host: logical special header).
// This is used from the UI as the user agent is settable from the form UI.
func (h *HTTPOptions) ResetHeaders() {
	h.extraHeaders = make(http.Header)
	h.hostOverride = ""
}

// InitHeaders initialize and/or resets the default headers (ie just User-Agent).
func (h *HTTPOptions) InitHeaders() {
	h.ResetHeaders()
	h.extraHeaders.Add("User-Agent", userAgent)
	// No other headers should be added here based on options content as this is called only once
	// before command line option -H are parsed/set.
}

// PayloadString returns the payload as a string. If payload is null return empty string
// This is only needed due to grpc ping proto. It takes string instead of byte array.
func (h *HTTPOptions) PayloadString() string {
	if len(h.Payload) == 0 {
		return ""
	}
	return string(h.Payload)
}

// ValidateAndAddBasicAuthentication validates user credentials and adds basic authentication to http header,
// if user credentials are valid.
func (h *HTTPOptions) ValidateAndAddBasicAuthentication(headers http.Header) error {
	if len(h.UserCredentials) <= 0 {
		return nil // user credential is not entered
	}
	s := strings.SplitN(h.UserCredentials, ":", 2)
	if len(s) != 2 {
		return fmt.Errorf("invalid user credentials \"%s\", expecting \"user:password\"", h.UserCredentials)
	}
	headers.Set("Authorization", generateBase64UserCredentials(h.UserCredentials))
	return nil
}

// AllHeaders returns the current set of headers including virtual/special Host header.
func (h *HTTPOptions) AllHeaders() http.Header {
	headers := h.GenerateHeaders()
	if h.hostOverride != "" {
		headers.Add("Host", h.hostOverride)
	}
	return headers
}

// Method returns the method of the http req.
func (h *HTTPOptions) Method() string {
	if len(h.Payload) > 0 || h.ContentType != "" {
		return fnet.POST
	}
	return fnet.GET
}

// AddAndValidateExtraHeader collects extra headers (see commonflags.go for example).
func (h *HTTPOptions) AddAndValidateExtraHeader(hdr string) error {
	// This function can be called from the flag settings, before we have a URL
	// so we can't just call h.Init(h.URL)
	if h.extraHeaders == nil {
		h.InitHeaders()
	}
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
	method := o.Method()
	var body io.Reader
	if method == fnet.POST {
		body = bytes.NewReader(o.Payload)
	}
	req, err := http.NewRequest(method, o.URL, body)
	if err != nil {
		log.Errf("Unable to make %s request for %s : %v", method, o.URL, err)
		return nil
	}
	req.Header = o.GenerateHeaders()
	if o.hostOverride != "" {
		req.Host = o.hostOverride
	}
	if !log.LogDebug() {
		return req
	}
	bytes, err := httputil.DumpRequestOut(req, false)
	if err != nil {
		log.Errf("Unable to dump request: %v", err)
	} else {
		log.Debugf("For URL %s, sending:\n%s", o.URL, bytes)
	}
	return req
}

// Client object for making repeated requests of the same URL using the same
// http client (net/http)
type Client struct {
	url       string
	req       *http.Request
	client    *http.Client
	transport *http.Transport
}

// Close cleans up any resources used by NewStdClient
func (c *Client) Close() int {
	log.Debugf("Close() on %+v", c)
	if c.req != nil {
		if c.req.Body != nil {
			if err := c.req.Body.Close(); err != nil {
				log.Warnf("Error closing std client body: %v", err)
			}
		}
		c.req = nil
	}
	if c.transport != nil {
		c.transport.CloseIdleConnections()
	}
	return 0 // TODO: find a way to track std client socket usage.
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
		log.Errf("Unable to send %s request for %s : %v", c.req.Method, c.url, err)
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
	log.Debugf("Got %d : %s for %s %s - response is %d bytes", code, resp.Status, c.req.Method, c.url, len(data))
	return code, data, 0
}

// NewClient creates either a standard or fast client (depending on
// the DisableFastClient flag)
func NewClient(o *HTTPOptions) Fetcher {
	o.Init(o.URL) // For completely new options
	// For changes to options after init
	o.URLSchemeCheck()
	if o.DisableFastClient {
		return NewStdClient(o)
	}
	return NewFastClient(o)
}

// NewStdClient creates a client object that wraps the net/http standard client.
func NewStdClient(o *HTTPOptions) *Client {
	o.Init(o.URL) // also normalizes NumConnections etc to be valid.
	req := newHTTPRequest(o)
	if req == nil {
		return nil
	}
	tr := http.Transport{
		MaxIdleConns:        o.NumConnections,
		MaxIdleConnsPerHost: o.NumConnections,
		DisableCompression:  !o.Compression,
		DisableKeepAlives:   o.DisableKeepAlive,
		Dial: (&net.Dialer{
			Timeout: o.HTTPReqTimeOut,
		}).Dial,
		TLSHandshakeTimeout: o.HTTPReqTimeOut,
	}
	if o.Insecure && o.https {
		log.LogVf("using insecure https")
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // nolint: gas
	}
	client := Client{
		url: o.URL,
		req: req,
		client: &http.Client{
			Timeout:   o.HTTPReqTimeOut,
			Transport: &tr,
		},
		transport: &tr,
	}
	if !o.FollowRedirects {
		// Lets us see the raw response instead of auto following redirects.
		client.client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return &client
}

// FetchURL fetches the data at the given url using the standard client and default options.
// Returns the http status code (http.StatusOK == 200 for success) and the data.
// To be used only for single fetches or when performance doesn't matter as the client is closed at the end.
func FetchURL(url string) (int, []byte) {
	o := NewHTTPOptions(url)
	// Maximize chances of getting the data back, vs the raw payload like the fast client
	o.DisableFastClient = true
	o.FollowRedirects = true
	return Fetch(o)
}

// Fetch creates a client an performs a fetch according to the http options passed in.
// To be used only for single fetches or when performance doesn't matter as the client is closed at the end.
func Fetch(httpOptions *HTTPOptions) (int, []byte) {
	cli := NewClient(httpOptions)
	code, data, _ := cli.Fetch()
	cli.Close()
	return code, data
}

// FastClient is a fast, lockfree single purpose http 1.0/1.1 client.
type FastClient struct {
	buffer       []byte
	req          []byte
	dest         net.Addr
	socket       net.Conn
	socketCount  int
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

// Close cleans up any resources used by FastClient
func (c *FastClient) Close() int {
	log.Debugf("Closing %p %s socket count %d", c, c.url, c.socketCount)
	if c.socket != nil {
		if err := c.socket.Close(); err != nil {
			log.Warnf("Error closing fast client's socket: %v", err)
		}
		c.socket = nil
	}
	return c.socketCount
}

// NewFastClient makes a basic, efficient http 1.0/1.1 client.
// This function itself doesn't need to be super efficient as it is created at
// the beginning and then reused many times.
func NewFastClient(o *HTTPOptions) Fetcher {
	method := o.Method()
	payloadLen := len(o.Payload)
	o.Init(o.URL)
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
	bc := FastClient{url: o.URL, host: url.Host, hostname: url.Hostname(), port: url.Port(),
		http10: o.HTTP10, halfClose: o.AllowHalfClose}
	bc.buffer = make([]byte, BufferSizeKb*1024)
	if bc.port == "" {
		bc.port = url.Scheme // ie http which turns into 80 later
		log.LogVf("No port specified, using %s", bc.port)
	}
	var addr net.Addr
	if o.UnixDomainSocket != "" {
		log.Infof("Using unix domain socket %v instead of %v %v", o.UnixDomainSocket, bc.hostname, bc.port)
		uds := &net.UnixAddr{Name: o.UnixDomainSocket, Net: fnet.UnixDomainSocket}
		addr = uds
	} else {
		addr = fnet.Resolve(bc.hostname, bc.port)
	}
	if addr == nil {
		// Error already logged
		return nil
	}
	bc.dest = addr
	// Create the bytes for the request:
	host := bc.host
	if o.hostOverride != "" {
		host = o.hostOverride
	}
	var buf bytes.Buffer
	buf.WriteString(method + " " + url.RequestURI() + " HTTP/" + proto + "\r\n")
	if !bc.http10 {
		buf.WriteString("Host: " + host + "\r\n")
		bc.parseHeaders = true
		if !o.DisableKeepAlive {
			bc.keepAlive = true
		} else {
			buf.WriteString("Connection: close\r\n")
		}
	}
	bc.reqTimeout = o.HTTPReqTimeOut
	w := bufio.NewWriter(&buf)
	// This writes multiple valued headers properly (unlike calling Get() to do it ourselves)
	o.GenerateHeaders().Write(w) // nolint: errcheck,gas
	w.Flush()                    // nolint: errcheck,gas
	buf.WriteString("\r\n")
	//Add the payload to http body
	if payloadLen > 0 {
		buf.Write(o.Payload)
	}
	bc.req = buf.Bytes()
	log.Debugf("Created client:\n%+v\n%s", bc.dest, bc.req)
	return &bc
}

// return the result from the state.
func (c *FastClient) returnRes() (int, []byte, int) {
	return c.code, c.buffer[:c.size], c.headerLen
}

// connect to destination.
func (c *FastClient) connect() net.Conn {
	c.socketCount++
	socket, err := net.Dial(c.dest.Network(), c.dest.String())
	if err != nil {
		log.Errf("Unable to connect to %v : %v", c.dest, err)
		return nil
	}
	tcpSock, ok := socket.(*net.TCPConn)
	if !ok {
		log.LogVf("Not setting socket options on non tcp socket %v", socket.RemoteAddr())
		return socket
	}
	// For now those errors are not critical/breaking
	if err = tcpSock.SetNoDelay(true); err != nil {
		log.Warnf("Unable to connect to set tcp no delay %v %v : %v", socket, c.dest, err)
	}
	if err = tcpSock.SetWriteBuffer(len(c.req)); err != nil {
		log.Warnf("Unable to connect to set write buffer %d %v %v : %v", len(c.req), socket, c.dest, err)
	}
	if err = tcpSock.SetReadBuffer(len(c.buffer)); err != nil {
		log.Warnf("Unable to connect to read buffer %d %v %v : %v", len(c.buffer), socket, c.dest, err)
	}
	return socket
}

// Extra error codes outside of the HTTP Status code ranges. ie negative.
const (
	// SocketError is return when a transport error occurred: unexpected EOF, connection error, etc...
	SocketError = -1
	// RetryOnce is used internally as an error code to allow 1 retry for bad socket reuse.
	RetryOnce = -2
)

// Fetch fetches the url content. Returns http code, data, offset of body.
func (c *FastClient) Fetch() (int, []byte, int) {
	c.code = SocketError
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
		log.Debugf("Reusing socket %v", conn)
	}
	c.socket = nil // because of error returns and single retry
	conErr := conn.SetReadDeadline(time.Now().Add(c.reqTimeout))
	// Send the request:
	n, err := conn.Write(c.req)
	if err != nil || conErr != nil {
		if reuse {
			// it's ok for the (idle) socket to die once, auto reconnect:
			log.Infof("Closing dead socket %v (%v)", conn, err)
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
		tcpConn, ok := conn.(*net.TCPConn)
		if ok {
			if err = tcpConn.CloseWrite(); err != nil {
				log.Errf("Unable to close write to %v %v : %v", conn, c.dest, err)
				return c.returnRes()
			} // else:
			log.Debugf("Half closed ok after sending request %v %v", conn, c.dest)
		} else {
			log.Warnf("Unable to close write non tcp connection %v", conn)
		}
	}
	// Read the response:
	c.readResponse(conn, reuse)
	if c.code == RetryOnce {
		// Special "eof on reused socket" code
		return c.Fetch() // recurse once
	}
	// Return the result:
	return c.returnRes()
}

// Response reading:
// TODO: refactor - unwiedly/ugly atm
func (c *FastClient) readResponse(conn net.Conn, reusedSocket bool) {
	max := len(c.buffer)
	parsedHeaders := false
	// TODO: safer to start with -1 / SocketError and fix ok for http 1.0
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
			if err != nil {
				if reusedSocket && c.size == 0 {
					// Ok for reused socket to be dead once (close by server)
					log.Infof("Closing dead socket %v (err %v at first read)", conn, err)
					c.errorCount++
					err = conn.Close() // close the previous one
					if err != nil {
						log.Warnf("Error closing dead socket %v: %v", conn, err)
					}
					c.code = RetryOnce // special "retry once" code
					return
				}
				if err == io.EOF && c.size != 0 {
					// handled below as possibly normal end of stream after we read something
					break
				}
				log.Errf("Read error %v %v %d : %v", conn, c.dest, c.size, err)
				c.code = SocketError
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
							if contentLength == -1 {
								// chunk length not available yet
								log.LogVf("chunk mode but no first chunk length yet, reading more")
								max = c.headerLen
								continue
							}
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

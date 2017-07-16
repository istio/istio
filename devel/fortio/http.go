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
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// Fetcher is the Url content fetcher that the different client implements.
type Fetcher interface {
	// Fetch returns http code, data, offset of body (for client which returns
	// headers)
	Fetch() (int, []byte, int)
}

// ExtraHeaders to be added to each request.
var extraHeaders http.Header

// Host is treated specially, remember that one separately.
var hostOverride string

func init() {
	extraHeaders = make(http.Header)
	extraHeaders.Add("User-Agent", userAgent)
}

// Verbosity controls verbose/debug output level, higher more verbose.
var Verbosity int

// Version is the fortio package version (TODO:auto gen/extract).
const (
	Version       = "0.1"
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
	// Not checking Verbosity as this is called during flag parsing and Verbosity isn't set yet
	if strings.EqualFold(key, "host") {
		log.Printf("Will be setting special Host header to %s", value)
		hostOverride = value
	} else {
		log.Printf("Setting regular extra header %s: %s", key, value)
		extraHeaders.Add(key, value)
	}
	return nil
}

// newHttpRequest makes a new http GET request for url with User-Agent.
func newHTTPRequest(url string) *http.Request {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("Unable to make request for %s : %v", url, err)
		return nil
	}
	req.Header = extraHeaders
	if hostOverride != "" {
		req.Host = hostOverride
	}
	if Verbosity < 3 {
		return req
	}
	bytes, err := httputil.DumpRequestOut(req, false)
	if err != nil {
		log.Printf("Unable to dump request %v", err)
	} else {
		log.Printf("For URL %s, sending:\n%s", url, bytes)
	}
	return req
}

// Client object for making repeated requests of the same URL using the same
// http client
type Client struct {
	url    string
	req    *http.Request
	client *http.Client
}

// FetchURL fetches URL contenty and does error handling/logging.
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
		log.Printf("Unable to send request for %s : %v", c.url, err)
		return http.StatusBadRequest, []byte(err.Error()), 0
	}
	var data []byte
	if Verbosity > 2 {
		if data, err = httputil.DumpResponse(resp, false); err != nil {
			log.Printf("Unable to dump response %v", err)
		} else {
			log.Printf("For URL %s, received:\n%s", c.url, data)
		}
	}
	data, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close() //nolint(errcheck)
	if err != nil {
		log.Printf("Unable to read response for %s : %v", c.url, err)
		code := resp.StatusCode
		if code == http.StatusOK {
			code = http.StatusNoContent
			log.Printf("Ok code despite read error, switching code to %d", code)
		}
		return code, data, 0
	}
	code := resp.StatusCode
	if Verbosity > 1 {
		log.Printf("Got %d : %s for %s - response is %d bytes", code, resp.Status, c.url, len(data))
	}
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
			Timeout: 3 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        numConnections,
				MaxIdleConnsPerHost: numConnections,
				DisableCompression:  !compression,
				Dial: (&net.Dialer{
					Timeout: 1 * time.Second,
				}).Dial,
				TLSHandshakeTimeout: 1 * time.Second,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	return &client
}

// BasicClient is a fast, lockfree single purpose http 1.0 client.
type BasicClient struct {
	buffer       [16384]byte // first for alignment reasons
	req          []byte
	dest         net.TCPAddr
	socket       *net.TCPConn
	errorCount   int
	url          string
	host         string
	hostname     string
	port         string
	http10       bool // http 1.0, simplest: no Host, forced no keepAlive, no parsing
	keepAlive    bool
	parseHeaders bool // don't bother in http/1.0
}

// NewBasicClient makes a basic, efficient http 1.0/1.1 client.
// This doesn't need to be super efficient as it's created at the beginning and
// reused many times.
func NewBasicClient(urlStr string, proto string, keepAlive bool) Fetcher {
	url, err := url.Parse(urlStr)
	if err != nil {
		log.Printf("Bad url '%s' : %v", urlStr, err)
		return nil
	}
	if url.Scheme != "http" {
		log.Printf("Only http is supported, can't use url %s", urlStr)
		return nil
	}
	// note: Host includes the port
	bc := BasicClient{url: urlStr, host: url.Host, hostname: url.Hostname(), port: url.Port(), http10: (proto == "1.0")}
	if bc.port == "" {
		bc.port = url.Scheme // ie http which turns into 80 later
		if Verbosity > 2 {
			log.Printf("No port specified, using %s", bc.port)
		}
	}
	addrs, err := net.LookupIP(bc.hostname)
	if err != nil {
		log.Printf("Unable to lookup '%s' : %v", bc.host, err)
		return nil
	}
	if len(addrs) > 1 && Verbosity > 2 {
		log.Printf("Using only the first of the addresses for %s : %v", bc.host, addrs)
	}
	if Verbosity > 2 {
		log.Printf("Will go to %s", addrs[0])
	}
	bc.dest.IP = addrs[0]
	bc.dest.Port, err = net.LookupPort("tcp", bc.port)
	if err != nil {
		log.Printf("Unable to resolve port '%s' : %v", bc.port, err)
		return nil
	}
	// Bytes:
	host := bc.host
	if hostOverride != "" {
		host = hostOverride
	}
	bc.req = []byte("GET " + url.RequestURI() + " HTTP/" + proto + "\r\n")
	if !bc.http10 {
		bc.req = append(bc.req, []byte("Host: "+host+"\r\n")...)
		bc.parseHeaders = true
		if keepAlive {
			bc.keepAlive = true
		} else {
			bc.req = append(bc.req, []byte("Connection: close\r\n")...)
		}
	}
	for h := range extraHeaders {
		// TODO: ugly ... what's a good/elegant and efficient way to do this
		bc.req = append(bc.req, []byte(h)...)
		bc.req = append(bc.req, ':', ' ')
		bc.req = append(bc.req, []byte(extraHeaders.Get(h))...)
		bc.req = append(bc.req, '\r', '\n')
	}
	bc.req = append(bc.req, '\r', '\n')
	if Verbosity > 2 {
		log.Printf("Created client:\n%+v\n%s", bc.dest, string(bc.req))
	}
	return &bc
}

const toUpperMask = ^byte('a' - 'A')

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
	if Verbosity > 0 && numChars != len(str) {
		log.Printf("ASCIIFold(\"%s\") contains %d characters, some non ascii (byte length %d): will mangle", str, numChars, len(str))
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

// ParseDecimal extract the first positive integer number from the input.
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

// case doesn't matter
var contentLengthHeader = []byte("\r\ncontent-length:")
var connectionClose = []byte("\r\nconnection: close")

// Fetch fetches the url content. Returns http code, data, offset of body.
func (c *BasicClient) Fetch() (int, []byte, int) {
	var data []byte
	code := -1
	conn := c.socket
	reuse := (conn != nil)
	if !reuse {
		socket, err := net.DialTCP("tcp", nil, &c.dest)
		conn = socket
		if err != nil {
			log.Printf("Unable to connect to %v : %v", c.dest, err)
			return code, data, 0
		}
		if err = conn.SetNoDelay(true); err != nil {
			log.Printf("Unable to connect to set tcp no delay %v %v : %v", conn, c.dest, err)
		}
		if err = conn.SetWriteBuffer(len(c.req)); err != nil {
			log.Printf("Unable to connect to set write buffer %d %v %v : %v", len(c.req), conn, c.dest, err)
		}
		if err = conn.SetReadBuffer(len(c.buffer)); err != nil {
			log.Printf("Unable to connect to read buffer %d %v %v : %v", len(c.buffer), conn, c.dest, err)
		}
	} else {
		if Verbosity > 1 {
			log.Printf("Reusing socket %v", *conn)
		}
	}
	c.socket = nil // because of error returns
	n, err := conn.Write(c.req)
	if err != nil {
		if reuse {
			// it's ok it died once
			if Verbosity > 0 {
				log.Printf("Closing dead socket %v (%v)", *conn, err)
			}
			conn.Close() // nolint: errcheck
			c.errorCount++
			return c.Fetch()
		}
		log.Printf("Unable to write to %v %v : %v", conn, c.dest, err)
		return code, data, 0
	}
	if n != len(c.req) {
		log.Printf("Short write to %v %v : %d instead of %d", conn, c.dest, n, len(c.req))
		return code, data, 0
	}
	if !c.keepAlive {
		if err = conn.CloseWrite(); err != nil {
			log.Printf("Unable to close write to %v %v : %v", conn, c.dest, err)
			return code, data, 0
		}
	}
	size := 0
	max := len(c.buffer)
	parsedHeaders := false
	code = http.StatusOK // In http 1.0 mode we don't bother parsing anything
	endofHeadersStart := retcodeOffset + 3
	lastCRLF := 0
	keepAlive := c.keepAlive
	for {
		n, err = conn.Read(c.buffer[size:])
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Read error %v %v %d : %v", conn, c.dest, size, err)
		}
		if Verbosity > 2 {
			log.Print("Read ok ", n)
		}
		size += n
		if !parsedHeaders && c.parseHeaders {
			// enough to get the code?
			if size >= retcodeOffset+3 {
				// even if the bytes are garbage we'll get a non 200 code (bytes are unsigned)
				code = ParseDecimal(c.buffer[retcodeOffset : retcodeOffset+3])
				if code != http.StatusOK {
					log.Printf("Parsed non ok code %d (%v)", code, string(c.buffer[:retcodeOffset+3]))
					break
				}
				if Verbosity > 2 {
					log.Printf("Code %d, looking for end of headers at %d / %d, last CRLF %d",
						code, endofHeadersStart, size, lastCRLF)
				}
				idx := endofHeadersStart
				for idx < size-1 {
					if c.buffer[idx] == '\r' && c.buffer[idx+1] == '\n' {
						if lastCRLF == idx-2 { // found end of headers
							parsedHeaders = true
							break
						}
						lastCRLF = idx
						idx++
					}
					idx++
				}
				endofHeadersStart = size // start there next read
				if parsedHeaders {
					// We have headers !
					lastCRLF += 4 // we use this and not endofHeadersStart so http/1.0 does return 0 and not the optimization for search start
					if Verbosity > 2 {
						log.Printf("headers are %s", string(c.buffer[:idx]))
					}
					// Find the content length
					if keepAlive {
						found, offset := FoldFind(c.buffer[:lastCRLF], contentLengthHeader)
						if !found {
							if Verbosity > 1 {
								log.Printf("Warning: content-length missing in %s", string(c.buffer[:lastCRLF]))
							} else {
								log.Printf("Warning: content-length missing (%d bytes headers)", lastCRLF)
							}
							keepAlive = false
							break
						}
						contentLength := ParseDecimal(c.buffer[offset+len(contentLengthHeader) : lastCRLF])
						if contentLength < 0 {
							log.Printf("Warning: content-length unparsable %s", string(c.buffer[offset+2:offset+len(contentLengthHeader)+4]))
							keepAlive = false
							break

						}
						max = lastCRLF + contentLength
						if Verbosity > 1 {
							log.Printf("found content length %d", contentLength)
						}
						if max > len(c.buffer) {
							log.Printf("Buffer is too small for headers %d + data %d",
								lastCRLF, contentLength)
							// TODO: just consume the extra instead
							max = len(c.buffer)
						}
						found, _ = FoldFind(c.buffer[:lastCRLF], connectionClose)
						if found {
							log.Printf("Server wants to close connection, no keep-alive!")
							keepAlive = false
						}
					}
				}
			}
		}
		if size == max {
			if !keepAlive {
				log.Printf("More data is available but stopping after %d (for %v %v)", max, conn, c.dest)
			}
			break
		}
	}
	if keepAlive && code == http.StatusOK {
		c.socket = conn
	} else {
		if err = conn.Close(); err != nil {
			log.Printf("Close error %v %v %d : %v", conn, c.dest, size, err)
		}
		// we cleared c.socket already
	}
	return code, c.buffer[:size], lastCRLF
}

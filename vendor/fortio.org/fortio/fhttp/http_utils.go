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
	"encoding/base64"
	"fmt"
	"html/template"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"fortio.org/fortio/fnet"
	"fortio.org/fortio/log"
	"fortio.org/fortio/stats"
)

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
// Only works for ASCII, not meant for unicode.
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

// -- server utils

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

// generateSize from string, format: "size=512" for 100% 512 bytes body replies,
// size="512:20,16384:10" for 20% 512 bytes, 10% 16k, 70% default echo back.
// returns -1 for the default case, so one can specify 0 and force no payload
// even if it's a post request with a payload (to test asymmetric large inbound
// small outbound).
// TODO: refactor similarities with status and delay
func generateSize(sizeInput string) (size int) {
	size = -1 // default value/behavior
	if len(sizeInput) == 0 {
		return size
	}
	lst := strings.Split(sizeInput, ",")
	log.Debugf("Parsing size %s -> %v", sizeInput, lst)
	// Simple non probabilistic status case:
	if len(lst) == 1 && !strings.ContainsRune(sizeInput, ':') {
		s, err := strconv.Atoi(sizeInput)
		if err != nil {
			log.Warnf("Bad input size %v, not a number nor comma and colon separated %% list", sizeInput)
			return size
		}
		size = s
		log.Debugf("Parsed size %s -> %d", sizeInput, size)
		fnet.ValidatePayloadSize(&size)
		return size
	}
	weights := make([]float32, len(lst))
	sizes := make([]int, len(lst))
	lastPercent := float64(0)
	i := 0
	for _, entry := range lst {
		l2 := strings.Split(entry, ":")
		if len(l2) != 2 {
			log.Warnf("Should have exactly 1 : in size list %s -> %v", sizeInput, entry)
			return size
		}
		s, err := strconv.Atoi(l2[0])
		if err != nil {
			log.Warnf("Bad input size %v -> %v, not a number before colon", sizeInput, l2[0])
			return size
		}
		fnet.ValidatePayloadSize(&s)
		percStr := removeTrailingPercent(l2[1])
		p, err := strconv.ParseFloat(percStr, 32)
		if err != nil || p < 0 || p > 100 {
			log.Warnf("Percentage is not a [0. - 100.] number in %v -> %v : %v %f", sizeInput, percStr, err, p)
			return size
		}
		lastPercent += p
		// Round() needed to cover 'exactly' 100% and not more or less because of rounding errors
		p32 := float32(stats.Round(lastPercent))
		if p32 > 100. {
			log.Warnf("Sum of percentage is greater than 100 in %v %f %f %f", sizeInput, lastPercent, p, p32)
			return size
		}
		weights[i] = p32
		sizes[i] = s
		i++
	}
	res := 100. * rand.Float32()
	for i, v := range weights {
		if res <= v {
			log.Debugf("[0.-100.[ for %s roll %f got #%d -> %d", sizeInput, res, i, sizes[i])
			return sizes[i]
		}
	}
	log.Debugf("[0.-100.[ for %s roll %f no hit, defaulting to -1", sizeInput, res)
	return size // default/reminder of probability table
}

// MaxDelay is the maximum delay allowed for the echoserver responses.
// 1.5s so we can test the default 1s timeout in envoy.
const MaxDelay = 1500 * time.Millisecond

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

// RoundDuration rounds to 10th of second. Only for positive durations.
// TODO: switch to Duration.Round once switched to go 1.9
func RoundDuration(d time.Duration) time.Duration {
	tenthSec := int64(100 * time.Millisecond)
	r := int64(d+50*time.Millisecond) / tenthSec
	return time.Duration(tenthSec * r)
}

// -- formerly in uihandler:

// HTMLEscapeWriter is an io.Writer escaping the output for safe html inclusion.
type HTMLEscapeWriter struct {
	NextWriter io.Writer
	Flusher    http.Flusher
}

func (w *HTMLEscapeWriter) Write(p []byte) (int, error) {
	template.HTMLEscape(w.NextWriter, p)
	if w.Flusher != nil {
		w.Flusher.Flush()
	}
	return len(p), nil
}

// NewHTMLEscapeWriter creates a io.Writer that can safely output
// to an http.ResponseWrite with HTMLEscape-ing.
func NewHTMLEscapeWriter(w io.Writer) io.Writer {
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Errf("expected writer %+v to be an http.ResponseWriter and thus a http.Flusher", w)
		flusher = nil
	}
	return &HTMLEscapeWriter{NextWriter: w, Flusher: flusher}
}

// OnBehalfOf adds a header with the remote addr to an http options object.
func OnBehalfOf(o *HTTPOptions, r *http.Request) {
	_ = o.AddAndValidateExtraHeader("X-On-Behalf-Of: " + r.RemoteAddr)
}

// AddHTTPS replaces "http://" in url with "https://" or prepends "https://"
// if url does not contain prefix "http://".
func AddHTTPS(url string) string {
	if len(url) > len(fnet.PrefixHTTP) {
		if strings.EqualFold(url[:len(fnet.PrefixHTTP)], fnet.PrefixHTTP) {
			log.Infof("Replacing http scheme with https for url: %s", url)
			return fnet.PrefixHTTPS + url[len(fnet.PrefixHTTP):]
		}
		// returns url with normalized lowercase https prefix
		if strings.EqualFold(url[:len(fnet.PrefixHTTPS)], fnet.PrefixHTTPS) {
			return fnet.PrefixHTTPS + url[len(fnet.PrefixHTTPS):]
		}
	}
	// url must not contain any prefix, so add https prefix
	log.Infof("Prepending https:// to url: %s", url)
	return fnet.PrefixHTTPS + url
}

// generateBase64UserCredentials encodes the user credential to base64 and adds a Basic as prefix.
func generateBase64UserCredentials(userCredentials string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(userCredentials))
}

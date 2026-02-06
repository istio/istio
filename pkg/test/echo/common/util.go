//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package common

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"

	"istio.io/istio/pkg/test/echo/proto"
)

func init() {
	flag.DurationVar(&DefaultRequestTimeout, "istio.test.echo.requestTimeout", DefaultRequestTimeout,
		"Specifies the timeout for individual ForwardEcho calls.")
}

var DefaultRequestTimeout = 5 * time.Second

const (
	ConnectionTimeout = 2 * time.Second
	DefaultCount      = 1
)

// FillInDefaults fills in the timeout and count if not specified in the given message.
func FillInDefaults(request *proto.ForwardEchoRequest) {
	request.TimeoutMicros = DurationToMicros(GetTimeout(request))
	request.Count = int32(GetCount(request))
}

// GetTimeout returns the timeout value as a time.Duration or DefaultRequestTimeout if not set.
func GetTimeout(request *proto.ForwardEchoRequest) time.Duration {
	timeout := MicrosToDuration(request.TimeoutMicros)
	if timeout == 0 {
		timeout = DefaultRequestTimeout
	}
	return timeout
}

// GetCount returns the count value or DefaultCount if not set.
func GetCount(request *proto.ForwardEchoRequest) int {
	if request.Count > 1 {
		return int(request.Count)
	}
	return DefaultCount
}

func HTTPToProtoHeaders(headers http.Header) []*proto.Header {
	out := make([]*proto.Header, 0, len(headers))
	for k, v := range headers {
		out = append(out, &proto.Header{Key: k, Value: strings.Join(v, ",")})
	}
	return out
}

func ProtoToHTTPHeaders(headers []*proto.Header) http.Header {
	out := make(http.Header)
	for _, h := range headers {
		// Avoid using .Add() to allow users to pass non-canonical forms
		out[h.Key] = append(out[h.Key], h.Value)
	}
	return out
}

// GetHeaders returns the headers for the message.
func GetHeaders(request *proto.ForwardEchoRequest) http.Header {
	return ProtoToHTTPHeaders(request.Headers)
}

// MicrosToDuration converts the given microseconds to a time.Duration.
func MicrosToDuration(micros int64) time.Duration {
	return time.Duration(micros) * time.Microsecond
}

// DurationToMicros converts the given duration to microseconds.
func DurationToMicros(d time.Duration) int64 {
	return int64(d / time.Microsecond)
}

// ParseTLSVersion parses a TLS version string and returns the corresponding tls constant.
func ParseTLSVersion(version string) (uint16, error) {
	switch version {
	case "":
		return tls.VersionTLS12, nil
	case "1.0":
		return tls.VersionTLS10, nil
	case "1.1":
		return tls.VersionTLS11, nil
	case "1.2":
		return tls.VersionTLS12, nil
	case "1.3":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("unsupported TLS version: %s", version)
	}
}

// ParseTLSCurves parses a list of curve names and returns the corresponding tls.CurveID values.
func ParseTLSCurves(curves []string) ([]tls.CurveID, error) {
	if len(curves) == 0 {
		return nil, nil
	}
	var result []tls.CurveID
	for _, curve := range curves {
		switch curve {
		case "P-256":
			result = append(result, tls.CurveP256)
		case "P-384":
			result = append(result, tls.CurveP384)
		case "P-521":
			result = append(result, tls.CurveP521)
		case "X25519":
			result = append(result, tls.X25519)
		case "X25519MLKEM768":
			result = append(result, tls.X25519MLKEM768)
		default:
			return nil, fmt.Errorf("unsupported TLS curve: %s", curve)
		}
	}
	return result, nil
}

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

package runtime

import (
	"fmt"
	"net"
	"strings"
	"time"

	"istio.io/istio/mixer/pkg/il/interpreter"
)

// Externs contains the list of standard external functions used during evaluation.
var Externs = map[string]interpreter.Extern{
	"ip":              interpreter.ExternFromFn("ip", externIp),
	"ip_equal":        interpreter.ExternFromFn("ip_equal", externIpEqual),
	"timestamp":       interpreter.ExternFromFn("timestamp", externTimestamp),
	"timestamp_equal": interpreter.ExternFromFn("timestamp_equal", externTimestampEqual),
	"match":           interpreter.ExternFromFn("match", externMatch),
}

func externIp(in string) ([]byte, error) {
	if ip := net.ParseIP(in); ip != nil {
		return []byte(ip), nil
	}
	return []byte{}, fmt.Errorf("could not convert %s to IP_ADDRESS", in)
}

func externIpEqual(a []byte, b []byte) bool {
	// net.IP is an alias for []byte, so these are safe to convert
	ip1 := net.IP(a)
	ip2 := net.IP(b)
	return ip1.Equal(ip2)
}

func externTimestamp(in string) (time.Time, error) {
	layout := time.RFC3339
	t, err := time.Parse(layout, in)
	if err != nil {
		return time.Time{}, fmt.Errorf("could not convert '%s' to TIMESTAMP. expected format: '%s'", in, layout)
	}
	return t, nil
}

func externTimestampEqual(t1 time.Time, t2 time.Time) bool {
	return t1.Equal(t2)
}

func externMatch(str string, pattern string) bool {
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(str, pattern[:len(pattern)-1])
	}
	if strings.HasPrefix(pattern, "*") {
		return strings.HasSuffix(str, pattern[1:])
	}
	return str == pattern
}

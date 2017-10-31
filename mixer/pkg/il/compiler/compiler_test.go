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

package compiler

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"istio.io/istio/mixer/pkg/config/descriptor"
	"istio.io/istio/mixer/pkg/il/interpreter"
	ilt "istio.io/istio/mixer/pkg/il/testing"
	"istio.io/istio/mixer/pkg/il/text"
)

func TestCompile(t *testing.T) {

	for i, test := range ilt.TestData {
		if test.E == "" {
			continue
		}

		conf := test.Conf
		if conf == nil {
			conf = ilt.TestConfigs["Default"]
		}
		finder := descriptor.NewFinder(conf)

		name := fmt.Sprintf("%d '%s'", i, test.E)
		t.Run(name, func(tt *testing.T) {
			result, err := Compile(test.E, finder)
			if err != nil {
				if err.Error() != test.CompileErr {
					tt.Fatalf("Unexpected error: '%s' != '%s'", err.Error(), test.CompileErr)
				}
				return
			}

			if test.CompileErr != "" {
				tt.Fatalf("expected error not found: '%s'", test.CompileErr)
				return
			}

			if test.IL != "" {
				actual := text.WriteText(result.Program)
				if strings.TrimSpace(actual) != strings.TrimSpace(test.IL) {
					tt.Log("===== EXPECTED ====\n")
					tt.Log(test.IL)
					tt.Log("\n====== ACTUAL =====\n")
					tt.Log(actual)
					tt.Log("===================\n")
					tt.Fail()
					return
				}
			}

			input := test.I
			if input == nil {
				input = map[string]interface{}{}
			}
			b := ilt.FakeBag{Attrs: input}

			// TODO(ozben): Rationalize the placement of externs.
			var ipExternFn = interpreter.ExternFromFn("ip", func(in string) ([]byte, error) {
				if ip := net.ParseIP(in); ip != nil {
					return []byte(ip), nil
				}
				return []byte{}, fmt.Errorf("could not convert %s to IP_ADDRESS", in)
			})

			var ipEqualExternFn = interpreter.ExternFromFn("ip_equal", func(a []byte, b []byte) bool {
				// net.IP is an alias for []byte, so these are safe to convert
				ip1 := net.IP(a)
				ip2 := net.IP(b)
				return ip1.Equal(ip2)
			})

			var timestampExternFn = interpreter.ExternFromFn("timestamp", func(in string) (time.Time, error) {
				layout := time.RFC3339
				t, err := time.Parse(layout, in)
				if err != nil {
					return time.Time{}, fmt.Errorf("could not convert '%s' to TIMESTAMP. expected format: '%s'", in, layout)
				}
				return t, nil
			})

			var timestampEqualExternFn = interpreter.ExternFromFn("timestamp_equal", func(t1 time.Time, t2 time.Time) bool {
				return t1.Equal(t2)
			})

			var matchExternFn = interpreter.ExternFromFn("match", func(str string, pattern string) bool {
				if strings.HasSuffix(pattern, "*") {
					return strings.HasPrefix(str, pattern[:len(pattern)-1])
				}
				if strings.HasPrefix(pattern, "*") {
					return strings.HasSuffix(str, pattern[1:])
				}
				return str == pattern
			})

			externMap := map[string]interpreter.Extern{
				"ip":              ipExternFn,
				"ip_equal":        ipEqualExternFn,
				"timestamp":       timestampExternFn,
				"timestamp_equal": timestampEqualExternFn,
				"match":           matchExternFn,
			}

			i := interpreter.New(result.Program, externMap)
			v, err := i.Eval("eval", &b)
			if err != nil {
				if test.Err != err.Error() {
					tt.Fatalf("expected error not found: E:'%v', A:'%v'", test.Err, err)
				}
				return
			}
			if test.Err != "" {
				tt.Fatalf("expected error not received: '%v'", test.Err)
			}

			if !ilt.AreEqual(test.R, v.AsInterface()) {
				tt.Fatalf("Result match failed: %+v == %+v", test.R, v.AsInterface())
			}
		})
	}
}

// Copyright Istio Authors
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

package tracing

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestOpts(t *testing.T) {
	cases := []struct {
		cmdLine string
		result  Options
	}{
		{"--trace_zipkin_url ZIP", Options{
			ZipkinURL: "ZIP",
		}},

		{"--trace_jaeger_url JAEGER", Options{
			JaegerURL: "JAEGER",
		}},

		{"--trace_log_spans", Options{
			LogTraceSpans: true,
		}},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			o := DefaultOptions()
			cmd := &cobra.Command{}
			o.AttachCobraFlags(cmd)
			cmd.SetArgs(strings.Split(c.cmdLine, " "))

			if err := cmd.Execute(); err != nil {
				t.Errorf("Got %v, expecting success", err)
			}

			if !reflect.DeepEqual(c.result, *o) {
				t.Errorf("Got %v, expected %v", *o, c.result)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	o := DefaultOptions()
	o.JaegerURL = "https://foo"
	o.ZipkinURL = "https://bar"

	if o.Validate() == nil {
		t.Error("Validate() did not produce expected failure")
	}

	o.JaegerURL = ""

	o.SamplingRate = 14.45
	if o.Validate() == nil {
		t.Error("Validate() did not produce expected failure for invalid sampling rate")
	}

	o.SamplingRate = -1.0234
	if o.Validate() == nil {
		t.Error("Validate() did not produce expected failure for invalid sampling rate")
	}

}

func TestTracingEnabled(t *testing.T) {
	o := DefaultOptions()

	if o.TracingEnabled() {
		t.Fatal("default arg values should not have enabled tracing")
	}

	o = DefaultOptions()
	o.LogTraceSpans = true
	if !o.TracingEnabled() {
		t.Fatal("LogTraceSpans should have triggered tracing")
	}

	o = DefaultOptions()
	o.ZipkinURL = "http://foo.bar.com"
	if !o.TracingEnabled() {
		t.Fatal("ZipkinURL should have triggered tracing")
	}

	o = DefaultOptions()
	o.JaegerURL = "http://foo.bar.com"
	if !o.TracingEnabled() {
		t.Fatal("JaegerURL should have triggered tracing")
	}
}

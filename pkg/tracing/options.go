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
	"errors"

	"github.com/spf13/cobra"
)

// Options defines the set of options supported by Istio's component tracing package.
type Options struct {
	// URL of zipkin collector (example: 'http://zipkin:9411/api/v1/spans').
	ZipkinURL string

	// URL of jaeger HTTP collector (example: 'http://jaeger:14268/api/traces?format=jaeger.thrift').
	JaegerURL string

	// Whether or not to emit trace spans as log records.
	LogTraceSpans bool

	// SamplingRate controls the rate at which a process will decide to generate trace spans.
	SamplingRate float64
}

// DefaultOptions returns a new set of options, initialized to the defaults
func DefaultOptions() *Options {
	return &Options{}
}

// Validate returns whether the options have been configured correctly or an error
func (o *Options) Validate() error {
	// due to a race condition in the OT libraries somewhere, we can't have both tracing outputs active at once
	if o.JaegerURL != "" && o.ZipkinURL != "" {
		return errors.New("can't have Jaeger and Zipkin outputs active simultaneously")
	}

	if o.SamplingRate > 1.0 || o.SamplingRate < 0.0 {
		return errors.New("sampling rate must be in the range: [0.0, 1.0]")
	}

	return nil
}

// TracingEnabled returns whether the given options enable tracing to take place.
func (o *Options) TracingEnabled() bool {
	return o.JaegerURL != "" || o.ZipkinURL != "" || o.LogTraceSpans
}

// AttachCobraFlags attaches a set of Cobra flags to the given Cobra command.
//
// Cobra is the command-line processor that Istio uses. This command attaches
// the necessary set of flags to expose a CLI to let the user control all
// tracing options.
func (o *Options) AttachCobraFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVarP(&o.ZipkinURL, "trace_zipkin_url", "", o.ZipkinURL,
		"URL of Zipkin collector (example: 'http://zipkin:9411/api/v1/spans').")

	cmd.PersistentFlags().StringVarP(&o.JaegerURL, "trace_jaeger_url", "", o.JaegerURL,
		"URL of Jaeger HTTP collector (example: 'http://jaeger:14268/api/traces?format=jaeger.thrift').")

	cmd.PersistentFlags().BoolVarP(&o.LogTraceSpans, "trace_log_spans", "", o.LogTraceSpans,
		"Whether or not to log trace spans.")

	cmd.PersistentFlags().Float64VarP(&o.SamplingRate, "trace_sampling_rate", "", o.SamplingRate,
		"Sampling rate for generating trace data. Must be a value in the range [0.0, 1.0].")
}

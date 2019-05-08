// Copyright 2019 The OpenZipkin Authors
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

package zipkin

import (
	"errors"

	"github.com/openzipkin/zipkin-go/idgenerator"
	"github.com/openzipkin/zipkin-go/model"
)

// Tracer Option Errors
var (
	ErrInvalidEndpoint             = errors.New("requires valid local endpoint")
	ErrInvalidExtractFailurePolicy = errors.New("invalid extract failure policy provided")
)

// ExtractFailurePolicy deals with Extraction errors
type ExtractFailurePolicy int

// ExtractFailurePolicyOptions
const (
	ExtractFailurePolicyRestart ExtractFailurePolicy = iota
	ExtractFailurePolicyError
	ExtractFailurePolicyTagAndRestart
)

// TracerOption allows for functional options to adjust behavior of the Tracer
// to be created with NewTracer().
type TracerOption func(o *Tracer) error

// WithLocalEndpoint sets the local endpoint of the tracer.
func WithLocalEndpoint(e *model.Endpoint) TracerOption {
	return func(o *Tracer) error {
		if e == nil {
			o.localEndpoint = nil
			return nil
		}
		ep := *e
		o.localEndpoint = &ep
		return nil
	}
}

// WithExtractFailurePolicy allows one to set the ExtractFailurePolicy.
func WithExtractFailurePolicy(p ExtractFailurePolicy) TracerOption {
	return func(o *Tracer) error {
		if p < 0 || p > ExtractFailurePolicyTagAndRestart {
			return ErrInvalidExtractFailurePolicy
		}
		o.extractFailurePolicy = p
		return nil
	}
}

// WithNoopSpan if set to true will switch to a NoopSpan implementation
// if the trace is not sampled.
func WithNoopSpan(unsampledNoop bool) TracerOption {
	return func(o *Tracer) error {
		o.unsampledNoop = unsampledNoop
		return nil
	}
}

// WithSharedSpans allows to place client-side and server-side annotations
// for a RPC call in the same span (Zipkin V1 behavior) or different spans
// (more in line with other tracing solutions). By default this Tracer
// uses shared host spans (so client-side and server-side in the same span).
func WithSharedSpans(val bool) TracerOption {
	return func(o *Tracer) error {
		o.sharedSpans = val
		return nil
	}
}

// WithSampler allows one to set a Sampler function
func WithSampler(sampler Sampler) TracerOption {
	return func(o *Tracer) error {
		o.sampler = sampler
		return nil
	}
}

// WithTraceID128Bit if set to true will instruct the Tracer to start traces
// with 128 bit TraceID's. If set to false the Tracer will start traces with
// 64 bits.
func WithTraceID128Bit(val bool) TracerOption {
	return func(o *Tracer) error {
		if val {
			o.generate = idgenerator.NewRandom128()
		} else {
			o.generate = idgenerator.NewRandom64()
		}
		return nil
	}
}

// WithIDGenerator allows one to set a custom ID Generator
func WithIDGenerator(generator idgenerator.IDGenerator) TracerOption {
	return func(o *Tracer) error {
		o.generate = generator
		return nil
	}
}

// WithTags allows one to set default tags to be added to each created span
func WithTags(tags map[string]string) TracerOption {
	return func(o *Tracer) error {
		for k, v := range tags {
			o.defaultTags[k] = v
		}
		return nil
	}
}

// WithNoopTracer allows one to start the Tracer as Noop implementation.
func WithNoopTracer(tracerNoop bool) TracerOption {
	return func(o *Tracer) error {
		if tracerNoop {
			o.noop = 1
		} else {
			o.noop = 0
		}
		return nil
	}
}

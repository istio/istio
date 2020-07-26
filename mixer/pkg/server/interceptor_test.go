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

package server

import (
	"context"
	"testing"

	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/mocktracer"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestNoB3SampledFalse(t *testing.T) {
	assert.False(t, isSampled(metadata.New(nil)))
}

func TestB3SampledTrue(t *testing.T) {
	assert.True(t, isSampled(metadata.New(map[string]string{"x-b3-sampled": "1"})))
}

func TestB3SampledTrueAgain(t *testing.T) {
	assert.True(t, isSampled(metadata.New(map[string]string{"x-b3-sampled": "true"})))
}

func TestB3SampledFalse(t *testing.T) {
	assert.False(t, isSampled(metadata.New(map[string]string{"x-b3-sampled": "0"})))
}

func TestB3SampledCaseFalse(t *testing.T) {
	assert.False(t, isSampled(metadata.New(map[string]string{"X-B3-SAMPLED": "0"})))
}

func TestB3True(t *testing.T) {
	assert.True(t, isSampled(metadata.New(map[string]string{"b3": "1"})))
}

func TestB3False(t *testing.T) {
	assert.False(t, isSampled(metadata.New(map[string]string{"b3": "0"})))
}

func TestB3WithTraceParentIdFalse(t *testing.T) {
	assert.False(t, isSampled(metadata.New(map[string]string{"b3": "80f198ee56343ba864fe8b2a57d3eff7-e457b5a2e4d86bd1-0"})))
}

func TestB3WithTraceParentAndSpanIdFalse(t *testing.T) {
	assert.False(t, isSampled(metadata.New(map[string]string{"b3": "80f198ee56343ba864fe8b2a57d3eff7-e457b5a2e4d86bd1-0-05e3ac9a4f6e3b90"})))
}

func TestB3CaseFalse(t *testing.T) {
	assert.False(t, isSampled(metadata.New(map[string]string{"B3": "0"})))
}

func TestSampledSpan(t *testing.T) {
	tracer := mocktracer.New()
	// Need to define a valid span context as otherwise the tracer would
	// return error opentracing.ErrSpanContextNotFound
	spanContext := mocktracer.MockSpanContext{
		TraceID: 1,
		SpanID:  2,
		Sampled: true,
	}
	interceptor := TracingServerInterceptor(tracer)

	// Need to define a B3 header to indicate also that sampling is enabled
	md := metadata.MD{
		"b3": []string{"1"},
	}
	ctx := metadata.NewIncomingContext(context.Background(), md)

	mdWriter := metadataReaderWriter{md}
	tracer.Inject(spanContext, opentracing.HTTPHeaders, mdWriter)

	info := &grpc.UnaryServerInfo{
		FullMethod: "mymethod",
	}
	interceptor(ctx, nil, info, func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, nil
	})
	assert.Len(t, tracer.FinishedSpans(), 1)
}

func TestErrSpanContextNotFound(t *testing.T) {
	tracer := mocktracer.New()
	interceptor := TracingServerInterceptor(tracer)

	// Need to define a B3 header to indicate also that sampling is enabled
	md := metadata.MD{
		"b3": []string{"1"},
	}
	ctx := metadata.NewIncomingContext(context.Background(), md)

	info := &grpc.UnaryServerInfo{
		FullMethod: "mymethod",
	}
	interceptor(ctx, nil, info, func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, nil
	})
	assert.Len(t, tracer.FinishedSpans(), 0)
}

func TestNotSampledSpan(t *testing.T) {
	tracer := mocktracer.New()
	// Need to define a valid span context as otherwise the tracer would
	// return error opentracing.ErrSpanContextNotFound
	spanContext := mocktracer.MockSpanContext{
		TraceID: 1,
		SpanID:  2,
		Sampled: true,
	}
	interceptor := TracingServerInterceptor(tracer)

	// Need to define a B3 header to indicate also that sampling is disabled
	md := metadata.MD{
		"b3": []string{"0"},
	}
	ctx := metadata.NewIncomingContext(context.Background(), md)

	mdWriter := metadataReaderWriter{md}
	tracer.Inject(spanContext, opentracing.HTTPHeaders, mdWriter)

	info := &grpc.UnaryServerInfo{
		FullMethod: "mymethod",
	}
	interceptor(ctx, nil, info, func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, nil
	})
	assert.Len(t, tracer.FinishedSpans(), 0)
}

func TestNoSpanContext(t *testing.T) {
	tracer := mocktracer.New()
	interceptor := TracingServerInterceptor(tracer)

	info := &grpc.UnaryServerInfo{
		FullMethod: "mymethod",
	}
	interceptor(context.Background(), nil, info, func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, nil
	})
	assert.Len(t, tracer.FinishedSpans(), 0)
}

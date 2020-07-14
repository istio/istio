// Copyright Istio Authors. All Rights Reserved.
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
	"strings"

	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/opentracing/opentracing-go/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

var (
	// Morally a const:
	componentTag = opentracing.Tag{Key: string(ext.Component), Value: "istio-mixer"}

	defaultNoopTracer = opentracing.NoopTracer{}
)

type metadataReaderWriter struct {
	metadata.MD
}

func (w metadataReaderWriter) Set(key, val string) {
	// The GRPC HPACK implementation rejects any uppercase keys here.
	//
	// As such, since the HTTP_HEADERS format is case-insensitive anyway, we
	// blindly lowercase the key (which is guaranteed to work in the
	// Inject/Extract sense per the OpenTracing spec).
	key = strings.ToLower(key)
	w.MD[key] = append(w.MD[key], val)
}

func (w metadataReaderWriter) ForeachKey(handler func(key, val string) error) error {
	for k, vals := range w.MD {
		for _, v := range vals {
			if err := handler(k, v); err != nil {
				return err
			}
		}
	}

	return nil
}

// TracingServerInterceptor copy of the GRPC tracing interceptor to enable switching
// between the supplied tracer and NoopTracer depending upon whether the request is
// being sampled.
func TracingServerInterceptor(tracer opentracing.Tracer) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp interface{}, err error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			md = metadata.New(nil)
		}

		t := tracer
		var spanContext opentracing.SpanContext

		if !isSampled(md) {
			t = defaultNoopTracer
		} else if spanContext, err = t.Extract(opentracing.HTTPHeaders, metadataReaderWriter{md}); err == opentracing.ErrSpanContextNotFound {
			t = defaultNoopTracer
		}

		serverSpan := t.StartSpan(
			info.FullMethod,
			ext.RPCServerOption(spanContext),
			componentTag,
		)
		defer serverSpan.Finish()

		ctx = opentracing.ContextWithSpan(ctx, serverSpan)
		resp, err = handler(ctx, req)
		if err != nil {
			otgrpc.SetSpanTags(serverSpan, err, false)
			serverSpan.LogFields(log.String("event", "error"), log.String("message", err.Error()))
		}
		return resp, err
	}
}

func isSampled(md metadata.MD) bool {
	for _, val := range md.Get("x-b3-sampled") {
		if val == "1" || strings.EqualFold(val, "true") {
			return true
		}
	}

	// allow for compressed header too
	for _, val := range md.Get("b3") {
		if val == "1" || strings.HasSuffix(val, "-1") || strings.Contains(val, "-1-") {
			return true
		}
	}

	return false
}

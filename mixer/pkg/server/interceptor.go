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

		var t opentracing.Tracer = defaultNoopTracer
		if isSampled(md) {
			t = tracer
		}

		spanContext, err := t.Extract(opentracing.HTTPHeaders, metadataReaderWriter{md})

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
	sampled := true

	for _, val := range md.Get("x-b3-sampled") {
		if val == "0" {
			sampled = false
			break
		}
	}

	// allow for compressed header too
	for _, val := range md.Get("b3") {
		if val == "0" || strings.HasSuffix(val, "-0") || strings.Contains(val, "-0-") {
			sampled = false
			break
		}
	}

	return sampled
}

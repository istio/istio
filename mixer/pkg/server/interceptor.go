package server

import (
	"context"
	"fmt"
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
	gRPCComponentTag = opentracing.Tag{Key: string(ext.Component), Value: "gRPC"}
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

type noopSpan struct{}
type noopSpanContext struct{}

var (
	defaultNoopSpanContext = noopSpanContext{}
	defaultNoopSpan        = noopSpan{}
	defaultNoopTracer      = opentracing.NoopTracer{}
)

const (
	emptyString = ""
)

// noopSpanContext:
func (n noopSpanContext) ForeachBaggageItem(handler func(k, v string) bool) {}

// noopSpan:
func (n noopSpan) Context() opentracing.SpanContext                       { return defaultNoopSpanContext }
func (n noopSpan) SetBaggageItem(key, val string) opentracing.Span        { return defaultNoopSpan }
func (n noopSpan) BaggageItem(key string) string                          { return emptyString }
func (n noopSpan) SetTag(key string, value interface{}) opentracing.Span  { return n }
func (n noopSpan) LogFields(fields ...log.Field)                          {}
func (n noopSpan) LogKV(keyVals ...interface{})                           {}
func (n noopSpan) Finish()                                                {}
func (n noopSpan) FinishWithOptions(opts opentracing.FinishOptions)       {}
func (n noopSpan) SetOperationName(operationName string) opentracing.Span { return n }
func (n noopSpan) Tracer() opentracing.Tracer                             { return defaultNoopTracer }
func (n noopSpan) LogEvent(event string)                                  {}
func (n noopSpan) LogEventWithPayload(event string, payload interface{})  {}
func (n noopSpan) Log(data opentracing.LogData)                           {}

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
		spanContext, err := tracer.Extract(opentracing.HTTPHeaders, metadataReaderWriter{md})
		if err != nil && err != opentracing.ErrSpanContextNotFound {
			// TODO: establish some sort of error reporting mechanism here. We
			// don't know where to put such an error and must rely on Tracer
			// implementations to do something appropriate for the time being.

			fmt.Printf("non nil error in trace extraction: %#v\n", err)
		}
		var serverSpan opentracing.Span
		if err == opentracing.ErrSpanContextNotFound {
			serverSpan = defaultNoopSpan
			fmt.Println("using noop span")
		} else {
			fmt.Println("using real span with real ID")
			serverSpan = tracer.StartSpan(
				info.FullMethod,
				ext.RPCServerOption(spanContext),
				gRPCComponentTag,
			)
		}
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

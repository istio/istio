package trace

import (
	"context"
	"github.com/signalfx/golib/log"
)

// DefaultLogger is used by package structs that don't have a default logger set.
var DefaultLogger = log.DefaultLogger.CreateChild()

// Trace is a list of spans
type Trace []*Span

// Span defines a span
type Span struct {
	TraceID        string            `json:"traceId"` // required
	Name           *string           `json:"name"`
	ParentID       *string           `json:"parentId"`
	ID             string            `json:"id"` // required
	Kind           *string           `json:"kind"`
	Timestamp      *float64          `json:"timestamp"`
	Duration       *float64          `json:"duration"`
	Debug          *bool             `json:"debug"`
	Shared         *bool             `json:"shared"`
	LocalEndpoint  *Endpoint         `json:"localEndpoint"`
	RemoteEndpoint *Endpoint         `json:"remoteEndpoint"`
	Annotations    []*Annotation     `json:"annotations"`
	Tags           map[string]string `json:"tags"`
}

// Endpoint is the network context of a node in the service graph
type Endpoint struct {
	ServiceName *string `json:"serviceName"`
	Ipv4        *string `json:"ipv4"`
	Ipv6        *string `json:"ipv6"`
	Port        *int32  `json:"port"`
}

// Annotation associates an event that explains latency with a timestamp.
// Unlike log statements, annotations are often codes. Ex. “ws” for WireSend
type Annotation struct {
	Timestamp *float64 `json:"timestamp"`
	Value     *string  `json:"value"`
}

// A Sink is an object that can accept traces and do something with them, like forward them to some endpoint
type Sink interface {
	AddSpans(ctx context.Context, traces []*Span) error
}

// A MiddlewareConstructor is used by FromChain to chain together a bunch of sinks that forward to each other
type MiddlewareConstructor func(sendTo Sink) Sink

// FromChain creates an endpoint Sink that sends calls between multiple middlewares for things like counting traces in between.
func FromChain(endSink Sink, sinks ...MiddlewareConstructor) Sink {
	for i := len(sinks) - 1; i >= 0; i-- {
		endSink = sinks[i](endSink)
	}
	return endSink
}

// NextSink is a special case of a sink that forwards to another sink
type NextSink interface {
	AddSpans(ctx context.Context, traces []*Span, next Sink) error
}

type nextWrapped struct {
	forwardTo Sink
	wrapping  NextSink
}

func (n *nextWrapped) AddSpans(ctx context.Context, traces []*Span) error {
	return n.wrapping.AddSpans(ctx, traces, n.forwardTo)
}

// NextWrap wraps a NextSink to make it usable by MiddlewareConstructor
func NextWrap(wrapping NextSink) MiddlewareConstructor {
	return func(sendTo Sink) Sink {
		return &nextWrapped{
			forwardTo: sendTo,
			wrapping:  wrapping,
		}
	}
}

// Copyright 2017, OpenCensus Authors
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

package trace

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"go.opencensus.io/internal"
)

// Span represents a span of a trace.  It has an associated SpanContext, and
// stores data accumulated while the span is active.
//
// Ideally users should interact with Spans by calling the functions in this
// package that take a Context parameter.
type Span struct {
	// data contains information recorded about the span.
	//
	// It will be non-nil if we are exporting the span or recording events for it.
	// Otherwise, data is nil, and the Span is simply a carrier for the
	// SpanContext, so that the trace ID is propagated.
	data        *SpanData
	mu          sync.Mutex // protects the contents of *data (but not the pointer value.)
	spanContext SpanContext
	// spanStore is the spanStore this span belongs to, if any, otherwise it is nil.
	*spanStore
	exportOnce sync.Once
}

// IsRecordingEvents returns true if events are being recorded for this span.
// Use this check to avoid computing expensive annotations when they will never
// be used.
func (s *Span) IsRecordingEvents() bool {
	if s == nil {
		return false
	}
	return s.data != nil
}

// TraceOptions contains options associated with a trace span.
type TraceOptions uint32

// IsSampled returns true if the span will be exported.
func (sc SpanContext) IsSampled() bool {
	return sc.TraceOptions.IsSampled()
}

// setIsSampled sets the TraceOptions bit that determines whether the span will be exported.
func (sc *SpanContext) setIsSampled(sampled bool) {
	if sampled {
		sc.TraceOptions |= 1
	} else {
		sc.TraceOptions &= ^TraceOptions(1)
	}
}

// IsSampled returns true if the span will be exported.
func (t TraceOptions) IsSampled() bool {
	return t&1 == 1
}

// SpanContext contains the state that must propagate across process boundaries.
//
// SpanContext is not an implementation of context.Context.
// TODO: add reference to external Census docs for SpanContext.
type SpanContext struct {
	TraceID      TraceID
	SpanID       SpanID
	TraceOptions TraceOptions
}

type contextKey struct{}

// FromContext returns the Span stored in a context, or nil if there isn't one.
func FromContext(ctx context.Context) *Span {
	s, _ := ctx.Value(contextKey{}).(*Span)
	return s
}

// WithSpan returns a new context with the given Span attached.
func WithSpan(parent context.Context, s *Span) context.Context {
	return context.WithValue(parent, contextKey{}, s)
}

// All available span kinds. Span kind must be either one of these values.
const (
	SpanKindUnspecified = iota
	SpanKindServer
	SpanKindClient
)

// StartOptions contains options concerning how a span is started.
type StartOptions struct {
	// Sampler to consult for this Span. If provided, it is always consulted.
	//
	// If not provided, then the behavior differs based on whether
	// the parent of this Span is remote, local, or there is no parent.
	// In the case of a remote parent or no parent, the
	// default sampler (see SetDefaultSampler) will be consulted. Otherwise,
	// when there is a non-remote parent, no new sampling decision will be made:
	// we will preserve the sampling of the parent.
	Sampler Sampler

	// SpanKind represents the kind of a span. If none is set,
	// SpanKindUnspecified is used.
	SpanKind int
}

// StartSpan starts a new child span of the current span in the context. If
// there is no span in the context, creates a new trace and span.
//
// This is provided as a convenience for WithSpan(ctx, NewSpan(...)). Use it
// if you require custom spans in addition to the default spans provided by
// ocgrpc, ochttp or similar framework integration.
func StartSpan(ctx context.Context, name string) (context.Context, *Span) {
	parentSpan, _ := ctx.Value(contextKey{}).(*Span)
	span := NewSpan(name, parentSpan, StartOptions{})
	return WithSpan(ctx, span), span
}

// NewSpan returns a new span.
//
// If parent is not nil, created span will be a child of the parent.
func NewSpan(name string, parent *Span, o StartOptions) *Span {
	hasParent := false
	var parentSpanContext SpanContext
	if parent != nil {
		hasParent = true
		parentSpanContext = parent.SpanContext()
	}
	return startSpanInternal(name, hasParent, parentSpanContext, false, o)
}

// NewSpanWithRemoteParent returns a new span with the given parent SpanContext.
func NewSpanWithRemoteParent(name string, parent SpanContext, o StartOptions) *Span {
	return startSpanInternal(name, true, parent, true, o)
}

func startSpanInternal(name string, hasParent bool, parent SpanContext, remoteParent bool, o StartOptions) *Span {
	span := &Span{}
	span.spanContext = parent
	mu.Lock()
	if !hasParent {
		span.spanContext.TraceID = newTraceIDLocked()
	}
	span.spanContext.SpanID = newSpanIDLocked()
	sampler := defaultSampler
	mu.Unlock()

	if !hasParent || remoteParent || o.Sampler != nil {
		// If this span is the child of a local span and no Sampler is set in the
		// options, keep the parent's TraceOptions.
		//
		// Otherwise, consult the Sampler in the options if it is non-nil, otherwise
		// the default sampler.
		if o.Sampler != nil {
			sampler = o.Sampler
		}
		span.spanContext.setIsSampled(sampler(SamplingParameters{
			ParentContext:   parent,
			TraceID:         span.spanContext.TraceID,
			SpanID:          span.spanContext.SpanID,
			Name:            name,
			HasRemoteParent: remoteParent}).Sample)
	}

	if !internal.LocalSpanStoreEnabled && !span.spanContext.IsSampled() {
		return span
	}

	span.data = &SpanData{
		SpanContext:     span.spanContext,
		StartTime:       time.Now(),
		SpanKind:        o.SpanKind,
		Name:            name,
		HasRemoteParent: remoteParent,
	}
	if hasParent {
		span.data.ParentSpanID = parent.SpanID
	}
	if internal.LocalSpanStoreEnabled {
		var ss *spanStore
		ss = spanStoreForNameCreateIfNew(name)
		if ss != nil {
			span.spanStore = ss
			ss.add(span)
		}
	}

	return span
}

// End ends the span.
func (s *Span) End() {
	if !s.IsRecordingEvents() {
		return
	}
	s.exportOnce.Do(func() {
		// TODO: optimize to avoid this call if sd won't be used.
		sd := s.makeSpanData()
		sd.EndTime = internal.MonotonicEndTime(sd.StartTime)
		if s.spanStore != nil {
			s.spanStore.finished(s, sd)
		}
		if s.spanContext.IsSampled() {
			// TODO: consider holding exportersMu for less time.
			exportersMu.Lock()
			for e := range exporters {
				e.ExportSpan(sd)
			}
			exportersMu.Unlock()
		}
	})
}

// makeSpanData produces a SpanData representing the current state of the Span.
// It requires that s.data is non-nil.
func (s *Span) makeSpanData() *SpanData {
	var sd SpanData
	s.mu.Lock()
	sd = *s.data
	if s.data.Attributes != nil {
		sd.Attributes = make(map[string]interface{})
		for k, v := range s.data.Attributes {
			sd.Attributes[k] = v
		}
	}
	s.mu.Unlock()
	return &sd
}

// SpanContext returns the SpanContext of the span.
func (s *Span) SpanContext() SpanContext {
	if s == nil {
		return SpanContext{}
	}
	return s.spanContext
}

// SetStatus sets the status of the span, if it is recording events.
func (s *Span) SetStatus(status Status) {
	if !s.IsRecordingEvents() {
		return
	}
	s.mu.Lock()
	s.data.Status = status
	s.mu.Unlock()
}

// AddAttributes sets attributes in the span.
//
// Existing attributes whose keys appear in the attributes parameter are overwritten.
func (s *Span) AddAttributes(attributes ...Attribute) {
	if !s.IsRecordingEvents() {
		return
	}
	s.mu.Lock()
	if s.data.Attributes == nil {
		s.data.Attributes = make(map[string]interface{})
	}
	copyAttributes(s.data.Attributes, attributes)
	s.mu.Unlock()
}

// copyAttributes copies a slice of Attributes into a map.
func copyAttributes(m map[string]interface{}, attributes []Attribute) {
	for _, a := range attributes {
		m[a.key] = a.value
	}
}

func (s *Span) lazyPrintfInternal(attributes []Attribute, format string, a ...interface{}) {
	now := time.Now()
	msg := fmt.Sprintf(format, a...)
	var m map[string]interface{}
	s.mu.Lock()
	if len(attributes) != 0 {
		m = make(map[string]interface{})
		copyAttributes(m, attributes)
	}
	s.data.Annotations = append(s.data.Annotations, Annotation{
		Time:       now,
		Message:    msg,
		Attributes: m,
	})
	s.mu.Unlock()
}

func (s *Span) printStringInternal(attributes []Attribute, str string) {
	now := time.Now()
	var a map[string]interface{}
	s.mu.Lock()
	if len(attributes) != 0 {
		a = make(map[string]interface{})
		copyAttributes(a, attributes)
	}
	s.data.Annotations = append(s.data.Annotations, Annotation{
		Time:       now,
		Message:    str,
		Attributes: a,
	})
	s.mu.Unlock()
}

// Annotate adds an annotation with attributes.
// Attributes can be nil.
func (s *Span) Annotate(attributes []Attribute, str string) {
	if !s.IsRecordingEvents() {
		return
	}
	s.printStringInternal(attributes, str)
}

// Annotatef adds an annotation with attributes.
func (s *Span) Annotatef(attributes []Attribute, format string, a ...interface{}) {
	if !s.IsRecordingEvents() {
		return
	}
	s.lazyPrintfInternal(attributes, format, a...)
}

// AddMessageSendEvent adds a message send event to the span.
//
// messageID is an identifier for the message, which is recommended to be
// unique in this span and the same between the send event and the receive
// event (this allows to identify a message between the sender and receiver).
// For example, this could be a sequence id.
func (s *Span) AddMessageSendEvent(messageID, uncompressedByteSize, compressedByteSize int64) {
	if !s.IsRecordingEvents() {
		return
	}
	now := time.Now()
	s.mu.Lock()
	s.data.MessageEvents = append(s.data.MessageEvents, MessageEvent{
		Time:                 now,
		EventType:            MessageEventTypeSent,
		MessageID:            messageID,
		UncompressedByteSize: uncompressedByteSize,
		CompressedByteSize:   compressedByteSize,
	})
	s.mu.Unlock()
}

// AddMessageReceiveEvent adds a message receive event to the span.
//
// messageID is an identifier for the message, which is recommended to be
// unique in this span and the same between the send event and the receive
// event (this allows to identify a message between the sender and receiver).
// For example, this could be a sequence id.
func (s *Span) AddMessageReceiveEvent(messageID, uncompressedByteSize, compressedByteSize int64) {
	if !s.IsRecordingEvents() {
		return
	}
	now := time.Now()
	s.mu.Lock()
	s.data.MessageEvents = append(s.data.MessageEvents, MessageEvent{
		Time:                 now,
		EventType:            MessageEventTypeRecv,
		MessageID:            messageID,
		UncompressedByteSize: uncompressedByteSize,
		CompressedByteSize:   compressedByteSize,
	})
	s.mu.Unlock()
}

// AddLink adds a link to the span.
func (s *Span) AddLink(l Link) {
	if !s.IsRecordingEvents() {
		return
	}
	s.mu.Lock()
	s.data.Links = append(s.data.Links, l)
	s.mu.Unlock()
}

func (s *Span) String() string {
	if s == nil {
		return "<nil>"
	}
	if s.data == nil {
		return fmt.Sprintf("span %s", s.spanContext.SpanID)
	}
	s.mu.Lock()
	str := fmt.Sprintf("span %s %q", s.spanContext.SpanID, s.data.Name)
	s.mu.Unlock()
	return str
}

var (
	mu             sync.Mutex // protects the variables below
	traceIDRand    *rand.Rand
	traceIDAdd     [2]uint64
	nextSpanID     uint64
	spanIDInc      uint64
	defaultSampler Sampler
)

func init() {
	// initialize traceID and spanID generators.
	var rngSeed int64
	for _, p := range []interface{}{
		&rngSeed, &traceIDAdd, &nextSpanID, &spanIDInc,
	} {
		binary.Read(crand.Reader, binary.LittleEndian, p)
	}
	traceIDRand = rand.New(rand.NewSource(rngSeed))
	spanIDInc |= 1
}

// newSpanIDLocked returns a non-zero SpanID from a randomly-chosen sequence.
// mu should be held while this function is called.
func newSpanIDLocked() SpanID {
	id := nextSpanID
	nextSpanID += spanIDInc
	if nextSpanID == 0 {
		nextSpanID += spanIDInc
	}
	var sid SpanID
	binary.LittleEndian.PutUint64(sid[:], id)
	return sid
}

// newTraceIDLocked returns a non-zero TraceID from a randomly-chosen sequence.
// mu should be held while this function is called.
func newTraceIDLocked() TraceID {
	var tid TraceID
	// Construct the trace ID from two outputs of traceIDRand, with a constant
	// added to each half for additional entropy.
	binary.LittleEndian.PutUint64(tid[0:8], traceIDRand.Uint64()+traceIDAdd[0])
	binary.LittleEndian.PutUint64(tid[8:16], traceIDRand.Uint64()+traceIDAdd[1])
	return tid
}

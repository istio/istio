package zipkin

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/openzipkin/zipkin-go/model"
)

type spanImpl struct {
	mtx sync.RWMutex
	model.SpanModel
	tracer        *Tracer
	mustCollect   int32 // used as atomic bool (1 = true, 0 = false)
	flushOnFinish bool
}

func (s *spanImpl) Context() model.SpanContext {
	return s.SpanContext
}

func (s *spanImpl) SetName(name string) {
	s.mtx.Lock()
	s.Name = name
	s.mtx.Unlock()
}

func (s *spanImpl) SetRemoteEndpoint(e *model.Endpoint) {
	s.mtx.Lock()
	if e == nil {
		s.RemoteEndpoint = nil
	} else {
		s.RemoteEndpoint = &model.Endpoint{}
		*s.RemoteEndpoint = *e
	}
	s.mtx.Unlock()
}

func (s *spanImpl) Annotate(t time.Time, value string) {
	a := model.Annotation{
		Timestamp: t,
		Value:     value,
	}

	s.mtx.Lock()
	s.Annotations = append(s.Annotations, a)
	s.mtx.Unlock()
}

func (s *spanImpl) Tag(key, value string) {
	s.mtx.Lock()

	if key == string(TagError) {
		if _, found := s.Tags[key]; found {
			s.mtx.Unlock()
			return
		}
	}

	s.Tags[key] = value
	s.mtx.Unlock()
}

func (s *spanImpl) Finish() {
	if atomic.CompareAndSwapInt32(&s.mustCollect, 1, 0) {
		s.Duration = time.Since(s.Timestamp)
		if s.flushOnFinish {
			s.tracer.reporter.Send(s.SpanModel)
		}
	}
}

func (s *spanImpl) Flush() {
	if s.SpanModel.Debug || (s.SpanModel.Sampled != nil && *s.SpanModel.Sampled) {
		s.tracer.reporter.Send(s.SpanModel)
	}
}

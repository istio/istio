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

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

/*
Package idgenerator contains several Span and Trace ID generators which can be
used by the Zipkin tracer. Additional third party generators can be plugged in
if they adhere to the IDGenerator interface.
*/
package idgenerator

import (
	"math/rand"
	"sync"
	"time"

	"github.com/openzipkin/zipkin-go/model"
)

var (
	seededIDGen = rand.New(rand.NewSource(time.Now().UnixNano()))
	// NewSource returns a new pseudo-random Source seeded with the given value.
	// Unlike the default Source used by top-level functions, this source is not
	// safe for concurrent use by multiple goroutines. Hence the need for a mutex.
	seededIDLock sync.Mutex
)

// IDGenerator interface can be used to provide the Zipkin Tracer with custom
// implementations to generate Span and Trace IDs.
type IDGenerator interface {
	SpanID(traceID model.TraceID) model.ID // Generates a new Span ID
	TraceID() model.TraceID                // Generates a new Trace ID
}

// NewRandom64 returns an ID Generator which can generate 64 bit trace and span
// id's
func NewRandom64() IDGenerator {
	return &randomID64{}
}

// NewRandom128 returns an ID Generator which can generate 128 bit trace and 64
// bit span id's
func NewRandom128() IDGenerator {
	return &randomID128{}
}

// NewRandomTimestamped generates 128 bit time sortable traceid's and 64 bit
// spanid's.
func NewRandomTimestamped() IDGenerator {
	return &randomTimestamped{}
}

// randomID64 can generate 64 bit traceid's and 64 bit spanid's.
type randomID64 struct{}

func (r *randomID64) TraceID() (id model.TraceID) {
	seededIDLock.Lock()
	id = model.TraceID{
		Low: uint64(seededIDGen.Int63()),
	}
	seededIDLock.Unlock()
	return
}

func (r *randomID64) SpanID(traceID model.TraceID) (id model.ID) {
	if !traceID.Empty() {
		return model.ID(traceID.Low)
	}
	seededIDLock.Lock()
	id = model.ID(seededIDGen.Int63())
	seededIDLock.Unlock()
	return
}

// randomID128 can generate 128 bit traceid's and 64 bit spanid's.
type randomID128 struct{}

func (r *randomID128) TraceID() (id model.TraceID) {
	seededIDLock.Lock()
	id = model.TraceID{
		High: uint64(seededIDGen.Int63()),
		Low:  uint64(seededIDGen.Int63()),
	}
	seededIDLock.Unlock()
	return
}

func (r *randomID128) SpanID(traceID model.TraceID) (id model.ID) {
	if !traceID.Empty() {
		return model.ID(traceID.Low)
	}
	seededIDLock.Lock()
	id = model.ID(seededIDGen.Int63())
	seededIDLock.Unlock()
	return
}

// randomTimestamped can generate 128 bit time sortable traceid's compatible
// with AWS X-Ray and 64 bit spanid's.
type randomTimestamped struct{}

func (t *randomTimestamped) TraceID() (id model.TraceID) {
	seededIDLock.Lock()
	id = model.TraceID{
		High: uint64(time.Now().Unix()<<32) + uint64(seededIDGen.Int31()),
		Low:  uint64(seededIDGen.Int63()),
	}
	seededIDLock.Unlock()
	return
}

func (t *randomTimestamped) SpanID(traceID model.TraceID) (id model.ID) {
	if !traceID.Empty() {
		return model.ID(traceID.Low)
	}
	seededIDLock.Lock()
	id = model.ID(seededIDGen.Int63())
	seededIDLock.Unlock()
	return
}

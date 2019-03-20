//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package processing_test

import (
	"testing"

	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/galley/pkg/runtime/resource"
)

func TestDispatcher_Handle(t *testing.T) {
	b := processing.NewDispatcherBuilder()

	fh1 := &fakeHandler{}
	fh2 := &fakeHandler{}
	fh3 := &fakeHandler{}
	b.Add(emptyInfo.Collection, fh1)
	b.Add(emptyInfo.Collection, fh2)
	b.Add(structInfo.Collection, fh3)
	d := b.Build()

	d.Handle(addRes1V1())
	d.Handle(addRes2V1())
	if len(fh1.evts) != 2 {
		t.Fatalf("unexpected events: %v", fh1.evts)
	}
	if len(fh2.evts) != 2 {
		t.Fatalf("unexpected events: %v", fh2.evts)
	}

	if len(fh3.evts) != 0 {
		t.Fatalf("unexpected events: %v", fh3.evts)
	}

	d.Handle(addRes3V1())
	if len(fh1.evts) != 2 {
		t.Fatalf("unexpected events: %v", fh1.evts)
	}
	if len(fh2.evts) != 2 {
		t.Fatalf("unexpected events: %v", fh2.evts)
	}

	if len(fh3.evts) != 1 {
		t.Fatalf("unexpected events: %v", fh3.evts)
	}
}

func TestDispatcher_UnhandledEvent(t *testing.T) {
	b := processing.NewDispatcherBuilder()
	d := b.Build()

	d.Handle(addRes1V1())
	// no panic
}

type fakeHandler struct {
	evts []resource.Event
}

var _ processing.Handler = &fakeHandler{}

func (f *fakeHandler) Handle(e resource.Event) {
	f.evts = append(f.evts, e)
}

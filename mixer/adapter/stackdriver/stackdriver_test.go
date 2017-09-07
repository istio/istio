// Copyright 2017 Istio Authors.
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

package stackdriver

import (
	"context"
	"testing"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/adapter/test"
	"istio.io/mixer/template/logentry"
	"istio.io/mixer/template/metric"
)

type (
	fakeBuilder struct {
		calledConfigure bool
		calledBuild     bool
		instance        *fakeAspect
	}

	fakeAspect struct {
		calledHandle bool
		calledClose  bool
	}
)

func (f *fakeBuilder) Build(adapter.Config, adapter.Env) (adapter.Handler, error) {
	f.calledBuild = true
	return f.instance, nil
}

func (f *fakeBuilder) SetMetricTypes(metrics map[string]*metric.Type) error {
	f.calledConfigure = true
	return nil
}

func (f *fakeBuilder) SetLogEntryTypes(entries map[string]*logentry.Type) error {
	f.calledConfigure = true
	return nil
}

func (f *fakeAspect) Close() error {
	f.calledClose = true
	return nil
}

func (f *fakeAspect) HandleMetric(context.Context, []*metric.Instance) error {
	f.calledHandle = true
	return nil
}

func (f *fakeAspect) HandleLogEntry(context.Context, []*logentry.Instance) error {
	f.calledHandle = true
	return nil
}

func TestDispatchConfigureAndBuild(t *testing.T) {
	m := &fakeBuilder{}
	l := &fakeBuilder{}
	b := &builder{m, l}
	if err := b.SetMetricTypes(make(map[string]*metric.Type)); err != nil {
		t.Errorf("Unexpected error configuring metric handler: %v", err)
	}
	if !m.calledConfigure {
		t.Error("Expected m.SetMetricTypes to be called, wasn't.")
	}
	if err := b.SetLogEntryTypes(make(map[string]*logentry.Type)); err != nil {
		t.Errorf("Unexpected error configuring log handler: %v", err)
	}
	if !l.calledConfigure {
		t.Error("Expected l.SetLogEntryTypes to be called, wasn't.")
	}

	if l.calledBuild || m.calledBuild {
		t.Fatalf("Build called on builders before calling b.Build")
	}
	if _, err := b.Build(nil, test.NewEnv(t)); err != nil {
		t.Errorf("Exepected err calling builder.Build: %v", err)
	}
	if !m.calledBuild {
		t.Errorf("b.Build but m.Build not called")
	}
	if !l.calledBuild {
		t.Errorf("b.Build but l.Build not called")
	}
}

func TestDispatchHandleAndClose(t *testing.T) {
	la := &fakeAspect{}
	lb := &fakeBuilder{instance: la}
	ma := &fakeAspect{}
	mb := &fakeBuilder{instance: ma}
	b := &builder{mb, lb}

	superHandler, err := b.Build(nil, test.NewEnv(t))
	if err != nil {
		t.Fatalf("Unexpected error calling builder.Build: %v", err)
	}

	ms, _ := superHandler.(metric.Handler)
	if err := ms.HandleMetric(context.Background(), []*metric.Instance{}); err != nil {
		t.Errorf("HandleMetric returned unexpected err: %v", err)
	}
	if !ma.calledHandle {
		t.Error("Called handler.HandleMetric, but call not forwarded to metric aspect")
	}

	ls, _ := superHandler.(logentry.Handler)
	if err := ls.HandleLogEntry(context.Background(), []*logentry.Instance{}); err != nil {
		t.Errorf("HandleLogEntry returned unexpected err: %v", err)
	}
	if !la.calledHandle {
		t.Error("Called handler.HandleLogEntry, but call not forwarded to log aspect")
	}

	if err := ms.Close(); err != nil {
		t.Errorf("Unexpected error when calling close: %v", err)
	}
	if !ma.calledClose {
		t.Error("Called handler.Close, but call not forwarded to metric aspect")
	}
	if !la.calledClose {
		t.Error("Called handler.Close, but call not forwarded to log aspect")
	}

}

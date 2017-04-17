// Copyright 2017 Google Inc.
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

package kubernetes

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"k8s.io/client-go/pkg/api/v1"

	"istio.io/mixer/adapter/kubernetes/config"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/adapter/test"
)

type fakeCache struct {
	cacheController

	getPodErr bool
	pods      map[string]*v1.Pod
}

func (fakeCache) HasSynced() bool {
	return true
}

func (fakeCache) Run(<-chan struct{}) {
	// do nothing
}

func (f fakeCache) GetPod(pod string) (*v1.Pod, error) {
	if f.getPodErr {
		return nil, errors.New("get pod error")
	}
	p, found := f.pods[pod]
	if !found {
		return nil, errors.New("pod not found")
	}
	return p, nil
}

func errorStartingPodCache(ignored string, empty time.Duration, e adapter.Env) (cacheController, error) {
	return nil, errors.New("cache build error")
}

func fakePodCache(ignored string, empty time.Duration, e adapter.Env) (cacheController, error) {
	return &fakeCache{}, nil
}

// note: not using TestAdapterInvariants here because of kubernetes dependency.
// we are aiming for simple unit testing. a larger, more involved integration
// test / e2e test must be written to validate the builder in relation to a
// real kubernetes cluster.
func TestBuilder(t *testing.T) {
	b := newBuilder(fakePodCache)

	// setup a check that the stopChan is appropriately closed
	closed := make(chan struct{})
	go func() {
		closed <- <-b.stopChan
	}()

	if b.Name() == "" {
		t.Error("Name() => all builders need names")
	}

	if b.Description() == "" {
		t.Errorf("Description() => builder '%s' doesn't provide a valid description", b.Name())
	}

	c := b.DefaultConfig()
	if err := b.ValidateConfig(c); err != nil {
		t.Errorf("ValidateConfig() => builder '%s' can't validate its default configuration: %v", b.Name(), err)
	}

	if err := b.Close(); err != nil {
		t.Errorf("Close() => builder '%s' fails to close when used with its default configuration: %v", b.Name(), err)
	}

	select {
	case <-closed:
	case <-time.After(500 * time.Millisecond): // set a small deadline for this check
		t.Error("Close() should have closed the stopChan, but this wait timed out.")
	}
}

func TestBuilder_ValidateConfigErrors(t *testing.T) {
	emptyConfig := &config.Params{}
	b := newBuilder(fakePodCache)
	err := b.ValidateConfig(emptyConfig)
	if err == nil {
		t.Fatalf("Expected config to fail validation: %#v", emptyConfig)
	}
	if len(err.Multi.Errors) != 14 {
		t.Fatalf("Got %d errors; wanted %d", len(err.Multi.Errors), 14)
	}
}

func TestBuilder_BuildAttributesGenerator(t *testing.T) {
	tests := []struct {
		name    string
		testFn  controllerFactoryFn
		conf    adapter.Config
		wantErr bool
	}{
		{"success", fakePodCache, conf, false},
		{"builder error", errorStartingPodCache, conf, true},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			b := newBuilder(v.testFn)
			_, err := b.BuildAttributesGenerator(test.NewEnv(t), v.conf)
			if err == nil && v.wantErr {
				t.Fatal("Expected error building adapter")
			}
			if err != nil && !v.wantErr {
				t.Fatalf("Got error, wanted none: %v", err)
			}
		})
	}
}

func TestKubegen_Close(t *testing.T) {
	v := kubegen{}
	if err := v.Close(); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestKubegen_Generate(t *testing.T) {
	inputs := map[string]interface{}{
		"sourceUID": "kubernetes://testsvc.default",
		"targetUID": "kubernetes://testsvc",
		"originUID": "",
	}

	want := map[string]interface{}{
		"sourceLabels": map[string]string{
			"app":       "test",
			"something": "",
		},
		"sourcePodIP":              "10.10.10.1",
		"sourceHostIP":             "10.1.1.10",
		"sourceNamespace":          "default",
		"sourcePodName":            "test_pod",
		"sourceService":            "test",
		"sourceServiceAccountName": "test",
	}

	pods := map[string]*v1.Pod{
		"default/testsvc": {
			ObjectMeta: v1.ObjectMeta{
				Name:      "test_pod",
				Namespace: "default",
				Labels: map[string]string{
					"app":       "test",
					"something": "",
				},
			},
			Status: v1.PodStatus{
				HostIP: "10.1.1.10",
				PodIP:  "10.10.10.1",
			},
			Spec: v1.PodSpec{
				ServiceAccountName: "test",
			},
		},
	}

	kg := &kubegen{
		log:    test.NewEnv(t).Logger(),
		params: *conf,
		pods:   fakeCache{pods: pods},
	}
	got, err := kg.Generate(inputs)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Generate(): got %#v; want %#v", got, want)
	}
}

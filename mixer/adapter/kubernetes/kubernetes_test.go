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

package kubernetes

import (
	"errors"
	"os"
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
	path      string
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

func fakePodCache(path string, empty time.Duration, e adapter.Env) (cacheController, error) {
	return &fakeCache{path: path}, nil
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
	tests := []struct {
		name     string
		conf     *config.Params
		errCount int
	}{
		{"empty config", &config.Params{}, 16},
		{"bad cluster domain name", &config.Params{ClusterDomainName: "something.silly"}, 16},
	}

	b := newBuilder(fakePodCache)
	for _, v := range tests {
		err := b.ValidateConfig(v.conf)
		if err == nil {
			t.Fatalf("Expected config to fail validation: %#v", v.conf)
		}
		if len(err.Multi.Errors) != v.errCount {
			t.Fatalf("Got %d errors; wanted %d", len(err.Multi.Errors), v.errCount)
		}
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

func TestBuilder_BuildAttributesGeneratorWithEnvVar(t *testing.T) {

	testConf := conf
	testConf.KubeconfigPath = "please/override"

	tests := []struct {
		name    string
		testFn  controllerFactoryFn
		conf    adapter.Config
		wantErr bool
	}{
		{"success", fakePodCache, testConf, false},
	}

	wantPath := "/want/kubeconfig"
	if err := os.Setenv("KUBECONFIG", wantPath); err != nil {
		t.Fatalf("Could not set KUBECONFIG environment var")
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
			got := b.pods.(*fakeCache).path
			if got != wantPath {
				t.Errorf("Bad kubeconfig path; got %s, want %s", got, wantPath)
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
	pods := map[string]*v1.Pod{
		"testns/testsvc": {
			ObjectMeta: v1.ObjectMeta{
				Name:      "test_pod",
				Namespace: "testns",
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
		"testns/badapplabel": {
			ObjectMeta: v1.ObjectMeta{
				Name:      "test_pod",
				Namespace: "testns",
				Labels: map[string]string{
					"app": "",
				},
			},
		},
	}

	kg := &kubegen{
		log:    test.NewEnv(t).Logger(),
		params: *conf,
		pods:   fakeCache{pods: pods},
	}

	sourceUIDIn := map[string]interface{}{
		"sourceUID":     "kubernetes://testsvc.testns",
		"targetUID":     "kubernetes://badsvcuid",
		"originUID":     "kubernetes://badsvcuid",
		"targetService": "hello:80",
	}

	sourceUIDOut := map[string]interface{}{
		"sourceLabels": map[string]string{
			"app":       "test",
			"something": "",
		},
		"sourcePodIP":              "10.10.10.1",
		"sourceHostIP":             "10.1.1.10",
		"sourceNamespace":          "testns",
		"sourcePodName":            "test_pod",
		"sourceService":            "test.testns.svc.cluster.local",
		"sourceServiceAccountName": "test",
		"targetService":            "hello.default.svc.cluster.local",
	}

	namespacedTargetSvcIn := map[string]interface{}{
		"targetService": "hello.world",
	}

	namespacedTargetSvcOut := map[string]interface{}{
		"targetService": "hello.world.svc.cluster.local",
	}

	partialTargetSvcIn := map[string]interface{}{
		"targetService": "hello.world.service:80",
	}

	partialTargetSvcOut := map[string]interface{}{
		"targetService": "hello.world.service.cluster.local",
	}

	morePartialTargetSvcIn := map[string]interface{}{
		"targetService": "hello.world.service.kluster:80",
	}

	morePartialTargetSvcOut := map[string]interface{}{
		"targetService": "hello.world.service.kluster.local",
	}

	targetSvcIn := map[string]interface{}{
		"targetService": "hello.world.service.kluster.remote:80",
	}

	targetSvcOut := map[string]interface{}{
		"targetService": "hello.world.service.kluster.remote",
	}

	ipTargetSvcIn := map[string]interface{}{
		"targetService": "10.1.100.10:80",
	}

	badTargetSvcOut := map[string]interface{}{}

	badTargetSvcIn := map[string]interface{}{
		"targetService": ":broken",
	}

	emptyTargetSvcIn := map[string]interface{}{
		"targetUID":     "kubernetes://badapplabel.testns",
		"targetService": "",
	}

	emptyTargetOut := map[string]interface{}{
		"targetLabels": map[string]string{
			"app": "",
		},
		"targetNamespace": "testns",
		"targetPodName":   "test_pod",
	}

	tests := []struct {
		name   string
		inputs map[string]interface{}
		want   map[string]interface{}
	}{
		{"source pod and target service", sourceUIDIn, sourceUIDOut},
		{"namespaced target service", namespacedTargetSvcIn, namespacedTargetSvcOut},
		{"target service with 3 parts", partialTargetSvcIn, partialTargetSvcOut},
		{"target service with 4 parts", morePartialTargetSvcIn, morePartialTargetSvcOut},
		{"fq target service", targetSvcIn, targetSvcOut},
		{"ip addr service", ipTargetSvcIn, badTargetSvcOut},
		{"bad target service", badTargetSvcIn, badTargetSvcOut},
		{"empty target service", emptyTargetSvcIn, emptyTargetOut},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			got, err := kg.Generate(v.inputs)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, v.want) {
				t.Fatalf("Generate(): got %#v; want %#v", got, v.want)
			}
		})
	}

}

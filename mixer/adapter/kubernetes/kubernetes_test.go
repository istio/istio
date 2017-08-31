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
	"net"
	"os"
	"reflect"
	"testing"
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/mixer/adapter/kubernetes/config"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/adapter/test"
)

type fakeCache struct {
	cacheController

	pods map[string]*v1.Pod
	path string
}

func (fakeCache) HasSynced() bool {
	return true
}

func (fakeCache) Run(<-chan struct{}) {
	// do nothing
}

func (f fakeCache) GetPod(pod string) (*v1.Pod, bool) {
	p, ok := f.pods[pod]
	return p, ok
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
	val := reflect.ValueOf(&config.Params{}).Elem()
	// currently two non-validated fields: kubeconfig and cache refresh duration
	expectedConfigErrs := val.NumField() - 2
	tests := []struct {
		name     string
		conf     *config.Params
		errCount int
	}{
		{"empty config", &config.Params{}, expectedConfigErrs},
		{"bad cluster domain name", &config.Params{ClusterDomainName: "something.silly"}, expectedConfigErrs},
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
		"testns/test-pod": {
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
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
		"testns/pod-cluster": {
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-cluster",
				Namespace: "testns",
				Labels:    map[string]string{"app": "alt-svc-with-cluster.testns.svc.cluster:8080"},
			},
		},
		"testns/long-pod": {
			ObjectMeta: metav1.ObjectMeta{
				Name:      "long-pod",
				Namespace: "testns",
				Labels: map[string]string{
					"app": "long-svc.testns.svc.cluster.local.solar",
				},
			},
		},
		"testns/empty":         {ObjectMeta: metav1.ObjectMeta{Name: "empty", Namespace: "testns", Labels: map[string]string{"app": ""}}},
		"testns/alt-pod":       {ObjectMeta: metav1.ObjectMeta{Name: "alt-pod", Namespace: "testns", Labels: map[string]string{"app": "alt-svc.testns"}}},
		"testns/bad-svc-pod":   {ObjectMeta: metav1.ObjectMeta{Name: "bad-svc-pod", Namespace: "testns", Labels: map[string]string{"app": ":"}}},
		"192.168.234.3":        {ObjectMeta: metav1.ObjectMeta{Name: "ip-svc-pod", Namespace: "testns", Labels: map[string]string{"app": "ipAddr"}}},
		"testns/istio-ingress": {ObjectMeta: metav1.ObjectMeta{Name: "istio-ingress", Namespace: "testns", Labels: map[string]string{"istio": "ingress"}}},
		"testns/ipApp":         {ObjectMeta: metav1.ObjectMeta{Name: "ipApp", Namespace: "testns", Labels: map[string]string{"app": "10.1.10.1"}}},
	}

	kg := &kubegen{log: test.NewEnv(t).Logger(), params: *conf, pods: fakeCache{pods: pods}}

	sourceUIDIn := map[string]interface{}{
		"sourceUID": "kubernetes://test-pod.testns",
		"targetUID": "kubernetes://badsvcuid",
		"originUID": "kubernetes://badsvcuid",
	}

	sourceUIDOut := map[string]interface{}{
		"sourceLabels": map[string]string{
			"app":       "test",
			"something": "",
		},
		"sourcePodIP":              "10.10.10.1",
		"sourceHostIP":             "10.1.1.10",
		"sourceNamespace":          "testns",
		"sourcePodName":            "test-pod",
		"sourceService":            "test.testns.svc.cluster.local",
		"sourceServiceAccountName": "test",
	}

	nsAppLabelIn := map[string]interface{}{"sourceUID": "kubernetes://alt-pod.testns"}

	nsAppLabelOut := map[string]interface{}{
		"sourceLabels": map[string]string{
			"app": "alt-svc.testns",
		},
		"sourceService":   "alt-svc.testns.svc.cluster.local",
		"sourceNamespace": "testns",
		"sourcePodName":   "alt-pod",
	}

	svcClusterIn := map[string]interface{}{"sourceUID": "kubernetes://pod-cluster.testns"}

	svcClusterOut := map[string]interface{}{
		"sourceLabels": map[string]string{
			"app": "alt-svc-with-cluster.testns.svc.cluster:8080",
		},
		"sourceService":   "alt-svc-with-cluster.testns.svc.cluster.local",
		"sourceNamespace": "testns",
		"sourcePodName":   "pod-cluster",
	}

	longSvcClusterIn := map[string]interface{}{"sourceUID": "kubernetes://long-pod.testns"}

	longSvcClusterOut := map[string]interface{}{
		"sourceLabels": map[string]string{
			"app": "long-svc.testns.svc.cluster.local.solar",
		},
		"sourceService":   "long-svc.testns.svc.cluster.local.solar",
		"sourceNamespace": "testns",
		"sourcePodName":   "long-pod",
	}

	emptySvcIn := map[string]interface{}{"targetUID": "kubernetes://empty.testns"}

	emptyServiceOut := map[string]interface{}{
		"targetLabels": map[string]string{
			"app": "",
		},
		"targetNamespace": "testns",
		"targetPodName":   "empty",
	}

	badTargetSvcIn := map[string]interface{}{"targetUID": "kubernetes://bad-svc-pod.testns"}

	badTargetOut := map[string]interface{}{
		"targetLabels": map[string]string{
			"app": ":",
		},
		"targetNamespace": "testns",
		"targetPodName":   "bad-svc-pod",
	}

	ipTargetSvcIn := map[string]interface{}{"targetIP": []uint8(net.ParseIP("192.168.234.3"))}

	ipTargetOut := map[string]interface{}{
		"targetLabels": map[string]string{
			"app": "ipAddr",
		},
		"targetNamespace": "testns",
		"targetPodName":   "ip-svc-pod",
		"targetService":   "ipAddr.testns.svc.cluster.local",
	}

	istioTargetSvcIn := map[string]interface{}{
		"targetUID": "kubernetes://istio-ingress.testns",
	}

	istioTargetOut := map[string]interface{}{
		"targetLabels": map[string]string{
			"istio": "ingress",
		},
		"targetNamespace": "testns",
		"targetPodName":   "istio-ingress",
		"targetService":   "ingress.testns.svc.cluster.local",
	}

	ipAppSvcIn := map[string]interface{}{
		"targetUID": "kubernetes://ipApp.testns",
	}

	ipAppTargetOut := map[string]interface{}{
		"targetLabels": map[string]string{
			"app": "10.1.10.1",
		},
		"targetNamespace": "testns",
		"targetPodName":   "ipApp",
	}

	tests := []struct {
		name   string
		inputs map[string]interface{}
		want   map[string]interface{}
	}{
		{"source pod and target service", sourceUIDIn, sourceUIDOut},
		{"alternate service canonicalization (namespace)", nsAppLabelIn, nsAppLabelOut},
		{"alternate service canonicalization (svc cluster)", svcClusterIn, svcClusterOut},
		{"alternate service canonicalization (long svc)", longSvcClusterIn, longSvcClusterOut},
		{"empty service", emptySvcIn, emptyServiceOut},
		{"bad target service", badTargetSvcIn, badTargetOut},
		{"target ip pod", ipTargetSvcIn, ipTargetOut},
		{"istio service", istioTargetSvcIn, istioTargetOut},
		{"ip app", ipAppSvcIn, ipAppTargetOut},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			got, err := kg.Generate(v.inputs)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			if !reflect.DeepEqual(got, v.want) {
				t.Errorf("Generate(): got %#v; want %#v", got, v.want)
				return
			}
		})
	}
}

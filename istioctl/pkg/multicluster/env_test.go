// Copyright Istio Authors.
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

package multicluster

import (
	"bytes"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/clientcmd/api/latest"

	"istio.io/istio/pkg/kube"
)

var fakeKubeconfigData = `apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: UEhPTlkK
    server: https://192.168.1.1
  name: prod0
contexts:
- context:
    cluster: prod0
    user: prod0
  name: prod0
current-context: prod0
users:
- name: prod0
  user:
    auth-provider:
      name: gcp` // nolint:lll

func createFakeKubeconfigFileOrDie(t *testing.T) (string, *api.Config) {
	t.Helper()

	f, err := os.CreateTemp("", "fakeKubeconfigForEnvironment")
	if err != nil {
		t.Fatalf("could not create fake kubeconfig file: %v", err)
	}
	kubeconfigPath := f.Name()
	defer func() {
		if err := f.Close(); err != nil {
			t.Errorf("couldn't close temp file %v: %v", kubeconfigPath, err)
		}
	}()

	if _, err = f.WriteString(fakeKubeconfigData); err != nil {
		t.Fatalf("could not write fake kubeconfig data to %v: %v", kubeconfigPath, err)
	}

	// Temporary workaround until https://github.com/kubernetes/kubernetes/pull/86414 merges
	into := &api.Config{
		Clusters:   map[string]*api.Cluster{},
		AuthInfos:  map[string]*api.AuthInfo{},
		Contexts:   map[string]*api.Context{},
		Extensions: map[string]runtime.Object{},
	}
	out, _, err := latest.Codec.Decode([]byte(fakeKubeconfigData), nil, into)
	if err != nil {
		t.Fatalf("could not decode fake kubeconfig: %v", err)
	}
	config, ok := out.(*api.Config)
	if !ok {
		t.Fatalf("decoded kubeconfig is not a valid api.Config (%v)", reflect.TypeOf(out))
	}

	// fill in missing defaults
	config.Contexts[config.CurrentContext].LocationOfOrigin = kubeconfigPath
	config.Clusters[config.CurrentContext].LocationOfOrigin = kubeconfigPath
	config.AuthInfos[config.CurrentContext].LocationOfOrigin = kubeconfigPath
	config.Extensions = make(map[string]runtime.Object)
	config.Preferences.Extensions = make(map[string]runtime.Object)

	return kubeconfigPath, config
}

type fakeEnvironment struct {
	KubeEnvironment

	client                  kube.CLIClient
	injectClientCreateError error
	kubeconfig              string
	wOut                    bytes.Buffer
	wErr                    bytes.Buffer
}

func newFakeEnvironmentOrDie(t *testing.T, minor string, config *api.Config, objs ...runtime.Object) *fakeEnvironment {
	t.Helper()

	var wOut, wErr bytes.Buffer

	f := &fakeEnvironment{
		KubeEnvironment: KubeEnvironment{
			config:     config,
			stdout:     &wOut,
			stderr:     &wErr,
			kubeconfig: "unused",
		},
		client:     kube.NewFakeClientWithVersion(minor, objs...),
		kubeconfig: "unused",
		wOut:       wOut,
		wErr:       wErr,
	}

	return f
}

func (f *fakeEnvironment) CreateClient(_ string) (kube.CLIClient, error) {
	if f.injectClientCreateError != nil {
		return nil, f.injectClientCreateError
	}
	return f.client, nil
}

func (f *fakeEnvironment) Poll(_, _ time.Duration, condition ConditionFunc) error {
	// TODO - add hooks to inject fake timeouts
	_, _ = condition()
	return nil
}

func TestNewEnvironment(t *testing.T) {
	context := "" // empty, use current-Context
	kubeconfig, wantConfig := createFakeKubeconfigFileOrDie(t)

	var wOut, wErr bytes.Buffer

	wantEnv := &KubeEnvironment{
		config:     wantConfig,
		stdout:     &wOut,
		stderr:     &wErr,
		kubeconfig: kubeconfig,
	}

	gotEnv, err := NewEnvironment(kubeconfig, context, &wOut, &wErr)
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(*wantEnv.GetConfig(), *gotEnv.GetConfig()); diff != "" {
		t.Errorf("bad config: \n got %v \nwant %v\ndiff %v", gotEnv, wantEnv, diff)
	}

	wantEnv.config = nil
	gotEnv.config = nil
	if !reflect.DeepEqual(wantEnv, gotEnv) {
		t.Errorf("bad environment: \n got %v \nwant %v", *gotEnv, *wantEnv)
	}

	// verify interface
	if gotEnv.Stderr() != &wErr {
		t.Errorf("Stderr() returned wrong io.writer")
	}
	if gotEnv.Stdout() != &wOut {
		t.Errorf("Stdout() returned wrong io.writer")
	}
	gotEnv.Printf("stdout %v", "test")
	wantOut := "stdout test"
	if gotOut := wOut.String(); gotOut != wantOut {
		t.Errorf("Printf() printed wrong string: got %v want %v", gotOut, wantOut)
	}
	gotEnv.Errorf("stderr %v", "test")
	wantErr := "stderr test"
	if gotErr := wErr.String(); gotErr != wantErr {
		t.Errorf("Errorf() printed wrong string: got %v want %v", gotErr, wantErr)
	}
}

// Copyright 2019 Istio Authors.
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
	"io/ioutil"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/clientcmd/api/latest"
)

var fakeKubeconfigData = `apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURERENDQWZTZ0F3SUJBZ0lSQU8xeS9mTmFIbUQyTVgzaEJWNStTUGN3RFFZSktvWklodmNOQVFFTEJRQXcKTHpFdE1Dc0dBMVVFQXhNa1pEZzFObUl5WmpjdE5EZGxOUzAwWWpCbUxUa3pOMk10TVRNMk1qVmxZamMyWlRVdwpNQjRYRFRFNU1UQXhNakF5TWpRek4xb1hEVEkwTVRBeE1EQXpNalF6TjFvd0x6RXRNQ3NHQTFVRUF4TWtaRGcxCk5tSXlaamN0TkRkbE5TMDBZakJtTFRrek4yTXRNVE0yTWpWbFlqYzJaVFV3TUlJQklqQU5CZ2txaGtpRzl3MEIKQVFFRkFBT0NBUThBTUlJQkNnS0NBUUVBd2xDUWc3WVdocG1GdkpVVkptS1Rtc2RuZTY4NnZtYTBjNEFFbWZudQpSMlp0d0Y4K3dQMGxHcUVMdUlPZmw0dWtwcHBWVUE3OGp4bXNNeW1jKzhBeTg5am5HcUlEdUVhYjY1RFFqZ0xrCkUxZ01CUk5WR2ZVYzkzVHN5U0JFVDlzcXJGNzJ4d2dwdER6RWZLREpWYVNQUkhlTy9qbG8yMnlzTVQyeXpLRzMKVzlaMjF2U0dPMGE2N2F6YUZ4QUNzMTJEUVpPRXlJQkhUckZYUHJMbXg4Z1pkQXcyWVU3K3lYbUtDZmVkYU9YNgpRUUNtZW8zVmRRRElaSUdteTIyYS9vVzBHTXZHdG5RdHc1K2lyR3hxUEw4Tm5wR05tWWNieEtNVTBCUGl6YW9GCjdpSk0wZTRBdWhqS29kd2dqc2QwRi9PQVZzUzlUQjFEWG8xMlJXUk1tcGw3a1FJREFRQUJveU13SVRBT0JnTlYKSFE4QkFmOEVCQU1DQWdRd0R3WURWUjBUQVFIL0JBVXdBd0VCL3pBTkJna3Foa2lHOXcwQkFRc0ZBQU9DQVFFQQptekM0YitLYVJRVkI0Tjl2dFlTNE5YeENLTDZPbzg0NXpmUzdHWkZicXN0MzdGbE1QcHRUWFB5NjdYbmtyckJYClhtMTZXVVZ2UUVVYktCeFdiOUJYZlhnZnhrWlkvbWJ3dHl6bzJXamRpZGZTTmNHUzJXdDlGVnhsMzhoaGV2YVQKcHJpMW9HYnMrTkJwSStVVzVHWEEyMWdqcE9NRGJBQitoT0VwaXRUcENMakJ0N0gzaUhuTGpIT1RaS3ZaaTl6bApRbElQZDV0N2JURDhPclJQTm55dUhnZFBLeHl4Tk5pSHRHeklZblVWbDdOQWF3TTQ1aVNoZndmR2NMUVhweWR2Cjdmb2VXUjF5anY4bkEyNnZIcDdRNFRlMXVDMi9UUG9FNXoybi8xbVpGMWJ1dWVrblpOeVZKVmhsbjhCSnNvUU8KWkVKb1o3VUNBVWdkdlVIK01DRTE5QT09Ci0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0K
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
      name: gcp`

func createFakeKubeconfigFileOrDie(t *testing.T, kubeconfig string) (string, *api.Config) {
	t.Helper()

	f, err := ioutil.TempFile("", "fakeKubeconfigForEnvironment")
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

	out, _, err := latest.Codec.Decode([]byte(fakeKubeconfigData), nil, nil)
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

	client                  *fake.Clientset
	injectClientCreateError error
	kubeconfig              string
	wOut                    bytes.Buffer
	wErr                    bytes.Buffer
}

func newFakeEnvironmentOrDie(t *testing.T, config *api.Config, objs ...runtime.Object) *fakeEnvironment {
	t.Helper()

	var wOut, wErr bytes.Buffer

	f := &fakeEnvironment{
		KubeEnvironment: KubeEnvironment{
			config:     config,
			stdout:     &wOut,
			stderr:     &wErr,
			kubeconfig: "unused",
		},
		client:     fake.NewSimpleClientset(objs...),
		kubeconfig: "unused",
		wOut:       wOut,
		wErr:       wErr,
	}

	return f
}

func (f *fakeEnvironment) CreateClientSet(context string) (kubernetes.Interface, error) {
	if f.injectClientCreateError != nil {
		return nil, f.injectClientCreateError
	}
	return f.client, nil
}

func TestNewEnvironment(t *testing.T) {
	context := "" // empty, use current-context
	kubeconfig, wantConfig := createFakeKubeconfigFileOrDie(t, fakeKubeconfigData)

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

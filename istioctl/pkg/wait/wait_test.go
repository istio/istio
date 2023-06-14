// Copyright Istio Authors
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

package wait

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config/schema/gvr"
)

func TestWaitCmd(t *testing.T) {
	cannedResponseObj := []xds.SyncedVersions{
		{
			ProxyID:         "foo",
			ClusterVersion:  "1",
			ListenerVersion: "1",
			RouteVersion:    "1",
		},
	}
	cannedResponse, _ := json.Marshal(cannedResponseObj)
	cannedResponseMap := map[string][]byte{"onlyonepilot": cannedResponse}

	distributionTrackingDisabledResponse := xds.DistributionTrackingDisabledMessage
	distributionTrackingDisabledResponseMap := map[string][]byte{"onlyonepilot": []byte(distributionTrackingDisabledResponse)}

	cases := []execTestCase{
		{
			execClientConfig: cannedResponseMap,
			args:             strings.Split("--generation=2 --timeout=20ms virtual-service foo.default", " "),
			wantException:    true,
		},
		{
			execClientConfig: cannedResponseMap,
			args:             strings.Split("--generation=1 virtual-service foo.default", " "),
			wantException:    false,
		},
		{
			execClientConfig: cannedResponseMap,
			args:             strings.Split("--generation=1 VirtualService foo.default", " "),
			wantException:    false,
		},
		{
			execClientConfig: cannedResponseMap,
			args:             strings.Split("--generation=1 not-service foo.default", " "),
			wantException:    true,
		},
		{
			execClientConfig: cannedResponseMap,
			args:             strings.Split("--timeout 20ms virtual-service bar.default", " "),
			wantException:    true,
			expectedOutput:   "Error: timeout expired before resource networking.istio.io/v1alpha3/VirtualService/default/bar became effective on all sidecars\n",
		},
		{
			execClientConfig: cannedResponseMap,
			args:             strings.Split("--timeout 2s virtualservice foo.default", " "),
			wantException:    false,
		},
		{
			execClientConfig: cannedResponseMap,
			args:             strings.Split("--timeout 2s --revision canary virtualservice foo.default", " "),
			wantException:    false,
		},
		{
			execClientConfig: distributionTrackingDisabledResponseMap,
			args:             strings.Split("--timeout 2s --revision canary virtualservice foo.default", " "),
			wantException:    true,
			expectedString:   distributionTrackingDisabledErrorString,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			_ = setupK8Sfake()
			verifyExecTestOutput(t, Cmd(cli.NewFakeContext(&cli.NewFakeContextOption{
				Namespace: "default",
				Results:   c.execClientConfig,
			})), c)
		})
	}
}

func setupK8Sfake() *fake.FakeDynamicClient {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	clientGetter = func(_ cli.Context) (dynamic.Interface, error) {
		return client, nil
	}
	l := sync.Mutex{}
	l.Lock()
	client.PrependWatchReactor("*", func(action ktesting.Action) (handled bool, ret watch.Interface, err error) {
		l.Unlock()
		return false, nil, nil
	})
	go func() {
		// wait till watch created, then send create events.
		// by default, k8s sends all existing objects at the beginning of a watch, but the test mock does not.  This
		// function forces the test to behave like kubernetes does, but creates a race condition on watch creation.
		l.Lock()
		x := client.Resource(gvr.VirtualService).Namespace("default")

		x.Create(context.TODO(),
			newUnstructured("networking.istio.io/v1alpha3", "virtualservice", "default", "foo", int64(1)),
			metav1.CreateOptions{})
		x.Create(context.TODO(),
			newUnstructured("networking.istio.io/v1alpha3", "virtualservice", "default", "bar", int64(3)),
			metav1.CreateOptions{})
	}()
	return client
}

type execTestCase struct {
	execClientConfig map[string][]byte
	args             []string

	// Typically use one of the three
	expectedOutput string // Expected constant output
	expectedString string // String output is expected to contain
	goldenFilename string // Expected output stored in golden file

	wantException bool
}

func verifyExecTestOutput(t *testing.T, cmd *cobra.Command, c execTestCase) {
	t.Helper()

	var out bytes.Buffer
	cmd.SetArgs(c.args)
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	fErr := cmd.Execute()
	output := out.String()

	if c.expectedOutput != "" && c.expectedOutput != output {
		t.Fatalf("Unexpected output for 'istioctl %s'\n got: %q\nwant: %q", strings.Join(c.args, " "), output, c.expectedOutput)
	}

	if c.expectedString != "" && !strings.Contains(output, c.expectedString) {
		t.Fatalf("Output didn't match for '%s %s'\n got %v\nwant: %v", cmd.Name(), strings.Join(c.args, " "), output, c.expectedString)
	}

	if c.goldenFilename != "" {
		util.CompareContent(t, []byte(output), c.goldenFilename)
	}

	if c.wantException {
		if fErr == nil {
			t.Fatalf("Wanted an exception for 'istioctl %s', didn't get one, output was %q",
				strings.Join(c.args, " "), output)
		}
	} else {
		if fErr != nil {
			t.Fatalf("Unwanted exception for 'istioctl %s': %v", strings.Join(c.args, " "), fErr)
		}
	}
}

func newUnstructured(apiVersion, kind, namespace, name string, generation int64) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]any{
				"namespace":  namespace,
				"name":       name,
				"generation": generation,
			},
		},
	}
}

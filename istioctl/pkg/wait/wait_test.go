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
	"testing"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
)

func TestWaitCmd(t *testing.T) {
	cannedResponseObj := []xds.SyncedVersions{
		{
			ProxyID:         "foo",
			ClusterVersion:  "1",
			ListenerVersion: "1",
			RouteVersion:    "1",
			EndpointVersion: "1",
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
			args:             strings.Split("--generation=1 VirtualService foo.default --proxy foo", " "),
			wantException:    false,
		},
		{
			execClientConfig: cannedResponseMap,
			args:             strings.Split("--generation=1 --timeout 20ms VirtualService foo.default --proxy not-proxy", " "),
			wantException:    true,
			expectedOutput:   "timeout expired before resource",
		},
		{
			execClientConfig: cannedResponseMap,
			args:             strings.Split("--generation=1 not-service foo.default", " "),
			wantException:    true,
			expectedOutput:   "type not-service is not recognized",
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
			expectedOutput:   distributionTrackingDisabledErrorString,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			cl := cli.NewFakeContext(&cli.NewFakeContextOption{
				Namespace: "default",
				Results:   c.execClientConfig,
			})
			k, err := cl.CLIClient()
			if err != nil {
				t.Fatal(err)
			}
			fc := k.Dynamic().(*dynamicfake.FakeDynamicClient)
			fc.PrependWatchReactor("*", func(action clienttesting.Action) (handled bool, ret watch.Interface, err error) {
				gvr := action.GetResource()
				ns := action.GetNamespace()
				watch, err := fc.Tracker().Watch(gvr, ns)
				if err != nil {
					return false, nil, err
				}
				// Kubernetes Fake watches do not add initial state, but real server does
				// https://kubernetes.io/docs/reference/using-api/api-concepts/#semantics-for-watch
				// Tracking in https://github.com/kubernetes/kubernetes/issues/123109
				gvk := gvk.MustFromGVR(gvr).Kubernetes()
				objs, err := fc.Tracker().List(gvr, gvk, ns)
				// This can happen for PartialMetadata objects unfortunately.. so just continue
				if err == nil {
					l, err := meta.ExtractList(objs)
					if err != nil {
						return false, nil, err
					}
					for _, object := range l {
						watch.(addObject).Add(object)
					}
				}
				return true, watch, nil
			})
			k.Dynamic().Resource(gvr.VirtualService).Namespace("default").Create(context.Background(),
				newUnstructured("networking.istio.io/v1alpha3", "virtualservice", "default", "foo", int64(1)),
				metav1.CreateOptions{})
			k.Dynamic().Resource(gvr.VirtualService).Namespace("default").Create(context.Background(),
				newUnstructured("networking.istio.io/v1alpha3", "virtualservice", "default", "bar", int64(3)),
				metav1.CreateOptions{})
			verifyExecTestOutput(t, Cmd(cl), c)
		})
	}
}

type addObject interface {
	Add(obj runtime.Object)
}

type execTestCase struct {
	execClientConfig map[string][]byte
	args             []string

	// Typically use one of the three
	expectedOutput string // Expected constant output

	wantException bool
}

func verifyExecTestOutput(t *testing.T, cmd *cobra.Command, c execTestCase) {
	if c.wantException {
		// Ensure tests do not hang for 30s
		c.args = append(c.args, "--timeout=20ms")
	}
	var out bytes.Buffer
	cmd.SetArgs(c.args)
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	fErr := cmd.Execute()
	output := out.String()

	if c.expectedOutput != "" && !strings.Contains(output, c.expectedOutput) {
		t.Fatalf("Unexpected output for 'istioctl %s'\n got: %q\nwant: %q", strings.Join(c.args, " "), output, c.expectedOutput)
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

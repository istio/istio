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

package inferencepool

import (
	"cmp"
	"encoding/json"
	"regexp"
	"strings"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
)

type TestStatusQueue struct {
	mu    sync.Mutex
	state map[status.Resource]any
}

var timestampRegex = regexp.MustCompile(`lastTransitionTime:.*`)

func (t *TestStatusQueue) EnqueueStatusUpdateResource(context any, target status.Resource) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state[target] = context
}

func (t *TestStatusQueue) Statuses() []any {
	t.mu.Lock()
	defer t.mu.Unlock()
	return maps.Values(t.state)
}

func (t *TestStatusQueue) Dump() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	sb := strings.Builder{}
	objs := []crd.IstioKind{}
	for k, v := range t.state {
		statusj, _ := json.Marshal(v)
		gk, _ := gvk.FromGVR(k.GroupVersionResource)
		obj := crd.IstioKind{
			TypeMeta: metav1.TypeMeta{
				Kind:       gk.Kind,
				APIVersion: k.GroupVersion().String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      k.Name,
				Namespace: k.Namespace,
			},
			Spec:   nil,
			Status: ptr.Of(json.RawMessage(statusj)),
		}
		objs = append(objs, obj)
	}
	slices.SortFunc(objs, func(a, b crd.IstioKind) int {
		ord := []string{gvk.GatewayClass.Kind, gvk.Gateway.Kind, gvk.HTTPRoute.Kind, gvk.GRPCRoute.Kind, gvk.TLSRoute.Kind, gvk.TCPRoute.Kind}
		if r := cmp.Compare(slices.Index(ord, a.Kind), slices.Index(ord, b.Kind)); r != 0 {
			return r
		}
		if r := a.CreationTimestamp.Time.Compare(b.CreationTimestamp.Time); r != 0 {
			return r
		}
		if r := cmp.Compare(a.Namespace, b.Namespace); r != 0 {
			return r
		}
		return cmp.Compare(a.Name, b.Name)
	})
	for _, obj := range objs {
		b, err := yaml.Marshal(obj)
		if err != nil {
			panic(err.Error())
		}
		// Replace parts that are not stable
		b = timestampRegex.ReplaceAll(b, []byte("lastTransitionTime: fake"))
		sb.WriteString(string(b))
		sb.WriteString("---\n")
	}
	return sb.String()
}

var _ status.Queue = &TestStatusQueue{}

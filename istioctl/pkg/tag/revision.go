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

package tag

import (
	"context"
	"fmt"

	admitv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/pkg/kube"
)

// PodFilteredInfo represents a small subset of fields from
// Pod object in Kubernetes. Exposed for integration test
type PodFilteredInfo struct {
	Namespace string          `json:"namespace"`
	Name      string          `json:"name"`
	Address   string          `json:"address"`
	Status    corev1.PodPhase `json:"status"`
	Age       string          `json:"age"`
}

// IstioOperatorCRInfo represents a tiny subset of fields from
// IstioOperator CR. This structure is used for displaying data.
// Exposed for integration test
type IstioOperatorCRInfo struct {
	IOP            *iopv1alpha1.IstioOperator `json:"-"`
	Namespace      string                     `json:"namespace"`
	Name           string                     `json:"name"`
	Profile        string                     `json:"profile"`
	Components     []string                   `json:"components,omitempty"`
	Customizations []IopDiff                  `json:"customizations,omitempty"`
}

type IopDiff struct {
	Path  string `json:"path"`
	Value string `json:"value"`
}

// MutatingWebhookConfigInfo represents a tiny subset of fields from
// MutatingWebhookConfiguration kubernetes object. This is exposed for
// integration tests only
type MutatingWebhookConfigInfo struct {
	Name     string `json:"name"`
	Revision string `json:"revision"`
	Tag      string `json:"tag,omitempty"`
}

// NsInfo represents namespace related information like pods running there.
// It is used to display data and is exposed for integration tests.
type NsInfo struct {
	Name string             `json:"name,omitempty"`
	Pods []*PodFilteredInfo `json:"pods,omitempty"`
}

// RevisionDescription is used to display revision related information.
// This is exposed for integration tests.
type RevisionDescription struct {
	IstioOperatorCRs   []*IstioOperatorCRInfo       `json:"istio_operator_crs,omitempty"`
	Webhooks           []*MutatingWebhookConfigInfo `json:"webhooks,omitempty"`
	ControlPlanePods   []*PodFilteredInfo           `json:"control_plane_pods,omitempty"`
	IngressGatewayPods []*PodFilteredInfo           `json:"ingess_gateways,omitempty"`
	EgressGatewayPods  []*PodFilteredInfo           `json:"egress_gateways,omitempty"`
	NamespaceSummary   map[string]*NsInfo           `json:"namespace_summary,omitempty"`
}

func ListRevisionDescriptions(client kube.CLIClient) (map[string]*RevisionDescription, error) {
	revisions := map[string]*RevisionDescription{}

	// Get a list of control planes which are installed in remote clusters
	// In this case, it is possible that they only have webhooks installed.
	webhooks, err := Webhooks(context.Background(), client)
	if err != nil {
		return nil, fmt.Errorf("error while listing mutating webhooks: %v", err)
	}
	for _, hook := range webhooks {
		rev := renderWithDefault(hook.GetLabels()[label.IoIstioRev.Name], DefaultRevisionName)
		tagLabel := hook.GetLabels()[IstioTagLabel]
		ri, revPresent := revisions[rev]
		if revPresent {
			if tagLabel != "" {
				ri.Webhooks = append(ri.Webhooks, &MutatingWebhookConfigInfo{
					Name:     hook.Name,
					Revision: rev,
					Tag:      tagLabel,
				})
			}
		} else {
			revisions[rev] = &RevisionDescription{
				IstioOperatorCRs: []*IstioOperatorCRInfo{},
				Webhooks:         []*MutatingWebhookConfigInfo{{Name: hook.Name, Revision: rev, Tag: tagLabel}},
			}
		}
	}

	return revisions, nil
}

func Webhooks(ctx context.Context, client kube.CLIClient) ([]admitv1.MutatingWebhookConfiguration, error) {
	hooks, err := client.Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, metav1.ListOptions{})
	if err != nil {
		return []admitv1.MutatingWebhookConfiguration{}, err
	}
	return hooks.Items, nil
}

func renderWithDefault(s, def string) string {
	if s != "" {
		return s
	}
	return def
}

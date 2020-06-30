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

package convert

import (
	"strings"

	"k8s.io/api/networking/v1beta1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pilot/pkg/config/kube/ingress"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
)

// IstioIngresses converts K8s extensions/v1beta1 Ingresses with Istio rules to v1alpha3 gateway and virtual service
func IstioIngresses(ingresses []*v1beta1.Ingress, domainSuffix string, client kubernetes.Interface) ([]model.Config, error) {

	if len(ingresses) == 0 {
		return make([]model.Config, 0), nil
	}
	if len(domainSuffix) == 0 {
		domainSuffix = constants.DefaultKubernetesDomain
	}
	ingressByHost := map[string]*model.Config{}
	for _, ingrezz := range ingresses {
		ingress.ConvertIngressVirtualService(*ingrezz, domainSuffix, ingressByHost, &serviceListerWrapper{client: client})
	}

	out := make([]model.Config, 0, len(ingressByHost))
	for _, vs := range ingressByHost {
		// Ensure name is valid; ConvertIngressVirtualService will create a name that doesn't start with alphanumeric
		if strings.HasPrefix(vs.Name, "-") {
			vs.Name = "wild" + vs.Name
		}
		out = append(out, *vs)
	}

	return out, nil
}

//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package ingress

import (
	"istio.io/istio/galley/pkg/runtime/conversions"
	"istio.io/istio/galley/pkg/runtime/conversions/envelope"
	"istio.io/istio/galley/pkg/runtime/resource"
	"k8s.io/api/extensions/v1beta1"
)

func toEnvelopedGateway(entry resource.Entry) (interface{}, error) {
	ingress := entry.Item.(*v1beta1.IngressSpec) // TODO
	gwEntry := conversions.IngressToGateway(entry.ID, ingress)
	enveloped, err := envelope.Envelope(gwEntry)
	if err != nil {
		scope.Errorf("Unable to envelope and store resource %q: %v", gwEntry.ID.String(), err)
		return nil, err
	}

	return enveloped, err
}

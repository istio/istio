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

package components

import (
	"testing"

	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/ids"
)

// Ingress represents a deployed Ingress Gateway instance.
type Ingress interface {
	component.Instance
	// Address returns the external HTTP address of the ingress gateway (or the NodePort address,
	// when running under Minikube).
	Address() string

	//  Call makes an HTTP call through ingress, where the URL has the given path.
	Call(path string) (IngressCallResponse, error)
}

// IngressCallResponse is the result of a call made through Istio Ingress.
type IngressCallResponse struct {
	// Response status code
	Code int

	// Response body
	Body string
}

// GetIngress from the repository
func GetIngress(e component.Repository, t testing.TB) Ingress {
	return e.GetComponentOrFail("", ids.Ingress, t).(Ingress)
}

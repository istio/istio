// Copyright 2019 Istio Authors
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

package istiocontrolplane

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/helmreconciler"
)

type IstioRenderingCustomizerFactory struct{}

var _ helmreconciler.RenderingCustomizerFactory

// NewCustomizer returns a RenderingCustomizer for Istio
func (f *IstioRenderingCustomizerFactory) NewCustomizer(instance runtime.Object) (helmreconciler.RenderingCustomizer, error) {
	switch v := instance.(type) {
	case *v1alpha2.IstioControlPlane:
		return &helmreconciler.SimpleRenderingCustomizer{
			InputValue:          NewIstioRenderingInput(v),
			PruningDetailsValue: NewIstioPruningDetails(v),
			ListenerValue:       NewIstioRenderingListener(v),
		}, nil
	default:
		return nil, fmt.Errorf("object is not an IstioControlPlane resource")
	}
}

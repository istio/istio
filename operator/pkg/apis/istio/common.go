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

package istio

import (
	"fmt"

	"sigs.k8s.io/yaml"

	operator_v1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
)

// UnmarshalIstioOperator unmarshals a string containing IstioOperator YAML.
func UnmarshalIstioOperator(iopYAML string, allowUnknownField bool) (*operator_v1alpha1.IstioOperator, error) {
	iop := &operator_v1alpha1.IstioOperator{}
	if allowUnknownField {
		if err := yaml.Unmarshal([]byte(iopYAML), iop); err != nil {
			return nil, fmt.Errorf("could not unmarshal: %v", err)
		}
	} else {
		if err := yaml.UnmarshalStrict([]byte(iopYAML), iop); err != nil {
			return nil, fmt.Errorf("could not unmarshal: %v", err)
		}
	}
	return iop, nil
}

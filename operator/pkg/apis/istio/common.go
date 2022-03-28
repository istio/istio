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

	"istio.io/api/operator/v1alpha1"
	operator_v1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/validate"
)

// UnmarshalAndValidateIOPS unmarshals a string containing IstioOperator YAML, validates it, and returns a struct
// representation if successful. In case of validation errors, it returns both the IstioOperatorSpec struct and
// an error, so the caller can decide how to handle it.
func UnmarshalAndValidateIOPS(iopsYAML string) (*v1alpha1.IstioOperatorSpec, error) {
	iops := &v1alpha1.IstioOperatorSpec{}
	if err := util.UnmarshalWithJSONPB(iopsYAML, iops, false); err != nil {
		return nil, fmt.Errorf("could not unmarshal the merged YAML: %s\n\nYAML:\n%s", err, iopsYAML)
	}
	if errs := validate.CheckIstioOperatorSpec(iops, true); len(errs) != 0 {
		return iops, fmt.Errorf(errs.Error())
	}
	return iops, nil
}

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

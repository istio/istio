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

package validate

import (
	"fmt"

	"istio.io/istio/operator/pkg/version"

	"github.com/ghodss/yaml"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/util"
)

var (
	// defaultValidations maps a data path to a validation function.
	defaultValuesValidations = map[string]ValidatorFunc{
		"global.proxy.includeIPRanges":     validateIPRangesOrStar,
		"global.proxy.excludeIPRanges":     validateIPRangesOrStar,
		"global.proxy.includeInboundPorts": validateStringList(validatePortNumberString),
		"global.proxy.excludeInboundPorts": validateStringList(validatePortNumberString),
	}
)

// CheckValues validates the values in the given tree, which follows the Istio values.yaml schema.
func CheckValues(root map[string]interface{}) util.Errors {
	vs, err := yaml.Marshal(root)
	if err != nil {
		return util.Errors{err}
	}
	val := &v1alpha1.Values{}
	if err := util.UnmarshalValuesWithJSONPB(string(vs), val, false); err != nil {
		return util.Errors{err}
	}
	return validateValues(root, nil)
}

// CheckValues validates the values in the given tree string, which follows the Istio values.yaml schema.
func CheckValuesString(vs []byte) util.Errors {
	var yamlTree = make(map[string]interface{})
	err := yaml.Unmarshal(vs, &yamlTree)
	if err != nil {
		return util.Errors{fmt.Errorf("values.yaml string failed validation: %v", err)}
	}
	return CheckValues(yamlTree)
}

func validateValues(node interface{}, path util.Path) (errs util.Errors) {
	pstr := path.String()
	scope.Debugf("validateValues %s", pstr)
	vf := defaultValuesValidations[pstr]
	if vf != nil {
		errs = util.AppendErrs(errs, vf(path, node))
	}

	nn, ok := node.(map[string]interface{})
	if !ok {
		// Leaf, nothing more to recurse.
		return errs
	}
	for k, v := range nn {
		errs = util.AppendErrs(errs, validateValues(v, append(path, k)))
	}

	return errs
}

// GenValidateError generates error with helpful message when input fails values.yaml schema validation
func GenValidateError(mvs version.MinorVersion, err error) error {
	vs := fmt.Sprintf("releaese-%s.%d", mvs.MajorVersion, mvs.Minor)
	return fmt.Errorf("the input values.yaml fail validation: %v\n"+
		"check against https://github.com/istio/istio/blob/%s/operator/pkg/apis/istio/v1alpha1/values_types.proto for schema\n"+
		"or run the command with --force flag to ignore the error", err, vs)
}

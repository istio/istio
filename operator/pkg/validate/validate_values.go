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

package validate

import (
	"reflect"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
)

// DefaultValuesValidations maps a data path to a validation function.
var DefaultValuesValidations = map[string]ValidatorFunc{
	"global.proxy.includeIPRanges":                    validateIPRangesOrStar,
	"global.proxy.excludeIPRanges":                    validateIPRangesOrStar,
	"global.proxy.includeInboundPorts":                validateStringList(validatePortNumberString),
	"global.proxy.excludeInboundPorts":                validateStringList(validatePortNumberString),
	"global.multiCluster.clusterName":                 validateDNS1123domain,
	"global.meshNetworks.*.endpoints[*].fromRegistry": validateDNS1123domain,

	"meshConfig": validateMeshConfig,
}

// CheckValues validates the values in the given tree, which follows the Istio values.yaml schema.
func CheckValues(root any) util.Errors {
	v := reflect.ValueOf(root)
	if root == nil || (v.Kind() == reflect.Ptr && v.IsNil()) {
		return nil
	}
	vs, err := util.ToYAMLGeneric(root)
	if err != nil {
		return util.Errors{err}
	}
	val := &v1alpha1.Values{}
	if err := util.UnmarshalWithJSONPB(string(vs), val, false); err != nil {
		return util.Errors{err}
	}
	return ValuesValidate(DefaultValuesValidations, root, nil)
}

// ValuesValidate validates the values of the tree using the supplied Func
func ValuesValidate(validations map[string]ValidatorFunc, node any, path util.Path) (errs util.Errors) {
	for pstr, validator := range validations {
		scope.Debugf("ValuesValidate %s", pstr)
		v, f, _ := tpath.GetFromStructPath(node, pstr)
		if f {
			errs = util.AppendErrs(errs, validator(util.PathFromString(pstr), v))
		}
	}
	return errs.Dedup()
}

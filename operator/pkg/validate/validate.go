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
	"fmt"
	"reflect"
	"sort"
	"strings"

	"google.golang.org/protobuf/types/known/structpb"
	"k8s.io/apimachinery/pkg/util/json"

	"istio.io/api/operator/v1alpha1"
	operator_v1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/metrics"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/util/protomarshal"
)

var (
	// DefaultValidations maps a data path to a validation function.
	DefaultValidations = map[string]ValidatorFunc{
		"values": func(path util.Path, i any) util.Errors {
			return CheckValues(i)
		},
		"meshConfig":                         validateMeshConfig,
		"hub":                                validateHub,
		"tag":                                validateTag,
		"revision":                           validateDNS1123Label,
		"components.ingressGateways[*].name": validateDNS1123Label,
		"components.egressGateways[*].name":  validateDNS1123Label,
		"components.ingressGateways[*].k8s.service.ports[*].name": validateDNS1123Label,
		"components.egressGateways[*].k8s.service.ports[*].name":  validateDNS1123Label,
		"components.ingressGateways[*].namespace":                 validateDNS1123Label,
		"components.egressGateways[*].namespace":                  validateDNS1123Label,
	}
	// requiredValues lists all the values that must be non-empty.
	requiredValues = map[string]bool{}
)

// CheckIstioOperator validates the operator CR.
func CheckIstioOperator(iop *operator_v1alpha1.IstioOperator, checkRequiredFields bool) error {
	if iop == nil {
		return nil
	}

	errs := CheckIstioOperatorSpec(iop.Spec, checkRequiredFields)
	return errs.ToError()
}

// CheckIstioOperatorSpec validates the values in the given Installer spec, using the field map DefaultValidations to
// call the appropriate validation function. checkRequiredFields determines whether missing mandatory fields generate
// errors.
func CheckIstioOperatorSpec(is *v1alpha1.IstioOperatorSpec, checkRequiredFields bool) (errs util.Errors) {
	if is == nil {
		return util.Errors{}
	}

	return Validate2(DefaultValidations, is)
}

func Validate2(validations map[string]ValidatorFunc, iop *v1alpha1.IstioOperatorSpec) (errs util.Errors) {
	// Convert to map[string]interface{} to avoid GatewaySpec not found.
	iopMap, err := iopSpecToMap(iop)
	if err != nil {
		return util.NewErrs(err)
	}

	// Sort the validation keys for deterministic order.
	sortedKeys := make([]string, 0, len(validations))
	for k := range validations {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	for _, path := range sortedKeys {
		validator := validations[path]
		v, f, _ := tpath.GetFromStructPath(iopMap, path)
		if f {
			if err := validator(util.PathFromString(path), v); err.ToError() != nil {
				errs = append(errs, err...)
			}
		}
	}
	return
}

func iopSpecToMap(spec *v1alpha1.IstioOperatorSpec) (map[string]interface{}, error) {
	jsonData, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal IstioOperatorSpec: %v", err)
	}
	var specMap map[string]interface{}
	err = json.Unmarshal(jsonData, &specMap)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal IstioOperatorSpec JSON: %v", err)
	}
	return specMap, nil
}

// Validate function below is used by third party for integrations and has to be public

// Validate validates the values of the tree using the supplied Func.
func Validate(validations map[string]ValidatorFunc, structPtr any, path util.Path, checkRequired bool) (errs util.Errors) {
	scope.Debugf("validate with path %s, %v (%T)", path, structPtr, structPtr)
	if structPtr == nil {
		return nil
	}
	if util.IsStruct(structPtr) {
		scope.Debugf("validate path %s, skipping struct type %T", path, structPtr)
		return nil
	}
	if !util.IsPtr(structPtr) {
		metrics.CRValidationErrorTotal.Increment()
		return util.NewErrs(fmt.Errorf("validate path %s, value: %v, expected ptr, got %T", path, structPtr, structPtr))
	}
	structElems := reflect.ValueOf(structPtr).Elem()
	if !util.IsStruct(structElems) {
		metrics.CRValidationErrorTotal.Increment()
		return util.NewErrs(fmt.Errorf("validate path %s, value: %v, expected struct, got %T", path, structElems, structElems))
	}

	if util.IsNilOrInvalidValue(structElems) {
		return
	}

	for i := 0; i < structElems.NumField(); i++ {
		fieldName := structElems.Type().Field(i).Name
		fieldValue := structElems.Field(i)
		if !fieldValue.CanInterface() {
			continue
		}
		kind := structElems.Type().Field(i).Type.Kind()
		if a, ok := structElems.Type().Field(i).Tag.Lookup("json"); ok && a == "-" {
			continue
		}

		scope.Debugf("Checking field %s", fieldName)
		switch kind {
		case reflect.Struct:
			errs = util.AppendErrs(errs, Validate(validations, fieldValue.Addr().Interface(), append(path, fieldName), checkRequired))
		case reflect.Map:
			newPath := append(path, fieldName)
			errs = util.AppendErrs(errs, validateLeaf(validations, newPath, fieldValue.Interface(), checkRequired))
			for _, key := range fieldValue.MapKeys() {
				nnp := append(newPath, key.String())
				errs = util.AppendErrs(errs, validateLeaf(validations, nnp, fieldValue.MapIndex(key), checkRequired))
			}
		case reflect.Slice:
			for i := 0; i < fieldValue.Len(); i++ {
				newValue := fieldValue.Index(i).Interface()
				newPath := append(path, indexPathForSlice(fieldName, i))
				if util.IsStruct(newValue) || util.IsPtr(newValue) {
					errs = util.AppendErrs(errs, Validate(validations, newValue, newPath, checkRequired))
				} else {
					errs = util.AppendErrs(errs, validateLeaf(validations, newPath, newValue, checkRequired))
				}
			}
		case reflect.Ptr:
			if util.IsNilOrInvalidValue(fieldValue.Elem()) {
				continue
			}
			newPath := append(path, fieldName)
			if fieldValue.Elem().Kind() == reflect.Struct {
				errs = util.AppendErrs(errs, Validate(validations, fieldValue.Interface(), newPath, checkRequired))
			} else {
				errs = util.AppendErrs(errs, validateLeaf(validations, newPath, fieldValue, checkRequired))
			}
		default:
			if structElems.Field(i).CanInterface() {
				errs = util.AppendErrs(errs, validateLeaf(validations, append(path, fieldName), fieldValue.Interface(), checkRequired))
			}
		}
	}
	if len(errs) > 0 {
		metrics.CRValidationErrorTotal.Increment()
	}
	return errs
}

func validateLeaf(validations map[string]ValidatorFunc, path util.Path, val any, checkRequired bool) util.Errors {
	pstr := path.String()
	msg := fmt.Sprintf("validate %s:%v(%T) ", pstr, val, val)
	if util.IsValueNil(val) || util.IsEmptyString(val) {
		if checkRequired && requiredValues[pstr] {
			return util.NewErrs(fmt.Errorf("field %s is required but not set", util.ToYAMLPathString(pstr)))
		}
		msg += fmt.Sprintf("validate %s: OK (empty value)", pstr)
		scope.Debug(msg)
		return nil
	}

	vf, ok := getValidationFuncForPath(validations, path)
	if !ok {
		msg += fmt.Sprintf("validate %s: OK (no validation)", pstr)
		scope.Debug(msg)
		// No validation defined.
		return nil
	}
	scope.Debug(msg)
	return vf(path, val)
}

func validateMeshConfig(path util.Path, root any) util.Errors {
	vs, err := util.ToYAMLGeneric(root)
	if err != nil {
		return util.Errors{err}
	}
	// ApplyMeshConfigDefaults allows unknown fields, so we first check for unknown fields
	if err := protomarshal.ApplyYAMLStrict(string(vs), mesh.DefaultMeshConfig()); err != nil {
		return util.Errors{fmt.Errorf("failed to unmarshall mesh config: %v", err)}
	}
	// This method will also perform validation automatically
	if _, validErr := mesh.ApplyMeshConfigDefaults(string(vs)); validErr != nil {
		return util.Errors{validErr}
	}
	return nil
}

func validateHub(path util.Path, val any) util.Errors {
	if val == "" {
		return nil
	}
	return validateWithRegex(path, val, ReferenceRegexp)
}

func validateTag(path util.Path, val any) util.Errors {
	switch t := val.(type) {
	case *structpb.Value:
		return validateWithRegex(path, t.GetStringValue(), TagRegexp)
	case string:
		return validateWithRegex(path, t, TagRegexp)
	}
	return nil
}

func validateDNS1123Label(_ util.Path, val any) util.Errors {
	if val == nil {
		return nil
	}
	validateFunc := func(name string) error {
		if name == "" {
			return nil
		}
		if !labels.IsDNS1123Label(name) {
			return fmt.Errorf("invalid DNS1123 label specified: %s", name)
		}
		return nil
	}
	switch t := val.(type) {
	case []any:
		errs := util.Errors{}
		for _, v := range t {
			if err := validateFunc(v.(string)); err != nil {
				errs = append(errs, err)
			}
		}
		return errs
	}
	if err := validateFunc(val.(string)); err != nil {
		return util.Errors{err}
	}
	return nil
}

func validateDNS1123domain(_ util.Path, val any) util.Errors {
	validateFunc := func(domain string) error {
		if len(domain) > 253 {
			return fmt.Errorf("invalid DNS1123 domain %q (longer than 253 chars)", val)
		}
		parts := strings.Split(domain, ".")
		for i, label := range parts {
			// Allow the last part to be empty, for unambiguous names like `istio.io.`
			if i == len(parts)-1 && label == "" {
				return nil
			}
			if !labels.IsDNS1123Label(label) {
				return fmt.Errorf("invalid DNS1123 domain %q", domain)
			}
		}
		return nil
	}
	switch t := val.(type) {
	case []any:
		errs := util.Errors{}
		for _, v := range t {
			if err := validateFunc(v.(string)); err != nil {
				errs = append(errs, err)
			}
		}
		return errs
	}
	if err := validateFunc(val.(string)); err != nil {
		return util.Errors{err}
	}
	return nil
}

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

package validation

import (
	"fmt"
	"reflect"

	"istio.io/api/operator/v1alpha1"
	valuesv1alpha1 "istio.io/operator/pkg/apis/istio/v1alpha1"
	"istio.io/operator/pkg/util"
)

const (
	validationMethodName = "Validate"
)

// ValidateConfig  calls validation func for every defined element in Values
func ValidateConfig(failOnMissingValidation bool, values *valuesv1alpha1.Values, iopls *v1alpha1.IstioOperatorSpec) util.Errors {
	var validationErrors util.Errors
	validationErrors = util.AppendErrs(validationErrors, validateSubTypes(reflect.ValueOf(values).Elem(), failOnMissingValidation, values, iopls))
	validationErrors = util.AppendErrs(validationErrors, validateFeatures(values, iopls))
	return validationErrors
}

// validateFeatures check whether the config sematically make sense. For example, feature X and feature Y can't be enabled together.
func validateFeatures(values *valuesv1alpha1.Values, _ *v1alpha1.IstioOperatorSpec) util.Errors {
	// When automatic mutual TLS is enabled, we check control plane security must also be enabled.
	g := values.GetGlobal()
	if g == nil {
		return nil
	}
	m := g.GetMtls()
	if m == nil {
		return nil
	}
	if m.GetAuto().Value && !g.GetControlPlaneSecurityEnabled().Value {
		return []error{fmt.Errorf("security: auto mtls is enabled, but control plane security is not enabled")}
	}
	return nil
}

func validateSubTypes(e reflect.Value, failOnMissingValidation bool, values *valuesv1alpha1.Values, iopls *v1alpha1.IstioOperatorSpec) util.Errors {
	// Dealing with receiver pointer and receiver value
	ptr := e
	k := e.Kind()
	if k == reflect.Ptr || k == reflect.Interface {
		e = e.Elem()
	}
	// check for method on value
	method := e.MethodByName(validationMethodName)
	if !method.IsValid() {
		method = ptr.MethodByName(validationMethodName)
	}

	var validationErrors util.Errors
	if util.IsNilOrInvalidValue(method) {
		if failOnMissingValidation {
			validationErrors = append(validationErrors, fmt.Errorf("type %s is missing Validation method", e.Type().String()))
		}
	} else {
		r := method.Call([]reflect.Value{reflect.ValueOf(failOnMissingValidation), reflect.ValueOf(values), reflect.ValueOf(iopls)})[0].Interface().(util.Errors)
		if len(r) != 0 {
			validationErrors = append(validationErrors, r...)
		}
	}
	// If it is not a struct nothing to do, returning previously collected validation errors
	if e.Kind() != reflect.Struct {
		return validationErrors
	}
	for i := 0; i < e.NumField(); i++ {
		// Corner case of a slice of something, if something is defined type, then process it recursiveley.
		if e.Field(i).Kind() == reflect.Slice {
			validationErrors = append(validationErrors, processSlice(e.Field(i), failOnMissingValidation, values, iopls)...)
			continue
		}
		if e.Field(i).Kind() == reflect.Map {
			validationErrors = append(validationErrors, processMap(e.Field(i), failOnMissingValidation, values, iopls)...)
			continue
		}
		// Validation is not required if it is not a defined type
		if e.Field(i).Kind() != reflect.Interface && e.Field(i).Kind() != reflect.Ptr {
			continue
		}
		val := e.Field(i).Elem()
		if util.IsNilOrInvalidValue(val) {
			continue
		}
		validationErrors = append(validationErrors, validateSubTypes(e.Field(i), failOnMissingValidation, values, iopls)...)
	}

	return validationErrors
}

func processSlice(e reflect.Value, failOnMissingValidation bool, values *valuesv1alpha1.Values, iopls *v1alpha1.IstioOperatorSpec) util.Errors {
	var validationErrors util.Errors
	for i := 0; i < e.Len(); i++ {
		validationErrors = append(validationErrors, validateSubTypes(e.Index(i), failOnMissingValidation, values, iopls)...)
	}

	return validationErrors
}

func processMap(e reflect.Value, failOnMissingValidation bool, values *valuesv1alpha1.Values, iopls *v1alpha1.IstioOperatorSpec) util.Errors {
	var validationErrors util.Errors
	for _, k := range e.MapKeys() {
		v := e.MapIndex(k)
		validationErrors = append(validationErrors, validateSubTypes(v, failOnMissingValidation, values, iopls)...)
	}

	return validationErrors
}

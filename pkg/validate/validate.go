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
	"net/url"
	"reflect"
	"strings"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/util"
)

var (
	// defaultValidations maps a data path to a validation function.
	defaultValidations = map[string]ValidatorFunc{
		"Hub":                    validateHub,
		"Tag":                    validateTag,
		"BaseSpecPath":           validateInstallPackagePath,
		"CustomPackagePath":      validateInstallPackagePath,
		"DefaultNamespacePrefix": validateDefaultNamespacePrefix,
	}

	// requiredValues lists all the values that must be non-empty.
	requiredValues = map[string]bool{}
)

// CheckIstioControlPlaneSpec validates the values in the given Installer spec, using the field map defaultValidations to
// call the appropriate validation function.
func CheckIstioControlPlaneSpec(is *v1alpha2.IstioControlPlaneSpec) util.Errors {
	return validate(defaultValidations, is, nil)
}

func validate(validations map[string]ValidatorFunc, structPtr interface{}, path util.Path) (errs util.Errors) {
	dbgPrint("validate with path %s, %v (%T)", path, structPtr, structPtr)
	if structPtr == nil {
		return nil
	}
	if reflect.TypeOf(structPtr).Kind() == reflect.Struct {
		dbgPrint("validate path %s, skipping struct type %T", path, structPtr)
		return nil
	}
	if reflect.TypeOf(structPtr).Kind() != reflect.Ptr {
		return util.NewErrs(fmt.Errorf("validate path %s, value: %v, expected ptr, got %T", path, structPtr, structPtr))
	}
	structElems := reflect.ValueOf(structPtr).Elem()
	if reflect.TypeOf(structElems).Kind() != reflect.Struct {
		return util.NewErrs(fmt.Errorf("validate path %s, value: %v, expected struct, got %T", path, structElems, structElems))
	}

	if util.IsNilOrInvalidValue(structElems) {
		return
	}

	for i := 0; i < structElems.NumField(); i++ {
		fieldName := structElems.Type().Field(i).Name
		fieldValue := structElems.Field(i)
		kind := structElems.Type().Field(i).Type.Kind()
		if a, ok := structElems.Type().Field(i).Tag.Lookup("json"); ok && a == "-" {
			continue
		}

		dbgPrint("Checking field %s", fieldName)
		switch kind {
		case reflect.Struct:
			errs = util.AppendErrs(errs, validate(validations, fieldValue.Addr().Interface(), append(path, fieldName)))
		case reflect.Map:
			newPath := append(path, fieldName)
			for _, key := range fieldValue.MapKeys() {
				nnp := append(newPath, key.String())
				errs = util.AppendErrs(errs, validateLeaf(validations, nnp, fieldValue.MapIndex(key)))
			}
		case reflect.Slice:
			for i := 0; i < fieldValue.Len(); i++ {
				errs = util.AppendErrs(errs, validate(validations, fieldValue.Index(i).Interface(), path))
			}
		case reflect.Ptr:
			if util.IsNilOrInvalidValue(fieldValue.Elem()) {
				continue
			}
			newPath := append(path, fieldName)
			if fieldValue.Elem().Kind() == reflect.Struct {
				errs = util.AppendErrs(errs, validate(validations, fieldValue.Interface(), newPath))
			} else {
				errs = util.AppendErrs(errs, validateLeaf(validations, newPath, fieldValue))
			}
		default:
			if structElems.Field(i).CanInterface() {
				errs = util.AppendErrs(errs, validateLeaf(validations, append(path, fieldName), fieldValue.Interface()))
			}
		}
	}
	return errs
}

func validateLeaf(validations map[string]ValidatorFunc, path util.Path, val interface{}) util.Errors {
	pstr := path.String()
	dbgPrintC("validate %s:%v(%T) ", pstr, val, val)
	if !requiredValues[pstr] && (util.IsValueNil(val) || util.IsEmptyString(val)) {
		// TODO(mostrowski): handle required fields.
		dbgPrint("validate %s: OK (empty value)", pstr)
		return nil
	}

	vf, ok := validations[pstr]
	if !ok {
		dbgPrint("validate %s: OK (no validation)", pstr)
		// No validation defined.
		return nil
	}
	return vf(path, val)
}

func validateHub(path util.Path, val interface{}) util.Errors {
	return validateWithRegex(path, val, ReferenceRegexp)
}

func validateTag(path util.Path, val interface{}) util.Errors {
	return validateWithRegex(path, val, TagRegexp)
}

func validateDefaultNamespacePrefix(path util.Path, val interface{}) util.Errors {
	return validateWithRegex(path, val, ObjectNameRegexp)
}

func validateInstallPackagePath(path util.Path, val interface{}) util.Errors {
	valStr, ok := val.(string)
	if !ok {
		return util.NewErrs(fmt.Errorf("validateInstallPackagePath(%s) bad type %T, want string", path, val))
	}

	if valStr == "" {
		// compiled-in charts
		return nil
	}

	if _, err := url.ParseRequestURI(val.(string)); err != nil {
		return util.NewErrs(fmt.Errorf("invalid value %s:%s", path, valStr))
	}

	validPrefixes := []string{util.LocalFilePrefix}
	validPrefixDesc := map[string]string{
		util.LocalFilePrefix: "RFC 8089 absolute path to file e.g. file:///var/istio/config.yaml",
	}

	for _, vp := range validPrefixes {
		if strings.HasPrefix(valStr, vp) {
			return nil
		}
	}

	errStr := fmt.Sprintf("Invalid path prefix for %s: %s. \nMust be one of the following:\n", path, valStr)
	for _, vp := range validPrefixes {
		errStr += fmt.Sprintf("%-15s %s", vp, validPrefixDesc[vp])
	}
	return util.NewErrs(fmt.Errorf(errStr))
}

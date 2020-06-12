//  Copyright Istio Authors
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

/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kube

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	ejson "github.com/exponent-io/jsonpath"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubeErrors "k8s.io/apimachinery/pkg/util/errors"
	kubeJson "k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/kube-openapi/pkg/util/proto/validation"
)

// groupVersionKindExtensionKey is the key used to lookup the
// GroupVersionKind value for an object definition from the
// definition's "extensions" map.
const groupVersionKindExtensionKey = "x-kubernetes-group-version-kind"

var _ resource.ContentValidator = &openAPISchemaValidator{}

// SchemaValidation validates the object against an OpenAPI schema.
type openAPISchemaValidator struct {
	schema *openAPISchema
}

func newValidator(schemas *openAPISchema) resource.ContentValidator {
	return conjunctiveValidator{
		newOpenAPISchemaValidator(schemas),
		noDoubleKeyValidator{},
	}
}

// newOpenAPISchemaValidator creates a new SchemaValidation that can be used
// to validate objects.
func newOpenAPISchemaValidator(schema *openAPISchema) resource.ContentValidator {
	return &openAPISchemaValidator{
		schema: schema,
	}
}

// ValidateBytes will validates the object against using the Resources
// object.
func (v *openAPISchemaValidator) ValidateBytes(data []byte) error {
	obj, err := parse(data)
	if err != nil {
		return err
	}

	gvk, errs := getObjectKind(obj)
	if errs != nil {
		return kubeErrors.NewAggregate(errs)
	}

	if (gvk == schema.GroupVersionKind{Version: "v1", Kind: "List"}) {
		return kubeErrors.NewAggregate(v.validateList(obj))
	}

	return kubeErrors.NewAggregate(v.validateResource(obj, gvk))
}

func (v *openAPISchemaValidator) validateList(object interface{}) []error {
	fields, ok := object.(map[string]interface{})
	if !ok || fields == nil {
		return []error{errors.New("invalid object to validate")}
	}

	allErrors := make([]error, 0)
	if _, ok := fields["items"].([]interface{}); !ok {
		return []error{errors.New("invalid object to validate")}
	}
	for _, item := range fields["items"].([]interface{}) {
		if gvk, errs := getObjectKind(item); errs != nil {
			allErrors = append(allErrors, errs...)
		} else {
			allErrors = append(allErrors, v.validateResource(item, gvk)...)
		}
	}
	return allErrors
}

func (v *openAPISchemaValidator) validateResource(obj interface{}, gvk schema.GroupVersionKind) []error {
	s := v.schema.LookupResource(gvk)
	if s == nil {
		// resource is not present, let's just skip validation.
		return nil
	}

	return validation.ValidateModel(obj, s, gvk.Kind)
}

func parse(data []byte) (interface{}, error) {
	var obj interface{}
	out, err := yaml.ToJSON(data)
	if err != nil {
		return nil, err
	}
	if err := kubeJson.Unmarshal(out, &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func getObjectKind(object interface{}) (schema.GroupVersionKind, []error) {
	var listErrors []error
	fields, ok := object.(map[string]interface{})
	if !ok || fields == nil {
		listErrors = append(listErrors, errors.New("invalid object to validate"))
		return schema.GroupVersionKind{}, listErrors
	}

	var group string
	var version string
	apiVersion := fields["apiVersion"]
	if apiVersion == nil {
		listErrors = append(listErrors, errors.New("apiVersion not set"))
	} else if _, ok := apiVersion.(string); !ok {
		listErrors = append(listErrors, errors.New("apiVersion isn't string type"))
	} else {
		gv, err := schema.ParseGroupVersion(apiVersion.(string))
		if err != nil {
			listErrors = append(listErrors, err)
		} else {
			group = gv.Group
			version = gv.Version
		}
	}
	kind := fields["kind"]
	if kind == nil {
		listErrors = append(listErrors, errors.New("kind not set"))
	} else if _, ok := kind.(string); !ok {
		listErrors = append(listErrors, errors.New("kind isn't string type"))
	}
	if listErrors != nil {
		return schema.GroupVersionKind{}, listErrors
	}

	return schema.GroupVersionKind{Group: group, Version: version, Kind: kind.(string)}, nil
}

// noDoubleKeyValidator is a schema that disallows double keys.
type noDoubleKeyValidator struct{}

// ValidateBytes validates bytes.
func (noDoubleKeyValidator) ValidateBytes(data []byte) error {
	var list []error
	if err := validateNoDuplicateKeys(data, "metadata", "labels"); err != nil {
		list = append(list, err)
	}
	if err := validateNoDuplicateKeys(data, "metadata", "annotations"); err != nil {
		list = append(list, err)
	}
	return kubeErrors.NewAggregate(list)
}

func validateNoDuplicateKeys(data []byte, path ...string) error {
	r := ejson.NewDecoder(bytes.NewReader(data))
	// This is Go being unfriendly. The 'path ...string' comes in as a
	// []string, and SeekTo takes ...interface{}, so we can't just pass
	// the path straight in, we have to copy it.  *sigh*
	ifacePath := make([]interface{}, 0)
	for ix := range path {
		ifacePath = append(ifacePath, path[ix])
	}
	found, err := r.SeekTo(ifacePath...)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	seen := map[string]bool{}
	for {
		tok, err := r.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case json.Delim:
			if t.String() == "}" {
				return nil
			}
		case ejson.KeyString:
			if seen[string(t)] {
				return fmt.Errorf("duplicate key: %s", string(t))
			}
			seen[string(t)] = true
		}
	}
}

// conjunctiveValidator encapsulates a schema list.
type conjunctiveValidator []resource.ContentValidator

// ValidateBytes validates bytes per a conjunctiveValidator.
func (c conjunctiveValidator) ValidateBytes(data []byte) error {
	var list []error
	schemas := []resource.ContentValidator(c)
	for ix := range schemas {
		if err := schemas[ix].ValidateBytes(data); err != nil {
			list = append(list, err)
		}
	}
	return kubeErrors.NewAggregate(list)
}

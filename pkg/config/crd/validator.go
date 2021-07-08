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

package crd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-multierror"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	structuraldefaulting "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/defaulting"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/kube-openapi/pkg/validation/validate"
	serviceapis "sigs.k8s.io/gateway-api/apis/v1alpha1"

	clientextensions "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	clientnetworkingalpha "istio.io/client-go/pkg/apis/networking/v1alpha3"
	clientnetworkingbeta "istio.io/client-go/pkg/apis/networking/v1beta1"
	clientsecurity "istio.io/client-go/pkg/apis/security/v1beta1"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/yml"
)

// Validator returns a new validator for custom resources
// Warning: this is meant for usage in tests only
type Validator struct {
	byGvk      map[schema.GroupVersionKind]*validate.SchemaValidator
	structural map[schema.GroupVersionKind]*structuralschema.Structural
	// If enabled, resources without a validator will be ignored. Otherwise, they will fail.
	SkipMissing bool
	Scheme      *runtime.Scheme
}

func (v *Validator) ValidateCustomResourceYAML(data string) error {
	decoder := serializer.NewCodecFactory(v.Scheme, serializer.EnableStrict).UniversalDeserializer()

	var errs *multierror.Error
	for _, item := range yml.SplitString(data) {
		obj, _, err := decoder.Decode([]byte(item), nil, nil)
		if err != nil {
			return err
		}
		errs = multierror.Append(errs, v.ValidateCustomResource(obj))
	}
	return errs.ErrorOrNil()
}

func (v *Validator) ValidateCustomResource(o runtime.Object) error {
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(o)
	if err != nil {
		return err
	}

	un := &unstructured.Unstructured{Object: content}
	vd, f := v.byGvk[un.GroupVersionKind()]
	if !f {
		if v.SkipMissing {
			return nil
		}
		return fmt.Errorf("failed to validate type %v: no validator found", un.GroupVersionKind())
	}
	// Fill in defaults
	structuraldefaulting.Default(un.Object, v.structural[un.GroupVersionKind()])
	if err := validation.ValidateCustomResource(nil, un.Object, vd).Filter(func(err error) bool {
		// It turns out to be extremely hard to 100% match the api-servers validation, in particular around
		// null vs unset objects. See https://github.com/kubernetes/kubernetes/issues/95407
		// To unblock, we will just filter out these errors.
		// One major issue with this approach is that it treats nested fields differently. For example,
		// it may be illegal to explicitly set a field to "", but legal if you don't set the field at all
		// by omitting the entire parent object.
		return strings.Contains(err.Error(), `must be of type object: "null"`) ||
			strings.Contains(err.Error(), `must be of type array: "null"`) ||
			strings.Contains(err.Error(), `must be of type string: "null"`)
	}).ToAggregate(); err != nil {
		return fmt.Errorf("%v/%v/%v: %v", un.GroupVersionKind().Kind, un.GetName(), un.GetNamespace(), err)
	}
	return nil
}

func NewValidatorFromFiles(files ...string) (*Validator, error) {
	crds := []apiextensions.CustomResourceDefinition{}
	for _, file := range files {
		data, err := os.Open(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read input yaml file: %v", err)
		}

		yamlDecoder := kubeyaml.NewYAMLOrJSONDecoder(data, 512*1024)
		for {
			un := &unstructured.Unstructured{}
			err = yamlDecoder.Decode(&un)
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			crd := apiextensions.CustomResourceDefinition{}
			switch un.GroupVersionKind() {
			case schema.GroupVersionKind{
				Group:   "apiextensions.k8s.io",
				Version: "v1",
				Kind:    "CustomResourceDefinition",
			}:
				crdv1 := apiextensionsv1.CustomResourceDefinition{}
				if err := runtime.DefaultUnstructuredConverter.
					FromUnstructured(un.UnstructuredContent(), &crdv1); err != nil {
					return nil, err
				}
				if err := apiextensionsv1.Convert_v1_CustomResourceDefinition_To_apiextensions_CustomResourceDefinition(&crdv1, &crd, nil); err != nil {
					return nil, err
				}
			case schema.GroupVersionKind{
				Group:   "apiextensions.k8s.io",
				Version: "v1beta1",
				Kind:    "CustomResourceDefinition",
			}:
				crdv1beta1 := apiextensionsv1beta1.CustomResourceDefinition{}
				if err := runtime.DefaultUnstructuredConverter.
					FromUnstructured(un.UnstructuredContent(), &crdv1beta1); err != nil {
					return nil, err
				}
				if err := apiextensionsv1beta1.Convert_v1beta1_CustomResourceDefinition_To_apiextensions_CustomResourceDefinition(&crdv1beta1, &crd, nil); err != nil {
					return nil, err
				}
			default:
				return nil, fmt.Errorf("unknown CRD type: %v", un.GroupVersionKind())
			}
			crds = append(crds, crd)
		}
	}
	return NewValidatorFromCRDs(crds...)
}

func NewValidatorFromCRDs(crds ...apiextensions.CustomResourceDefinition) (*Validator, error) {
	v := &Validator{
		byGvk:      map[schema.GroupVersionKind]*validate.SchemaValidator{},
		structural: map[schema.GroupVersionKind]*structuralschema.Structural{},
	}
	for _, crd := range crds {
		versions := crd.Spec.Versions
		if len(versions) == 0 {
			versions = []apiextensions.CustomResourceDefinitionVersion{{Name: crd.Spec.Version}} // nolint: staticcheck
		}
		for _, ver := range versions {
			gvk := schema.GroupVersionKind{
				Group:   crd.Spec.Group,
				Version: ver.Name,
				Kind:    crd.Spec.Names.Kind,
			}
			crdSchema := ver.Schema
			if crdSchema == nil {
				crdSchema = crd.Spec.Validation
			}
			if crdSchema == nil {
				return nil, fmt.Errorf("crd did not have validation defined")
			}

			schemaValidator, _, err := validation.NewSchemaValidator(crdSchema)
			if err != nil {
				return nil, err
			}
			structural, err := structuralschema.NewStructural(crdSchema.OpenAPIV3Schema)
			if err != nil {
				return nil, err
			}

			v.byGvk[gvk] = schemaValidator
			v.structural[gvk] = structural
		}
	}

	// Set up default scheme
	v.Scheme = runtime.NewScheme()
	if err := clientextensions.AddToScheme(v.Scheme); err != nil {
		return nil, err
	}
	if err := clientnetworkingalpha.AddToScheme(v.Scheme); err != nil {
		return nil, err
	}
	if err := clientnetworkingbeta.AddToScheme(v.Scheme); err != nil {
		return nil, err
	}
	if err := clientsecurity.AddToScheme(v.Scheme); err != nil {
		return nil, err
	}
	if err := scheme.AddToScheme(v.Scheme); err != nil {
		return nil, err
	}
	if err := serviceapis.AddToScheme(v.Scheme); err != nil {
		return nil, err
	}

	return v, nil
}

func NewIstioValidator(t test.Failer) *Validator {
	v, err := NewValidatorFromFiles(
		filepath.Join(env.IstioSrc, "tests/integration/pilot/testdata/service-apis-crd.yaml"),
		filepath.Join(env.IstioSrc, "manifests/charts/base/crds/crd-all.gen.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

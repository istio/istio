// Copyright 2018 Istio Authors.
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
	"errors"
	"fmt"
	"io"
	"os"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	mixercrd "istio.io/istio/mixer/pkg/config/crd"
	mixerstore "istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/runtime/config/constant"
	mixervalidate "istio.io/istio/mixer/pkg/validate"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

var (
	mixerAPIVersion = "config.istio.io/v1alpha2"

	errMissingFilename = errors.New(`error: you must specify resources by --filename.
Example resource specifications include:
   '-f rsrc.yaml'
   '--filename=rsrc.json'`)
	errKindNotSupported = errors.New("kind is not supported")

	validFields = map[string]struct{}{
		"apiVersion": {},
		"kind":       {},
		"metadata":   {},
		"spec":       {},
		"status":     {},
	}

	validMixerKinds = map[string]struct{}{
		constant.RulesKind:             {},
		constant.AdapterKind:           {},
		constant.TemplateKind:          {},
		constant.HandlerKind:           {},
		constant.InstanceKind:          {},
		constant.AttributeManifestKind: {},
	}
)

type validator struct {
	mixerValidator mixerstore.BackendValidator
}

func checkFields(un *unstructured.Unstructured) error {
	var errs error
	for key := range un.Object {
		if _, ok := validFields[key]; !ok {
			errs = multierror.Append(errs, fmt.Errorf("unknown field %q", key))
		}
	}
	return errs
}

func (v *validator) validateResource(un *unstructured.Unstructured) error {
	schema, exists := model.IstioConfigTypes.GetByType(crd.CamelCaseToKebabCase(un.GetKind()))
	if exists {
		obj, err := crd.ConvertObjectFromUnstructured(schema, un, "")
		if err != nil {
			return fmt.Errorf("cannot parse proto message: %v", err)
		}
		if err = checkFields(un); err != nil {
			return err
		}
		return schema.Validate(obj.Name, obj.Namespace, obj.Spec)
	}

	if v.mixerValidator != nil && un.GetAPIVersion() == mixerAPIVersion {
		if !v.mixerValidator.SupportsKind(un.GetKind()) {
			return errKindNotSupported
		}
		if err := checkFields(un); err != nil {
			return err
		}
		if _, ok := validMixerKinds[un.GetKind()]; !ok {
			log.Warnf("deprecated Mixer kind %q, please use %q or %q instead", un.GetKind(),
				constant.HandlerKind, constant.InstanceKind)
		}

		return v.mixerValidator.Validate(&mixerstore.BackendEvent{
			Type: mixerstore.Update,
			Key: mixerstore.Key{
				Name:      un.GetName(),
				Namespace: un.GetNamespace(),
				Kind:      un.GetKind(),
			},
			Value: mixercrd.ToBackEndResource(un),
		})
	}

	return nil
}

func (v *validator) validateFile(reader io.Reader) error {
	decoder := yaml.NewDecoder(reader)
	var errs error
	for {
		// YAML allows non-string keys and the produces generic keys for nested fields
		raw := make(map[interface{}]interface{})
		err := decoder.Decode(&raw)
		if err == io.EOF {
			return errs
		}
		if err != nil {
			errs = multierror.Append(errs, err)
			return errs
		}
		if len(raw) == 0 {
			continue
		}
		out := transformInterfaceMap(raw)
		un := unstructured.Unstructured{Object: out}
		err = v.validateResource(&un)
		if err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("%s/%s/%s:",
				un.GetKind(), un.GetNamespace(), un.GetName())))
		}
	}
}

func validateFiles(filenames []string, referential bool) error {
	if len(filenames) == 0 {
		return errMissingFilename
	}

	v := &validator{
		mixerValidator: mixervalidate.NewDefaultValidator(referential),
	}

	var errs error
	for _, filename := range filenames {
		reader, err := os.Open(filename)
		if err != nil {
			errs = multierror.Append(errs, fmt.Errorf("cannot read file %q: %v", filename, err))
			continue
		}
		err = v.validateFile(reader)
		if err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	return errs
}

// NewValidateCommand creates a new command for validating Istio k8s resources.
func NewValidateCommand() *cobra.Command {
	var filenames []string
	var referential bool

	c := &cobra.Command{
		Use:     "validate -f FILENAME [options]",
		Short:   "Validate Istio policy and rules",
		Example: `istioctl validate -f bookinfo-gateway.yaml`,
		Args:    cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return validateFiles(filenames, referential)
		},
	}

	flags := c.PersistentFlags()
	flags.StringSliceVarP(&filenames, "filename", "f", nil, "Names of files to validate")
	flags.BoolVarP(&referential, "referential", "x", true, "Enable structural validation for policy and telemetry")

	return c
}

func transformInterfaceArray(in []interface{}) []interface{} {
	out := make([]interface{}, len(in))
	for i, v := range in {
		out[i] = transformMapValue(v)
	}
	return out
}

func transformInterfaceMap(in map[interface{}]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[fmt.Sprintf("%v", k)] = transformMapValue(v)
	}
	return out
}

func transformMapValue(in interface{}) interface{} {
	switch v := in.(type) {
	case []interface{}:
		return transformInterfaceArray(v)
	case map[interface{}]interface{}:
		return transformInterfaceMap(v)
	default:
		return v
	}
}

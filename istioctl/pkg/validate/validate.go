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
	yaml "gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
)

/*

TODO(https://github.com/istio/istio/issues/4887)

Reusing the existing mixer validation code pulls in all of the mixer
adapter packages into istioctl. Not only is this not ideal (see
issue), but it also breaks the istioctl build on windows as some mixer
adapters use linux specific packges (e.g. syslog).

func createMixerValidator() store.BackendValidator {
	info := generatedTmplRepo.SupportedTmplInfo
	templates := make(map[string]*template.Info, len(info))
	for k := range info {
		t := info[k]
		templates[k] = &t
	}
	adapters := config.AdapterInfoMap(adapter.Inventory(), template.NewRepository(info).SupportsTemplate)
	return store.NewValidator(nil, runtimeConfig.KindMap(adapters, templates))
}

var mixerValidator = createMixerValidator()

type validateArgs struct {
	filenames []string
	// TODO validateObjectStream namespace/object?
}

func (args validateArgs) validate() error {
	var errs *multierror.Error
	if len(args.filenames) == 0 {
		errs = multierror.Append(errs, errors.New("no filenames set (see --filename/-f)"))
	}
	return errs.ErrorOrNil()
}
*/

func validateResource(un *unstructured.Unstructured) error {
	schema, exists := model.IstioConfigTypes.GetByType(crd.CamelCaseToKebabCase(un.GetKind()))
	if exists {
		obj, err := crd.ConvertObjectFromUnstructured(schema, un, "")
		if err != nil {
			return fmt.Errorf("cannot parse proto message: %v", err)
		}
		return schema.Validate(obj.Name, obj.Namespace, obj.Spec)
	}
	return fmt.Errorf("%s.%s validation is not supported", un.GetKind(), un.GetAPIVersion())
	/*
		TODO(https://github.com/istio/istio/issues/4887)

		ev := &store.BackendEvent{
			Key: store.Key{
				Name:      un.GetName(),
				Namespace: un.GetNamespace(),
				Kind:      un.GetKind(),
			},
			Value: mixerCrd.ToBackEndResource(un),
		}
		return mixerValidator.Validate(ev)
	*/
}

func validateFile(reader io.Reader) error {
	decoder := yaml.NewDecoder(reader)
	var errs error
	for {
		out := make(map[string]interface{})
		err := decoder.Decode(&out)
		if err == io.EOF {
			return errs
		}
		if err != nil {
			errs = multierror.Append(errs, err)
			return errs
		}
		if len(out) == 0 {
			continue
		}
		un := unstructured.Unstructured{Object: out}
		err = validateResource(&un)
		if err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("%s/%s/%s",
				un.GetKind(), un.GetNamespace(), un.GetName())))
		}
	}
}

func validateFiles(filenames []string) error {
	if len(filenames) == 0 {
		return errors.New(`error: you must specify resources by --filename.
Example resource specifications include:
   '-f rsrc.yaml'
   '--filename=rsrc.json'`)
	}

	var errs error
	for _, filename := range filenames {
		reader, err := os.Open(filename)
		if err != nil {
			errs = multierror.Append(errs, fmt.Errorf("cannot read file %q: %v", filename, err))
			continue
		}
		err = validateFile(reader)
		if err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	return errs
}

// NewValidateCommand creates a new command for validating Istio k8s resources.
func NewValidateCommand() *cobra.Command {
	var filenames []string

	c := &cobra.Command{
		Use:     "validate -f FILENAME [options]",
		Short:   "Validate Istio policy and rules",
		Example: `istioctl validate -f bookinfo-gateway.yaml`,
		Args:    cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return validateFiles(filenames)
		},
	}

	flags := c.PersistentFlags()
	flags.StringSliceVarP(&filenames, "filename", "f", nil, "Names of files to validate")

	return c
}

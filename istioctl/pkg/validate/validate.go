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

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime" // TODO use k8s.io/cli-runtime when we switch to v1.12 k8s dependency
	// k8s.io/cli-runtime was created for k8s v.12. Prior to that release,
	// the genericclioptions packages are organized under kubectl.
	"k8s.io/kubernetes/pkg/kubectl/genericclioptions"
	"k8s.io/kubernetes/pkg/kubectl/genericclioptions/resource"

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
	return fmt.Errorf("mixer API validation is not supported")
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

var errMissingResource = errors.New(`error: you must specify resources by --filename.
Example resource specifications include:
   '-f rsrc.yaml'
   '--filename=rsrc.json'`)

func validateObjects(restClientGetter resource.RESTClientGetter, options resource.FilenameOptions, writer io.Writer) error {
	// resource.Builder{} validates most of the CLI flags consistent
	// with kubectl which is good. Unforatunly, it also assumes
	// resources can be specified as '<resource> <name>' which is
	// bad. We don't don't for file-based configuration validation. If
	// a filename is missing, resource.Builder{} prints a warning
	// referencing '<resource> <name>' which would be confusing to the
	// user. Avoid this confusion by checking for missing filenames
	// are ourselves for invoking the builder.
	if len(options.Filenames) == 0 {
		return errMissingResource
	}

	r := resource.NewBuilder(restClientGetter).
		Unstructured().
		FilenameParam(false, &options).
		Flatten().
		Local().
		Do()
	if err := r.Err(); err != nil {
		return err
	}

	return r.Visit(func(info *resource.Info, err error) error {
		content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(info.Object)
		if err != nil {
			return err
		}

		un := &unstructured.Unstructured{Object: content}
		if err := validateResource(un); err != nil {
			return fmt.Errorf("error validating resource (%v Name=%v Namespace=%v): %v",
				un.GetObjectKind().GroupVersionKind(), un.GetName(), un.GetNamespace(), err)
		}
		return nil
	})
}

func strPtr(val string) *string {
	return &val
}

func boolPtr(val bool) *bool {
	return &val
}

// NewValidateCommand creates a new command for validating Istio k8s resources.
func NewValidateCommand() *cobra.Command {
	var (
		kubeConfigFlags = &genericclioptions.ConfigFlags{
			Context:    strPtr(""),
			Namespace:  strPtr(""),
			KubeConfig: strPtr(""),
		}

		filenames     = []string{}
		fileNameFlags = &genericclioptions.FileNameFlags{
			Filenames: &filenames,
			Recursive: boolPtr(true),
		}
	)

	c := &cobra.Command{
		Use:     "validate -f FILENAME [options]",
		Short:   "Validate Istio policy and rules",
		Example: `istioctl validate -f bookinfo-gateway.yaml`,
		Args:    cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return validateObjects(kubeConfigFlags, fileNameFlags.ToOptions(), c.OutOrStderr())
		},
	}

	flags := c.PersistentFlags()
	kubeConfigFlags.AddFlags(flags)
	fileNameFlags.AddFlags(flags)

	return c
}

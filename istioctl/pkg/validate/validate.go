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

	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/kubectl/genericclioptions"
	"k8s.io/kubernetes/pkg/kubectl/genericclioptions/resource"

	mixercrd "istio.io/istio/mixer/pkg/config/crd"
	mixerstore "istio.io/istio/mixer/pkg/config/store"
	mixervalidate "istio.io/istio/mixer/pkg/validate"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
)

// TODO use k8s.io/cli-runtime when we switch to v1.12 k8s dependency
// k8s.io/cli-runtime was created for k8s v.12. Prior to that release,
// the genericclioptions packages are organized under kubectl.

var (
	// Expect YAML to only include these top-level fields
	validFields = map[string]bool{
		"apiVersion": true,
		"kind":       true,
		"metadata":   true,
		"spec":       true,
		"status":     true,
	}

	mixerAPIVersion = "config.istio.io/v1alpha2"
)

type validator struct {
	errs error

	mixerValidator mixerstore.BackendValidator
}

func (v *validator) validateResource(un *unstructured.Unstructured) error {
	schema, exists := model.IstioConfigTypes.GetByType(crd.CamelCaseToKebabCase(un.GetKind()))
	if exists {
		obj, err := crd.ConvertObjectFromUnstructured(schema, un, "")
		if err != nil {
			return fmt.Errorf("cannot parse proto message: %v", err)
		}
		return schema.Validate(obj.Name, obj.Namespace, obj.Spec)
	}

	if v.mixerValidator != nil && un.GetAPIVersion() == mixerAPIVersion {
		if !v.mixerValidator.SupportsKind(un.GetKind()) {
			return fmt.Errorf("%s not implemented", un.GetKind())
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
	return fmt.Errorf("%s.%s validation is not supported", un.GetKind(), un.GetAPIVersion())
}

var errMissingResource = errors.New(`error: you must specify resources by --filename.
Example resource specifications include:
   '-f rsrc.yaml'
   '--filename=rsrc.json'`)

func validateObjects(restClientGetter resource.RESTClientGetter, options resource.FilenameOptions, writer io.Writer) error {
	// resource.Builder{} validates most of the CLI flags consistent
	// with kubectl which is good. Unfortunately, it also assumes
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

	v := &validator{
		mixerValidator: mixervalidate.NewDefaultValidator(true),
	}

	_ = r.Visit(func(info *resource.Info, err error) error {
		if err != nil {
			return err
		}
		content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(info.Object)
		if err != nil {
			v.errs = multierror.Append(v.errs, err)
			return nil
		}

		un := &unstructured.Unstructured{Object: content}
		for key := range content {
			if _, ok := validFields[key]; !ok {
				v.errs = multierror.Append(v.errs, fmt.Errorf("%s: Unknown field %q on %s resource %s namespace %q",
					info.Source, key, un.GetObjectKind().GroupVersionKind(), un.GetName(), un.GetNamespace()))
			}
		}

		if err := v.validateResource(un); err != nil {
			v.errs = multierror.Append(v.errs, fmt.Errorf("error validating resource (%v Name=%v Namespace=%v): %v",
				un.GetObjectKind().GroupVersionKind(), un.GetName(), un.GetNamespace(), err))
		}
		return nil
	})
	if v.errs != nil {
		return v.errs
	}
	for _, fname := range options.Filenames {
		fmt.Fprintf(writer, "%q is valid\n", fname)
	}
	return nil
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

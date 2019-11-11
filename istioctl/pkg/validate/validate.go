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
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"

	mixercrd "istio.io/istio/mixer/pkg/config/crd"
	mixerstore "istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/runtime/config/constant"
	mixervalidate "istio.io/istio/mixer/pkg/validate"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schemas"

	"istio.io/pkg/log"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
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
	istioDeploymentLabel = []string{
		"app",
		"version",
	}
	serviceProtocolUDP = "UDP"
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

func (v *validator) validateResource(istioNamespace string, un *unstructured.Unstructured) error {
	schema, exists := schemas.Istio.GetByType(crd.CamelCaseToKebabCase(un.GetKind()))
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
	var errs error
	if un.IsList() {
		_ = un.EachListItem(func(item runtime.Object) error { // nolint: errcheck
			castItem := item.(*unstructured.Unstructured)
			if castItem.GetKind() == "Service" {
				err := v.validateServicePortPrefix(istioNamespace, castItem)
				if err != nil {
					errs = multierror.Append(errs, err)
				}
			}
			if castItem.GetKind() == "Deployment" {
				v.validateDeploymentLabel(istioNamespace, castItem)
			}
			return nil
		})
	}

	if errs != nil {
		return errs
	}
	if un.GetKind() == "Service" {
		return v.validateServicePortPrefix(istioNamespace, un)
	}

	if un.GetKind() == "Deployment" {
		v.validateDeploymentLabel(istioNamespace, un)
		return nil
	}
	return nil
}

func (v *validator) validateServicePortPrefix(istioNamespace string, un *unstructured.Unstructured) error {
	var errs error
	if un.GetNamespace() == handleNamespace(istioNamespace) {
		return nil
	}
	spec := un.Object["spec"].(map[string]interface{})
	if _, ok := spec["ports"]; ok {
		ports := spec["ports"].([]interface{})
		for _, port := range ports {
			p := port.(map[string]interface{})
			if p["protocol"] != nil && strings.EqualFold(p["protocol"].(string), serviceProtocolUDP) {
				continue
			}
			if p["name"] == nil {
				errs = multierror.Append(errs, fmt.Errorf("service %q has an unnamed port. This is not recommended,"+
					" see https://istio.io/docs/setup/kubernetes/prepare/requirements/", fmt.Sprintf("%s/%s/:",
					un.GetName(), un.GetNamespace())))
				continue
			}
			if servicePortPrefixed(p["name"].(string)) {
				errs = multierror.Append(errs, fmt.Errorf("service %q port %q does not follow the Istio naming convention."+
					" See https://istio.io/docs/setup/kubernetes/prepare/requirements/", fmt.Sprintf("%s/%s/:",
					un.GetName(), un.GetNamespace()), p["name"].(string)))
			}
		}
	}
	if errs != nil {
		return errs
	}
	return nil
}

func (v *validator) validateDeploymentLabel(istioNamespace string, un *unstructured.Unstructured) {
	if un.GetNamespace() == handleNamespace(istioNamespace) {
		return
	}
	labels := un.GetLabels()
	for _, l := range istioDeploymentLabel {
		if _, ok := labels[l]; !ok {
			log.Warnf("deployment %q may not provide Istio metrics and telemetry without label %q."+
				" See https://istio.io/docs/setup/kubernetes/prepare/requirements/ \n", fmt.Sprintf("%s/%s:",
				un.GetName(), un.GetNamespace()), l)
		}
	}
}

func (v *validator) validateFile(istioNamespace *string, reader io.Reader) error {
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
		err = v.validateResource(*istioNamespace, &un)
		if err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("%s/%s/%s:",
				un.GetKind(), un.GetNamespace(), un.GetName())))
		}
	}
}

func validateFiles(istioNamespace *string, filenames []string, referential bool, writer io.Writer) error {
	if len(filenames) == 0 {
		return errMissingFilename
	}

	v := &validator{
		mixerValidator: mixervalidate.NewDefaultValidator(referential),
	}

	var errs, err error
	var reader io.Reader
	for _, filename := range filenames {
		if filename == "-" {
			reader = os.Stdin
		} else {
			reader, err = os.Open(filename)
		}
		if err != nil {
			errs = multierror.Append(errs, fmt.Errorf("cannot read file %q: %v", filename, err))
			continue
		}
		err = v.validateFile(istioNamespace, reader)
		if err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	if errs != nil {
		return errs
	}
	for _, fname := range filenames {
		if fname == "-" {
			_, _ = fmt.Fprintf(writer, "validation succeed\n")
			break
		} else {
			_, _ = fmt.Fprintf(writer, "%q is valid\n", fname)
		}
	}

	return nil
}

// NewValidateCommand creates a new command for validating Istio k8s resources.
func NewValidateCommand(istioNamespace *string) *cobra.Command {
	var filenames []string
	var referential bool

	c := &cobra.Command{
		Use:   "validate -f FILENAME [options]",
		Short: "Validate Istio policy and rules",
		Example: `
		# Validate bookinfo-gateway.yaml
		istioctl validate -f bookinfo-gateway.yaml
		
		# Validate current deployments under 'default' namespace within the cluster
		kubectl get deployments -o yaml |istioctl validate -f -

		# Validate current services under 'default' namespace within the cluster
		kubectl get services -o yaml |istioctl validate -f -

		# Also see the related experimental command 'istioctl x analyze'
		istioctl x analyze samples/bookinfo/networking/bookinfo-gateway.yaml
`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return validateFiles(istioNamespace, filenames, referential, c.OutOrStderr())
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

func servicePortPrefixed(n string) bool {
	i := strings.IndexByte(n, '-')
	if i >= 0 {
		n = n[:i]
	}
	p := protocol.Parse(n)
	return p == protocol.Unsupported
}
func handleNamespace(istioNamespace string) string {
	if istioNamespace == "" {
		istioNamespace = controller.IstioNamespace
	}
	return istioNamespace
}

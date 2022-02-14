// Copyright Istio Authors.
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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	operator_istio "istio.io/istio/operator/pkg/apis/istio"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/util"
	operator_validate "istio.io/istio/operator/pkg/validate"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/validation"
	"istio.io/istio/pkg/url"
)

var (
	errMissingFilename = errors.New(`error: you must specify resources by --filename.
Example resource specifications include:
   '-f rsrc.yaml'
   '--filename=rsrc.json'`)

	validFields = map[string]struct{}{
		"apiVersion": {},
		"kind":       {},
		"metadata":   {},
		"spec":       {},
		"status":     {},
	}

	istioDeploymentLabel = []string{
		"app",
		"version",
	}
	serviceProtocolUDP = "UDP"
)

type validator struct{}

func checkFields(un *unstructured.Unstructured) error {
	var errs error
	for key := range un.Object {
		if _, ok := validFields[key]; !ok {
			errs = multierror.Append(errs, fmt.Errorf("unknown field %q", key))
		}
	}
	return errs
}

func (v *validator) validateResource(istioNamespace, defaultNamespace string, un *unstructured.Unstructured, writer io.Writer) (validation.Warning, error) {
	gvk := config.GroupVersionKind{
		Group:   un.GroupVersionKind().Group,
		Version: un.GroupVersionKind().Version,
		Kind:    un.GroupVersionKind().Kind,
	}
	// TODO(jasonwzm) remove this when multi-version is supported. v1beta1 shares the same
	// schema as v1lalpha3. Fake conversion and validate against v1alpha3.
	if gvk.Group == name.NetworkingAPIGroupName && gvk.Version == "v1beta1" {
		gvk.Version = "v1alpha3"
	}
	schema, exists := collections.Pilot.FindByGroupVersionKind(gvk)
	if exists {
		obj, err := convertObjectFromUnstructured(schema, un, "")
		if err != nil {
			return nil, fmt.Errorf("cannot parse proto message: %v", err)
		}
		if err = checkFields(un); err != nil {
			return nil, err
		}

		// If object to validate has no namespace, set it (the validity of a CR
		// may depend on its namespace; for example a VirtualService with exportTo=".")
		if obj.Namespace == "" {
			// If the user didn't specify --namespace, and is validating a CR with no namespace, use "default"
			if defaultNamespace == "" {
				defaultNamespace = metav1.NamespaceDefault
			}
			obj.Namespace = defaultNamespace
		}

		warnings, err := schema.Resource().ValidateConfig(*obj)
		return warnings, err
	}

	var errs error
	if un.IsList() {
		_ = un.EachListItem(func(item runtime.Object) error {
			castItem := item.(*unstructured.Unstructured)
			if castItem.GetKind() == name.ServiceStr {
				err := v.validateServicePortPrefix(istioNamespace, castItem)
				if err != nil {
					errs = multierror.Append(errs, err)
				}
			}
			if castItem.GetKind() == name.DeploymentStr {
				err := v.validateDeploymentLabel(istioNamespace, castItem, writer)
				if err != nil {
					errs = multierror.Append(errs, err)
				}
			}
			return nil
		})
	}

	if errs != nil {
		return nil, errs
	}
	if un.GetKind() == name.ServiceStr {
		return nil, v.validateServicePortPrefix(istioNamespace, un)
	}

	if un.GetKind() == name.DeploymentStr {
		if err := v.validateDeploymentLabel(istioNamespace, un, writer); err != nil {
			return nil, err
		}
		return nil, nil
	}

	if un.GetAPIVersion() == "install.istio.io/v1alpha1" {
		if un.GetKind() == "IstioOperator" {
			if err := checkFields(un); err != nil {
				return nil, err
			}
			// IstioOperator isn't part of pkg/config/schema/collections,
			// usual conversion not available.  Convert unstructured to string
			// and ask operator code to check.
			un.SetCreationTimestamp(metav1.Time{}) // UnmarshalIstioOperator chokes on these
			by := util.ToYAML(un)
			iop, err := operator_istio.UnmarshalIstioOperator(by, false)
			if err != nil {
				return nil, err
			}
			return nil, operator_validate.CheckIstioOperator(iop, true)
		}
	}

	// Didn't really validate.  This is OK, as we often get non-Istio Kubernetes YAML
	// we can't complain about.

	return nil, nil
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
					" See "+url.DeploymentRequirements, fmt.Sprintf("%s/%s/:", un.GetName(), un.GetNamespace())))
				continue
			}
			if servicePortPrefixed(p["name"].(string)) {
				errs = multierror.Append(errs, fmt.Errorf("service %q port %q does not follow the Istio naming convention."+
					" See "+url.DeploymentRequirements, fmt.Sprintf("%s/%s/:", un.GetName(), un.GetNamespace()), p["name"].(string)))
			}
		}
	}
	if errs != nil {
		return errs
	}
	return nil
}

func (v *validator) validateDeploymentLabel(istioNamespace string, un *unstructured.Unstructured, writer io.Writer) error {
	if un.GetNamespace() == handleNamespace(istioNamespace) {
		return nil
	}
	labels, err := GetTemplateLabels(un)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("See %s\n", url.DeploymentRequirements)
	for _, l := range istioDeploymentLabel {
		if _, ok := labels[l]; !ok {
			fmt.Fprintf(writer, "deployment %q may not provide Istio metrics and telemetry without label %q. "+url,
				fmt.Sprintf("%s/%s:", un.GetName(), un.GetNamespace()), l)
		}
	}
	return nil
}

// GetTemplateLabels returns spec.template.metadata.labels from Deployment
func GetTemplateLabels(u *unstructured.Unstructured) (map[string]string, error) {
	if spec, ok := u.Object["spec"].(map[string]interface{}); ok {
		if template, ok := spec["template"].(map[string]interface{}); ok {
			m, _, err := unstructured.NestedStringMap(template, "metadata", "labels")
			if err != nil {
				return nil, err
			}
			return m, nil
		}
	}
	return nil, nil
}

func (v *validator) validateFile(istioNamespace *string, defaultNamespace string, reader io.Reader, writer io.Writer) (validation.Warning, error) {
	decoder := yaml.NewDecoder(reader)
	decoder.SetStrict(true)
	var errs error
	var warnings validation.Warning
	for {
		// YAML allows non-string keys and the produces generic keys for nested fields
		raw := make(map[interface{}]interface{})
		err := decoder.Decode(&raw)
		if err == io.EOF {
			return warnings, errs
		}
		if err != nil {
			errs = multierror.Append(errs, err)
			return warnings, errs
		}
		if len(raw) == 0 {
			continue
		}
		out := transformInterfaceMap(raw)
		un := unstructured.Unstructured{Object: out}
		warning, err := v.validateResource(*istioNamespace, defaultNamespace, &un, writer)
		if err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("%s/%s/%s:",
				un.GetKind(), un.GetNamespace(), un.GetName())))
		}
		if warning != nil {
			warnings = multierror.Append(warnings, multierror.Prefix(warning, fmt.Sprintf("%s/%s/%s:",
				un.GetKind(), un.GetNamespace(), un.GetName())))
		}
	}
}

func validateFiles(istioNamespace *string, defaultNamespace string, filenames []string, writer io.Writer) error {
	if len(filenames) == 0 {
		return errMissingFilename
	}

	v := &validator{}

	var errs, err error
	var reader io.ReadCloser
	warningsByFilename := map[string]validation.Warning{}
	for _, filename := range filenames {
		if filename == "-" {
			reader = io.NopCloser(os.Stdin)
		} else {
			reader, err = os.Open(filename)
		}
		if err != nil {
			errs = multierror.Append(errs, fmt.Errorf("cannot read file %q: %v", filename, err))
			continue
		}
		warning, err := v.validateFile(istioNamespace, defaultNamespace, reader, writer)
		if err != nil {
			errs = multierror.Append(errs, err)
		}
		reader.Close()
		warningsByFilename[filename] = warning
	}

	if errs != nil {
		// Display warnings we encountered as well
		for _, fname := range filenames {
			if w := warningsByFilename[fname]; w != nil {
				if fname == "-" {
					_, _ = fmt.Fprint(writer, warningToString(w))
					break
				} else {
					_, _ = fmt.Fprintf(writer, "%q has warnings: %v\n", fname, warningToString(w))
				}
			}
		}
		return errs
	}
	for _, fname := range filenames {
		if fname == "-" {
			if w := warningsByFilename[fname]; w != nil {
				_, _ = fmt.Fprint(writer, warningToString(w))
			} else {
				_, _ = fmt.Fprintf(writer, "validation succeed\n")
			}
			break
		} else {
			if w := warningsByFilename[fname]; w != nil {
				_, _ = fmt.Fprintf(writer, "%q has warnings: %v\n", fname, warningToString(w))
			} else {
				_, _ = fmt.Fprintf(writer, "%q is valid\n", fname)
			}
		}
	}

	return nil
}

// NewValidateCommand creates a new command for validating Istio k8s resources.
func NewValidateCommand(istioNamespace *string, defaultNamespace *string) *cobra.Command {
	var filenames []string
	var referential bool

	c := &cobra.Command{
		Use:     "validate -f FILENAME [options]",
		Aliases: []string{"v"},
		Short:   "Validate Istio policy and rules files",
		Example: `  # Validate bookinfo-gateway.yaml
  istioctl validate -f samples/bookinfo/networking/bookinfo-gateway.yaml

  # Validate bookinfo-gateway.yaml with shorthand syntax
  istioctl v -f samples/bookinfo/networking/bookinfo-gateway.yaml

  # Validate current deployments under 'default' namespace within the cluster
  kubectl get deployments -o yaml | istioctl validate -f -

  # Validate current services under 'default' namespace within the cluster
  kubectl get services -o yaml | istioctl validate -f -

  # Also see the related command 'istioctl analyze'
  istioctl analyze samples/bookinfo/networking/bookinfo-gateway.yaml
`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return validateFiles(istioNamespace, *defaultNamespace, filenames, c.OutOrStderr())
		},
	}

	flags := c.PersistentFlags()
	flags.StringSliceVarP(&filenames, "filename", "f", nil, "Names of files to validate")
	flags.BoolVarP(&referential, "referential", "x", true, "Enable structural validation for policy and telemetry")

	return c
}

func warningToString(w validation.Warning) string {
	we, ok := w.(*multierror.Error)
	if ok {
		we.ErrorFormat = func(i []error) string {
			points := make([]string, len(i))
			for i, err := range i {
				points[i] = fmt.Sprintf("* %s", err)
			}

			return fmt.Sprintf(
				"\n\t%s\n",
				strings.Join(points, "\n\t"))
		}
	}
	return w.Error()
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
		istioNamespace = constants.IstioSystemNamespace
	}
	return istioNamespace
}

// TODO(nmittler): Remove this once Pilot migrates to galley schema.
func convertObjectFromUnstructured(schema collection.Schema, un *unstructured.Unstructured, domain string) (*config.Config, error) {
	data, err := fromSchemaAndJSONMap(schema, un.Object["spec"])
	if err != nil {
		return nil, err
	}

	return &config.Config{
		Meta: config.Meta{
			GroupVersionKind:  schema.Resource().GroupVersionKind(),
			Name:              un.GetName(),
			Namespace:         un.GetNamespace(),
			Domain:            domain,
			Labels:            un.GetLabels(),
			Annotations:       un.GetAnnotations(),
			ResourceVersion:   un.GetResourceVersion(),
			CreationTimestamp: un.GetCreationTimestamp().Time,
		},
		Spec: data,
	}, nil
}

// TODO(nmittler): Remove this once Pilot migrates to galley schema.
func fromSchemaAndJSONMap(schema collection.Schema, data interface{}) (config.Spec, error) {
	// Marshal to json bytes
	str, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	out, err := schema.Resource().NewInstance()
	if err != nil {
		return nil, err
	}
	if err = config.ApplyJSONStrict(out, string(str)); err != nil {
		return nil, err
	}
	return out, nil
}

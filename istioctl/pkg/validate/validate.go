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
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"istio.io/istio/istioctl/pkg/cli"
	operator "istio.io/istio/operator/pkg/apis"
	operatorvalidate "istio.io/istio/operator/pkg/apis/validation"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/config/validation"
	"istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
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

	serviceProtocolUDP = "UDP"

	fileExtensions = []string{".json", ".yaml", ".yml"}
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
	g := config.GroupVersionKind{
		Group:   un.GroupVersionKind().Group,
		Version: un.GroupVersionKind().Version,
		Kind:    un.GroupVersionKind().Kind,
	}
	schema, exists := collections.Pilot.FindByGroupVersionAliasesKind(g)
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

		warnings, err := schema.ValidateConfig(*obj)
		return warnings, err
	}

	var errs error
	if un.IsList() {
		_ = un.EachListItem(func(item runtime.Object) error {
			castItem := item.(*unstructured.Unstructured)
			if castItem.GetKind() == gvk.Service.Kind {
				err := v.validateServicePortPrefix(istioNamespace, castItem)
				if err != nil {
					errs = multierror.Append(errs, err)
				}
			}
			if castItem.GetKind() == gvk.Deployment.Kind {
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
	if un.GetKind() == gvk.Service.Kind {
		return nil, v.validateServicePortPrefix(istioNamespace, un)
	}

	if un.GetKind() == gvk.Deployment.Kind {
		if err := v.validateDeploymentLabel(istioNamespace, un, writer); err != nil {
			return nil, err
		}
		return nil, nil
	}

	if un.GetAPIVersion() == operator.IstioOperatorGVK.GroupVersion().String() {
		if un.GetKind() == operator.IstioOperatorGVK.Kind {
			if err := checkFields(un); err != nil {
				return nil, err
			}
			warnings, err := operatorvalidate.ParseAndValidateIstioOperator(un.Object, nil)
			if err != nil {
				return nil, err
			}
			if len(warnings) > 0 {
				return validation.Warning(warnings.ToError()), nil
			}
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
	spec := un.Object["spec"].(map[string]any)
	if _, ok := spec["ports"]; ok {
		ports := spec["ports"].([]any)
		for _, port := range ports {
			p := port.(map[string]any)
			if p["protocol"] != nil && strings.EqualFold(p["protocol"].(string), serviceProtocolUDP) {
				continue
			}
			if ap := p["appProtocol"]; ap != nil {
				if protocol.Parse(ap.(string)).IsUnsupported() {
					errs = multierror.Append(errs, fmt.Errorf("service %q doesn't follow Istio protocol selection. "+
						"This is not recommended, See "+url.ProtocolSelection, fmt.Sprintf("%s/%s/:", un.GetName(), un.GetNamespace())))
				}
			} else {
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
	objLabels, err := GetTemplateLabels(un)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("See %s\n", url.DeploymentRequirements)
	if !labels.HasCanonicalServiceName(objLabels) || !labels.HasCanonicalServiceRevision(objLabels) {
		fmt.Fprintf(writer, "deployment %q may not provide Istio metrics and telemetry labels: %q. "+url,
			fmt.Sprintf("%s/%s:", un.GetName(), un.GetNamespace()), objLabels)
	}
	return nil
}

// GetTemplateLabels returns spec.template.metadata.labels from Deployment
func GetTemplateLabels(u *unstructured.Unstructured) (map[string]string, error) {
	if spec, ok := u.Object["spec"].(map[string]any); ok {
		if template, ok := spec["template"].(map[string]any); ok {
			m, _, err := unstructured.NestedStringMap(template, "metadata", "labels")
			if err != nil {
				return nil, err
			}
			return m, nil
		}
	}
	return nil, nil
}

func (v *validator) validateFile(path string, istioNamespace *string, defaultNamespace string, reader io.Reader, writer io.Writer,
) (validation.Warning, error) {
	yamlReader := kubeyaml.NewYAMLReader(bufio.NewReader(reader))
	var errs error
	var warnings validation.Warning
	for {
		doc, err := yamlReader.Read()
		if err == io.EOF {
			return warnings, errs
		}
		if err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("failed to decode file %s: ", path)))
			return warnings, errs
		}
		if len(doc) == 0 {
			continue
		}
		out := map[string]any{}
		if err := yaml.UnmarshalStrict(doc, &out); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("failed to decode file %s: ", path)))
			return warnings, errs
		}
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

func isFileFormatValid(file string) bool {
	ext := filepath.Ext(file)
	return slices.Contains(fileExtensions, ext)
}

func validateFiles(istioNamespace *string, defaultNamespace string, filenames []string, writer io.Writer) error {
	if len(filenames) == 0 {
		return errMissingFilename
	}

	v := &validator{}

	var errs error
	var reader io.ReadCloser
	warningsByFilename := map[string]validation.Warning{}

	processFile := func(path string) {
		var err error
		if path == "-" {
			reader = io.NopCloser(os.Stdin)
		} else {
			reader, err = os.Open(path)
			if err != nil {
				errs = multierror.Append(errs, fmt.Errorf("cannot read file %q: %v", path, err))
				return
			}
		}
		warning, err := v.validateFile(path, istioNamespace, defaultNamespace, reader, writer)
		if err != nil {
			errs = multierror.Append(errs, err)
		}
		err = reader.Close()
		if err != nil {
			log.Infof("file: %s is not closed: %v", path, err)
		}
		warningsByFilename[path] = warning
	}
	processDirectory := func(directory string, processFile func(string)) error {
		err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if info.IsDir() {
				return nil
			}

			if isFileFormatValid(path) {
				processFile(path)
			}

			return nil
		})
		return err
	}

	processedFiles := map[string]bool{}
	for _, filename := range filenames {
		var isDir bool
		if filename != "-" {
			fi, err := os.Stat(filename)
			if err != nil {
				errs = multierror.Append(errs, fmt.Errorf("cannot stat file %q: %v", filename, err))
				continue
			}
			isDir = fi.IsDir()
		}

		if !isDir {
			processFile(filename)
			processedFiles[filename] = true
			continue
		}
		if err := processDirectory(filename, func(path string) {
			processFile(path)
			processedFiles[path] = true
		}); err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	filenames = []string{}
	for p := range processedFiles {
		filenames = append(filenames, p)
	}

	if errs != nil {
		// Display warnings we encountered as well
		for _, fname := range filenames {
			if w := warningsByFilename[fname]; w != nil {
				if fname == "-" {
					_, _ = fmt.Fprint(writer, warningToString(w))
					break
				}
				_, _ = fmt.Fprintf(writer, "%q has warnings: %v\n", fname, warningToString(w))
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
		}

		if w := warningsByFilename[fname]; w != nil {
			_, _ = fmt.Fprintf(writer, "%q has warnings: %v\n", fname, warningToString(w))
		} else {
			_, _ = fmt.Fprintf(writer, "%q is valid\n", fname)
		}
	}

	return nil
}

// NewValidateCommand creates a new command for validating Istio k8s resources.
func NewValidateCommand(ctx cli.Context) *cobra.Command {
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

  # Validate all yaml files under samples/bookinfo/networking directory
  istioctl validate -f samples/bookinfo/networking

  # Validate current deployments under 'default' namespace within the cluster
  kubectl get deployments -o yaml | istioctl validate -f -

  # Validate current services under 'default' namespace within the cluster
  kubectl get services -o yaml | istioctl validate -f -

  # Also see the related command 'istioctl analyze'
  istioctl analyze samples/bookinfo/networking/bookinfo-gateway.yaml
`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			istioNamespace := ctx.IstioNamespace()
			defaultNamespace := ctx.NamespaceOrDefault("")
			return validateFiles(&istioNamespace, defaultNamespace, filenames, c.OutOrStderr())
		},
	}

	flags := c.PersistentFlags()
	flags.StringSliceVarP(&filenames, "filename", "f", nil, "Inputs of files to validate")
	flags.BoolVarP(&referential, "referential", "x", true, "Enable structural validation for policy and telemetry")
	_ = flags.MarkHidden("referential")
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
func convertObjectFromUnstructured(schema resource.Schema, un *unstructured.Unstructured, domain string) (*config.Config, error) {
	data, err := fromSchemaAndJSONMap(schema, un.Object["spec"])
	if err != nil {
		return nil, err
	}

	return &config.Config{
		Meta: config.Meta{
			GroupVersionKind:  schema.GroupVersionKind(),
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
func fromSchemaAndJSONMap(schema resource.Schema, data any) (config.Spec, error) {
	// Marshal to json bytes
	str, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	out, err := schema.NewInstance()
	if err != nil {
		return nil, err
	}
	if err = config.ApplyJSONStrict(out, string(str)); err != nil {
		return nil, err
	}
	return out, nil
}

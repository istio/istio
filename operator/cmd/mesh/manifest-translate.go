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

package mesh

import (
	"fmt"
	"io/fs"
	"reflect"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/manifests"
	"istio.io/istio/operator/pkg/component"
	"istio.io/istio/operator/pkg/controlplane"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
)

type ManifestTranslateArgs struct {
	// InFilenames is an array of paths to the input IstioOperator CR files.
	InFilenames []string

	// Set is a string with element format "path=value" where path is an IstioOperator path and the value is a
	// value to set the node at that path to.
	Set []string
	// ManifestsPath is a path to a charts and profiles directory in the local filesystem with a release tgz.
	ManifestsPath string
	// Revision is the Istio control plane revision the command targets.
	Revision string
}

func addManifestTranslateFlags(cmd *cobra.Command, args *ManifestTranslateArgs) {
	cmd.PersistentFlags().StringSliceVarP(&args.InFilenames, "filename", "f", nil, filenameFlagHelpStr)
	cmd.PersistentFlags().StringArrayVarP(&args.Set, "set", "s", nil, setFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.ManifestsPath, "manifests", "d", "", ManifestsFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.Revision, "revision", "r", "", revisionFlagHelpStr)
}

func ManifestTranslateCmd(ctx cli.Context, mgArgs *ManifestTranslateArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "translate",
		Short: "Translates an Istio install manifest to Helm values",
		Long:  "The translate subcommand translates an Istio install manifest and outputs to the console by default.",
		// nolint: lll
		Example: `  # Translate an IstioOperator yaml file into helm values
  istioctl manifest translate -f istio.yaml

  # Translate a default Istio installation
  istioctl manifest translate

  # Enable Tracing
  istioctl manifest translate --set meshConfig.enableTracing=true

  # Translate the demo profile
  istioctl manifest translate --set profile=demo

  # To override a setting that includes dots, escape them with a backslash (\).  Your shell may require enclosing quotes.
  istioctl manifest translate --set "values.sidecarInjectorWebhook.injectedAnnotations.container\.apparmor\.security\.beta\.kubernetes\.io/istio-proxy=runtime/default"
`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("translate accepts no positional arguments, got %#v", args)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if kubeClientFunc == nil {
				kubeClientFunc = ctx.CLIClient
			}
			var kubeClient kube.CLIClient
			l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), installerScope)
			return ManifestTranslate(kubeClient, mgArgs, l)
		},
	}
}

func ManifestTranslate(kubeClient kube.CLIClient, mgArgs *ManifestTranslateArgs, l clog.Logger) error {
	_, iop, err := manifest.GenerateConfig(mgArgs.InFilenames, applyFlagAliases(mgArgs.Set, mgArgs.ManifestsPath, mgArgs.Revision),
		true, kubeClient, l)
	if err != nil {
		return err
	}
	t := translate.NewTranslator()
	valuesMap := map[string]any{}
	debugMap := map[string]any{}
	opts := &component.Options{
		InstallSpec: iop.Spec,
		Translator:  t,
		Filter:      nil,
		Version:     nil,
	}
	comps, err := controlplane.BuildComponents(*opts)
	if err != nil {
		return err
	}
	for _, c := range comps {
		k := c.ComponentName
		compYAML, err := t.TranslateHelmValues(iop.Spec, c.ComponentSpec, k)
		if err != nil {
			return err
		}
		compmap := map[string]any{}
		err = yaml.Unmarshal([]byte(compYAML), &compmap)
		if err != nil {
			return err
		}
		debugMap[string(c.ComponentName)] = compmap
		if err := mergeMaps(compmap, valuesMap, util.Path{}); err != nil {
			return err
		}
	}
	defaults := loadDefaults(mgArgs.ManifestsPath)

	// remove any values that are already defaults
	subtractMaps(defaults, valuesMap)
	// remove any empty parts of the map
	removeEmptyYaml(valuesMap)

	out, err := yaml.Marshal(valuesMap)
	l.Print(string(out))

	return nil
}

func subtractMaps(src, dst map[string]any) {
	if util.IsValueNil(src) {
		return
	}
	for k, v := range src {
		if _, ok := dst[k]; ok {
			if reflect.DeepEqual(v, dst[k]) {
				delete(dst, k)
			} else if reflect.TypeOf(v).Kind() == reflect.Map {
				subtractMaps(v.(map[string]any), dst[k].(map[string]any))
				if dst[k] == nil || len(dst[k].(map[string]any)) == 0 {
					delete(dst, k)
				}
			}
		}
	}
}

func removeEmptyYaml(src map[string]any) {
	if len(src) == 0 {
		return
	}
	for k, v := range src {
		if src[k] == nil {
			delete(src, k)
		} else if reflect.TypeOf(v).Kind() == reflect.Map {
			removeEmptyYaml(v.(map[string]any))
			if len(v.(map[string]any)) == 0 {
				delete(src, k)
			}
		} else if reflect.TypeOf(v).Kind() == reflect.Slice && len(v.([]any)) == 0 {
			delete(src, k)
		}
	}
}

func mergeMaps(src any, dst map[string]any, path util.Path) (errs util.Errors) {
	if util.IsValueNil(src) {
		return nil
	}

	vv := reflect.ValueOf(src)
	vt := reflect.TypeOf(src)
	switch vt.Kind() {
	case reflect.Ptr:
		if !util.IsNilOrInvalidValue(vv.Elem()) {
			errs = util.AppendErrs(errs, mergeMaps(vv.Elem().Interface(), dst, path))
		}
	case reflect.Struct:
		for i := 0; i < vv.NumField(); i++ {
			fieldName := vv.Type().Field(i).Name
			fieldValue := vv.Field(i)
			if a, ok := vv.Type().Field(i).Tag.Lookup("json"); ok && a == "-" {
				continue
			}
			if !fieldValue.CanInterface() {
				continue
			}
			errs = util.AppendErrs(errs, mergeMaps(fieldValue.Interface(), dst, append(path, fieldName)))
		}
	case reflect.Map:
		for _, key := range vv.MapKeys() {
			nnp := append(path, key.String())
			errs = util.AppendErr(errs, mergeLeaf(dst, nnp, vv.MapIndex(key)))
		}
	case reflect.Slice:
		for i := 0; i < vv.Len(); i++ {
			errs = util.AppendErrs(errs, mergeMaps(vv.Index(i).Interface(), dst, path))
		}
	default:
		// Must be a leaf
		if vv.CanInterface() {
			errs = util.AppendErr(errs, mergeLeaf(dst, path, vv))
		}
	}

	return errs
}

func mergeLeaf(out map[string]any, path util.Path, value reflect.Value) error {
	return tpath.WriteNode(out, path, value.Interface())
}

// load all values.yaml files, representing defaults, and merge into single default map
func loadDefaults(manifestsPath string) map[string]any {
	f := manifests.BuiltinOrDir(manifestsPath)
	filenames, err := fs.Glob(f, "charts/**/values.yaml")
	// files, err := os.ReadDir(manifestsPath)
	if err != nil {
		log.Fatal(err)
	}
	result := map[string]any{}
	for _, filename := range filenames {
		fs.ReadFile(f, filename)
		valuesFile, err := fs.ReadFile(f, filename)
		if err != nil {
			continue
		}
		var values map[string]any
		err = yaml.Unmarshal(valuesFile, &values)
		if err != nil {
			log.Fatal(err)
		}
		mergeMaps(values, result, util.Path{})
	}
	return result
}

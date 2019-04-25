/*
Copyright The Helm Authors.

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

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/manifest"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/proto/hapi/release"
	"k8s.io/helm/pkg/renderutil"
	"k8s.io/helm/pkg/tiller"
	"k8s.io/helm/pkg/timeconv"
)

const defaultDirectoryPermission = 0755

var (
	whitespaceRegex = regexp.MustCompile(`^\s*$`)

	// defaultKubeVersion is the default value of --kube-version flag
	defaultKubeVersion = fmt.Sprintf("%s.%s", chartutil.DefaultKubeVersion.Major, chartutil.DefaultKubeVersion.Minor)
)

const templateDesc = `
Render chart templates locally and display the output.

This does not require Tiller. However, any values that would normally be
looked up or retrieved in-cluster will be faked locally. Additionally, none
of the server-side testing of chart validity (e.g. whether an API is supported)
is done.

To render just one template in a chart, use '-x':

	$ helm template mychart -x templates/deployment.yaml
`

type templateCmd struct {
	namespace        string
	valueFiles       valueFiles
	chartPath        string
	out              io.Writer
	values           []string
	stringValues     []string
	fileValues       []string
	nameTemplate     string
	showNotes        bool
	releaseName      string
	releaseIsUpgrade bool
	renderFiles      []string
	kubeVersion      string
	outputDir        string
}

func newTemplateCmd(out io.Writer) *cobra.Command {

	t := &templateCmd{
		out: out,
	}

	cmd := &cobra.Command{
		Use:   "template [flags] CHART",
		Short: fmt.Sprintf("locally render templates"),
		Long:  templateDesc,
		RunE:  t.run,
	}

	f := cmd.Flags()
	f.BoolVar(&t.showNotes, "notes", false, "show the computed NOTES.txt file as well")
	f.StringVarP(&t.releaseName, "name", "n", "release-name", "release name")
	f.BoolVar(&t.releaseIsUpgrade, "is-upgrade", false, "set .Release.IsUpgrade instead of .Release.IsInstall")
	f.StringArrayVarP(&t.renderFiles, "execute", "x", []string{}, "only execute the given templates")
	f.VarP(&t.valueFiles, "values", "f", "specify values in a YAML file (can specify multiple)")
	f.StringVar(&t.namespace, "namespace", "", "namespace to install the release into")
	f.StringArrayVar(&t.values, "set", []string{}, "set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	f.StringArrayVar(&t.stringValues, "set-string", []string{}, "set STRING values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	f.StringArrayVar(&t.fileValues, "set-file", []string{}, "set values from respective files specified via the command line (can specify multiple or separate values with commas: key1=path1,key2=path2)")
	f.StringVar(&t.nameTemplate, "name-template", "", "specify template used to name the release")
	f.StringVar(&t.kubeVersion, "kube-version", defaultKubeVersion, "kubernetes version used as Capabilities.KubeVersion.Major/Minor")
	f.StringVar(&t.outputDir, "output-dir", "", "writes the executed templates to files in output-dir instead of stdout")

	return cmd
}

func (t *templateCmd) run(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return errors.New("chart is required")
	}
	// verify chart path exists
	if _, err := os.Stat(args[0]); err == nil {
		if t.chartPath, err = filepath.Abs(args[0]); err != nil {
			return err
		}
	} else {
		return err
	}

	// verify that output-dir exists if provided
	if t.outputDir != "" {
		_, err := os.Stat(t.outputDir)
		if os.IsNotExist(err) {
			return fmt.Errorf("output-dir '%s' does not exist", t.outputDir)
		}
	}

	if t.namespace == "" {
		t.namespace = defaultNamespace()
	}
	// get combined values and create config
	rawVals, err := vals(t.valueFiles, t.values, t.stringValues, t.fileValues, "", "", "")
	if err != nil {
		return err
	}
	config := &chart.Config{Raw: string(rawVals), Values: map[string]*chart.Value{}}

	// If template is specified, try to run the template.
	if t.nameTemplate != "" {
		t.releaseName, err = generateName(t.nameTemplate)
		if err != nil {
			return err
		}
	}

	if msgs := validation.IsDNS1123Subdomain(t.releaseName); t.releaseName != "" && len(msgs) > 0 {
		return fmt.Errorf("release name %s is invalid: %s", t.releaseName, strings.Join(msgs, ";"))
	}

	// Check chart requirements to make sure all dependencies are present in /charts
	c, err := chartutil.Load(t.chartPath)
	if err != nil {
		return prettyError(err)
	}

	renderOpts := renderutil.Options{
		ReleaseOptions: chartutil.ReleaseOptions{
			Name:      t.releaseName,
			IsInstall: !t.releaseIsUpgrade,
			IsUpgrade: t.releaseIsUpgrade,
			Time:      timeconv.Now(),
			Namespace: t.namespace,
		},
		KubeVersion: t.kubeVersion,
	}

	renderedTemplates, err := renderutil.Render(c, config, renderOpts)
	if err != nil {
		return err
	}

	if settings.Debug {
		rel := &release.Release{
			Name:      t.releaseName,
			Chart:     c,
			Config:    config,
			Version:   1,
			Namespace: t.namespace,
			Info:      &release.Info{LastDeployed: timeconv.Timestamp(time.Now())},
		}
		printRelease(os.Stdout, rel)
	}

	listManifests := manifest.SplitManifests(renderedTemplates)
	var manifestsToRender []manifest.Manifest

	// if we have a list of files to render, then check that each of the
	// provided files exists in the chart.
	if len(t.renderFiles) > 0 {
		for _, f := range t.renderFiles {
			missing := true
			if !filepath.IsAbs(f) {
				newF, err := filepath.Abs(filepath.Join(t.chartPath, f))
				if err != nil {
					return fmt.Errorf("could not turn template path %s into absolute path: %s", f, err)
				}
				f = newF
			}

			for _, manifest := range listManifests {
				// manifest.Name is rendered using linux-style filepath separators on Windows as
				// well as macOS/linux.
				manifestPathSplit := strings.Split(manifest.Name, "/")
				// remove the chart name from the path
				manifestPathSplit = manifestPathSplit[1:]
				toJoin := append([]string{t.chartPath}, manifestPathSplit...)
				manifestPath := filepath.Join(toJoin...)

				// if the filepath provided matches a manifest path in the
				// chart, render that manifest
				if f == manifestPath {
					manifestsToRender = append(manifestsToRender, manifest)
					missing = false
				}
			}
			if missing {
				return fmt.Errorf("could not find template %s in chart", f)
			}
		}
	} else {
		// no renderFiles provided, render all manifests in the chart
		manifestsToRender = listManifests
	}

	for _, m := range tiller.SortByKind(manifestsToRender) {
		data := m.Content
		b := filepath.Base(m.Name)
		if !t.showNotes && b == "NOTES.txt" {
			continue
		}
		if strings.HasPrefix(b, "_") {
			continue
		}

		if t.outputDir != "" {
			// blank template after execution
			if whitespaceRegex.MatchString(data) {
				continue
			}
			err = writeToFile(t.outputDir, m.Name, data)
			if err != nil {
				return err
			}
			continue
		}
		fmt.Printf("---\n# Source: %s\n", m.Name)
		fmt.Println(data)
	}
	return nil
}

// write the <data> to <output-dir>/<name>
func writeToFile(outputDir string, name string, data string) error {
	outfileName := strings.Join([]string{outputDir, name}, string(filepath.Separator))

	err := ensureDirectoryForFile(outfileName)
	if err != nil {
		return err
	}

	f, err := os.Create(outfileName)
	if err != nil {
		return err
	}

	defer f.Close()

	_, err = f.WriteString(fmt.Sprintf("---\n# Source: %s\n%s", name, data))

	if err != nil {
		return err
	}

	fmt.Printf("wrote %s\n", outfileName)
	return nil
}

// check if the directory exists to create file. creates if don't exists
func ensureDirectoryForFile(file string) error {
	baseDir := path.Dir(file)
	_, err := os.Stat(baseDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	return os.MkdirAll(baseDir, defaultDirectoryPermission)
}

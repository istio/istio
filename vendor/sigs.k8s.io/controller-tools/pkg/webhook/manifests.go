/*
Copyright 2018 The Kubernetes Authors.

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

package webhook

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/ghodss/yaml"

	"sigs.k8s.io/controller-runtime/pkg/webhook"
	generateinteral "sigs.k8s.io/controller-tools/pkg/internal/general"
	"sigs.k8s.io/controller-tools/pkg/webhook/internal"
)

// ManifestOptions represent options for generating the webhook manifests.
type ManifestOptions struct {
	InputDir       string
	OutputDir      string
	PatchOutputDir string

	webhooks []webhook.Webhook
	svrOps   *webhook.ServerOptions
	svr      *webhook.Server
}

// SetDefaults sets up the default options for RBAC Manifest generator.
func (o *ManifestOptions) SetDefaults() {
	o.InputDir = filepath.Join(".", "pkg", "webhook")
	o.OutputDir = filepath.Join(".", "config", "webhook")
	o.PatchOutputDir = filepath.Join(".", "config", "default")
}

// Validate validates the input options.
func (o *ManifestOptions) Validate() error {
	if _, err := os.Stat(o.InputDir); err != nil {
		return fmt.Errorf("invalid input directory '%s' %v", o.InputDir, err)
	}
	return nil
}

// Generate generates RBAC manifests by parsing the RBAC annotations in Go source
// files specified in the input directory.
func Generate(o *ManifestOptions) error {
	if err := o.Validate(); err != nil {
		return err
	}

	_, err := os.Stat(o.OutputDir)
	if os.IsNotExist(err) {
		err = os.MkdirAll(o.OutputDir, 0766)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	o.webhooks = []webhook.Webhook{}
	o.svrOps = &webhook.ServerOptions{
		Client: internal.NewManifestClient(path.Join(o.OutputDir, "webhook.yaml")),
	}
	err = generateinteral.ParseDir(o.InputDir, o.parseAnnotation)
	if err != nil {
		return fmt.Errorf("failed to parse the input dir: %v", err)
	}

	o.svr, err = webhook.NewServer("generator", &internal.Manager{}, *o.svrOps)
	if err != nil {
		return err
	}
	err = o.svr.Register(o.webhooks...)
	if err != nil {
		return fmt.Errorf("failed to process the input before generating: %v", err)
	}

	err = o.svr.InstallWebhookManifests()
	if err != nil {
		return err
	}

	return o.labelPatch()
}

func (o *ManifestOptions) labelPatch() error {
	var kustomizeLabelPatch = `apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: controller-manager
spec:
  template:
    metadata:
{{- with .Labels }}
      labels:
{{ toYaml . | indent 8 }}
{{- end }}
`

	type KustomizeLabelPatch struct {
		Labels map[string]string
	}

	p := KustomizeLabelPatch{Labels: o.svrOps.Service.Selectors}
	funcMap := template.FuncMap{
		"toYaml": toYAML,
		"indent": indent,
	}
	temp, err := template.New("kustomizeLabelPatch").Funcs(funcMap).Parse(kustomizeLabelPatch)
	if err != nil {
		return err
	}
	buf := bytes.NewBuffer(nil)
	if err := temp.Execute(buf, p); err != nil {
		return err
	}
	return ioutil.WriteFile(path.Join(o.PatchOutputDir, "manager_label_patch.yaml"), buf.Bytes(), 0644)
}

func toYAML(m map[string]string) (string, error) {
	d, err := yaml.Marshal(m)
	return string(d), err
}

func indent(n int, s string) (string, error) {
	buf := bytes.NewBuffer(nil)
	for _, elem := range strings.Split(s, "\n") {
		for i := 0; i < n; i++ {
			_, err := buf.WriteRune(' ')
			if err != nil {
				return "", err
			}
		}
		_, err := buf.WriteString(elem)
		if err != nil {
			return "", err
		}
		_, err = buf.WriteRune('\n')
		if err != nil {
			return "", err
		}
	}
	return strings.TrimRight(buf.String(), " \n"), nil
}

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

package bootstrap

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

const (
	// EpochFileTemplate is a template for the root config JSON
	EpochFileTemplate = "envoy-rev%d.%s"
	DefaultCfgDir     = "./var/lib/istio/envoy/envoy_bootstrap_tmpl.json"
)

// TODO(nmittler): Move this to application code. This shouldn't be declared in a library.
var overrideVar = env.RegisterStringVar("ISTIO_BOOTSTRAP", "", "")

// Instance of a configured Envoy bootstrap writer.
type Instance interface {
	// WriteTo writes the content of the Envoy bootstrap to the given writer.
	WriteTo(templateFile string, w io.Writer) error

	// CreateFileForEpoch generates an Envoy bootstrap file for a particular epoch.
	CreateFileForEpoch(epoch int) (string, error)
}

// New creates a new Instance of an Envoy bootstrap writer.
func New(cfg Config) Instance {
	return &instance{
		Config: cfg,
	}
}

type instance struct {
	Config
}

func (i *instance) WriteTo(templateFile string, w io.Writer) error {
	// Get the input bootstrap template.
	t, err := newTemplate(templateFile)
	if err != nil {
		return err
	}

	// Create the parameters for the template.
	templateParams, err := i.toTemplateParams()
	if err != nil {
		return err
	}

	// Execute the template.
	return t.Execute(w, templateParams)
}

func toJSON(i any) string {
	if i == nil {
		return "{}"
	}

	ba, err := json.Marshal(i)
	if err != nil {
		log.Warnf("Unable to marshal %v: %v", i, err)
		return "{}"
	}

	return string(ba)
}

// GetEffectiveTemplatePath gets the template file that should be used for bootstrap
func GetEffectiveTemplatePath(pc *model.NodeMetaProxyConfig) string {
	var templateFilePath string
	switch {
	case pc.CustomConfigFile != "":
		templateFilePath = pc.CustomConfigFile
	case pc.ProxyBootstrapTemplatePath != "":
		templateFilePath = pc.ProxyBootstrapTemplatePath
	default:
		templateFilePath = DefaultCfgDir
	}
	override := overrideVar.Get()
	if len(override) > 0 {
		templateFilePath = override
	}
	return templateFilePath
}

func (i *instance) CreateFileForEpoch(epoch int) (string, error) {
	// Create the output file.
	if err := os.MkdirAll(i.Metadata.ProxyConfig.ConfigPath, 0o700); err != nil {
		return "", err
	}

	templateFile := GetEffectiveTemplatePath(i.Metadata.ProxyConfig)

	outputFilePath := configFile(i.Metadata.ProxyConfig.ConfigPath, templateFile, epoch)
	outputFile, err := os.Create(outputFilePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = outputFile.Close() }()

	// Write the content of the file.
	if err := i.WriteTo(templateFile, outputFile); err != nil {
		return "", err
	}

	return outputFilePath, err
}

func configFile(config string, templateFile string, epoch int) string {
	suffix := "json"
	// Envoy will interpret the file extension to determine the type. We should detect yaml inputs
	if strings.HasSuffix(templateFile, ".yaml.tmpl") || strings.HasSuffix(templateFile, ".yaml") {
		suffix = "yaml"
	}
	return path.Join(config, fmt.Sprintf(EpochFileTemplate, epoch, suffix))
}

func newTemplate(templateFilePath string) (*template.Template, error) {
	cfgTmpl, err := os.ReadFile(templateFilePath)
	if err != nil {
		return nil, err
	}

	funcMap := template.FuncMap{
		"toJSON": toJSON,
	}
	return template.New("bootstrap").Funcs(funcMap).Funcs(sprig.GenericFuncMap()).Parse(string(cfgTmpl))
}

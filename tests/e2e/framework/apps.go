// Copyright 2017 Google Inc.
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

package framework

import (
	"flag"
	"path/filepath"

	"github.com/golang/glog"

	"istio.io/istio/tests/e2e/util"
)

const (
	hopImageDefault  = "gcr.io/istio-testing/hop:seb"
	kubeInjectPrefix = "KubeInject"
)

var (
	hopImageHub = flag.String("hop_image", hopImageDefault, "app hub")
)

type AppInterface interface {
	Deploy(string, string, *util.Istioctl) error
}

// App gathers information for Hop app
type App struct {
	AppYamlTemplate string
	AppYaml         string
	KubeInject      bool
}

// Hop gathers information for Hop app
type Hop struct {
	*App
	AppImage   string
	Deployment string
	Service    string
	HTTPPort   int
	GRPCPort   int
	Version    string
}

// DeployAppFromTmpl deploy testing app from tmpl
func (a *App) generateAppYaml(tmpDir string) error {
	if a.AppYamlTemplate == "" {
		return nil
	}
	var err error
	a.AppYaml, err = util.CreateTempfile(tmpDir, filepath.Base(a.AppYamlTemplate), yamlSuffix)
	if err != nil {
		return err
	}
	if err := util.Fill(a.AppYaml, a.AppYamlTemplate, a); err != nil {
		glog.Errorf("Failed to generate yaml for template %s", a.AppYamlTemplate)
		return err
	}
	return nil
}

func (a *App) Deploy(tmpDir, namespace string, istioCtl *util.Istioctl) error {
	if err := a.generateAppYaml(tmpDir); err != nil {
		return err
	}
	finalYaml := a.AppYaml
	if a.KubeInject {
		var err error
		finalYaml, err = util.CreateTempfile(tmpDir, kubeInjectPrefix, yamlSuffix)
		if err != nil {
			return err
		}
		if err = istioCtl.KubeInject(a.AppYaml, finalYaml); err != nil {
			return err
		}
	}
	if err := util.KubeApply(namespace, finalYaml); err != nil {
		glog.Errorf("Kubectl apply %s failed", finalYaml)
		return err
	}
	return nil
}

// Copyright 2017 Istio Authors
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

package main

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/manager/cmd"
	"istio.io/manager/platform/kube/inject"
	"istio.io/manager/test/util"
)

type infra struct {
	// docker tags
	Hub, Tag   string
	MixerImage string
	CaImage    string

	Namespace string
	Verbosity int

	// map from app to pods
	apps map[string][]string

	Auth proxyconfig.ProxyMeshConfig_AuthPolicy

	// switches for infrastructure components
	Mixer   bool
	Ingress bool
	Egress  bool

	// check proxy logs
	checkLogs bool

	namespaceCreated bool
}

func (infra *infra) setup() error {
	if infra.Namespace == "" {
		var err error
		if infra.Namespace, err = util.CreateNamespace(client); err != nil {
			return err
		}
		infra.namespaceCreated = true
	} else {
		if _, err := client.Core().Namespaces().Get(infra.Namespace, meta_v1.GetOptions{}); err != nil {
			return err
		}
	}

	deploy := func(name string) error {
		if yaml, err := fill(name, infra); err != nil {
			return err
		} else if err = infra.kubeApply(yaml); err != nil {
			return err
		}
		return nil
	}

	if err := deploy("config.yaml.tmpl"); err != nil {
		return err
	}
	if err := deploy("manager.yaml.tmpl"); err != nil {
		return err
	}
	if err := deploy("mixer.yaml.tmpl"); err != nil {
		return err
	}
	if infra.Auth != proxyconfig.ProxyMeshConfig_NONE {
		if err := deploy("ca.yaml.tmpl"); err != nil {
			return err
		}
	}
	if infra.Ingress {
		if err := deploy("ingress-proxy.yaml.tmpl"); err != nil {
			return err
		}
	}
	if infra.Egress {
		if err := deploy("egress-proxy.yaml.tmpl"); err != nil {
			return err
		}
	}

	return nil
}

func (infra *infra) deployApps() error {
	// deploy a healthy mix of apps, with and without proxy
	if err := infra.deployApp("t", "t", "8080", "80", "9090", "90", "unversioned", false); err != nil {
		return err
	}
	if err := infra.deployApp("a", "a", "8080", "80", "9090", "90", "v1", true); err != nil {
		return err
	}
	if err := infra.deployApp("b", "b", "80", "8080", "90", "9090", "unversioned", true); err != nil {
		return err
	}
	if err := infra.deployApp("c-v1", "c", "80", "8080", "90", "9090", "v1", true); err != nil {
		return err
	}
	if err := infra.deployApp("c-v2", "c", "80", "8080", "90", "9090", "v2", true); err != nil {
		return err
	}
	return nil
}

func (infra *infra) deployApp(deployment, svcName, port1, port2, port3, port4, version string, injectProxy bool) error {
	w, err := fill("app.yaml.tmpl", map[string]string{
		"Hub":        infra.Hub,
		"Tag":        infra.Tag,
		"service":    svcName,
		"deployment": deployment,
		"port1":      port1,
		"port2":      port2,
		"port3":      port3,
		"port4":      port4,
		"version":    version,
	})
	if err != nil {
		return err
	}

	writer := new(bytes.Buffer)

	if injectProxy {
		mesh, err := cmd.GetMeshConfig(client, infra.Namespace, "istio")
		if err != nil {
			return err
		}

		p := &inject.Params{
			InitImage:       inject.InitImageName(infra.Hub, infra.Tag),
			ProxyImage:      inject.ProxyImageName(infra.Hub, infra.Tag),
			Verbosity:       infra.Verbosity,
			SidecarProxyUID: inject.DefaultSidecarProxyUID,
			Version:         "manager-integration-test",
			Mesh:            mesh,
		}
		if err := inject.IntoResourceFile(p, strings.NewReader(w), writer); err != nil {
			return err
		}
	} else {
		if _, err := io.Copy(writer, strings.NewReader(w)); err != nil {
			return err
		}
	}

	return infra.kubeApply(writer.String())
}

func (infra *infra) teardown() {
	if infra.namespaceCreated {
		util.DeleteNamespace(client, infra.Namespace)
		infra.Namespace = ""
	}
}

func (infra *infra) kubeApply(yaml string) error {
	return util.RunInput(fmt.Sprintf("kubectl apply -n %s -f -", infra.Namespace), yaml)
}

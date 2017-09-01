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
	"io/ioutil"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/adapter/config/crd"
	"istio.io/pilot/model"
	"istio.io/pilot/platform/kube/inject"
	"istio.io/pilot/test/util"
)

const (
	ingressSecretName = "istio-ingress-certs"
)

type infra struct {
	Name string

	// docker tags
	Hub, Tag   string
	MixerImage string
	CaImage    string

	Namespace      string
	IstioNamespace string
	Verbosity      int

	// map from app to pods
	apps map[string][]string

	Auth proxyconfig.ProxyMeshConfig_AuthPolicy

	// switches for infrastructure components
	Mixer     bool
	Ingress   bool
	Egress    bool
	Zipkin    bool
	DebugPort int

	// check proxy logs
	checkLogs bool

	namespaceCreated      bool
	istioNamespaceCreated bool

	// sidecar initializer
	UseInitializer bool
	InjectConfig   *inject.Config
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

	if infra.IstioNamespace == "" {
		var err error
		if infra.IstioNamespace, err = util.CreateNamespace(client); err != nil {
			return err
		}
		infra.istioNamespaceCreated = true
	} else {
		if _, err := client.Core().Namespaces().Get(infra.IstioNamespace, meta_v1.GetOptions{}); err != nil {
			return err
		}
	}

	deploy := func(name, namespace string) error {
		if yaml, err := fill(name, infra); err != nil {
			return err
		} else if err = infra.kubeApply(yaml, namespace); err != nil {
			return err
		}
		return nil
	}
	if err := deploy("rbac-beta.yaml.tmpl", infra.IstioNamespace); err != nil {
		return err
	}
	if err := deploy("config.yaml.tmpl", infra.Namespace); err != nil {
		return err
	}

	if err := deploy("config.yaml.tmpl", infra.IstioNamespace); err != nil {
		return err
	}

	mesh, err := inject.GetMeshConfig(client, infra.IstioNamespace, "istio")
	if err != nil {
		return err
	}

	infra.InjectConfig = &inject.Config{
		Policy:     inject.InjectionPolicyOptOut,
		Namespaces: []string{infra.Namespace, infra.IstioNamespace},
		Params: inject.Params{
			InitImage:         inject.InitImageName(infra.Hub, infra.Tag),
			ProxyImage:        inject.ProxyImageName(infra.Hub, infra.Tag),
			Verbosity:         infra.Verbosity,
			SidecarProxyUID:   inject.DefaultSidecarProxyUID,
			EnableCoreDump:    true,
			Version:           "integration-test",
			Mesh:              mesh,
			MeshConfigMapName: "istio",
		},
	}

	// NOTE: InitializerConfiguration is cluster-scoped and may be
	// created and used by other tests in the same test cluster.
	if infra.UseInitializer {
		if err := deploy("initializer-config.yaml.tmpl", infra.IstioNamespace); err != nil {
			return err
		}
	}

	// TODO - Initializer configs can block initializers from being
	// deployed. The workaround is to explicitly set the initializer
	// field of the deployment to an empty list, thus bypassing the
	// initializers. This works when the deployment is first created,
	// but Subsequent modifications with 'apply' fail with the
	// message:
	//
	// 		The Deployment "istio-sidecar-initializer" is invalid:
	// 		metadata.initializers: Invalid value: "null": field is
	// 		immutable once initialization has completed.
	//
	// Delete any existing initializer deployment from previous test
	// runs first before trying to (re)create it.
	//
	// See github.com/kubernetes/kubernetes/issues/49048 for k8s
	// tracking issue.
	if yaml, err := fill("initializer.yaml.tmpl", infra); err != nil {
		return err
	} else if err = infra.kubeDelete(yaml, infra.IstioNamespace); err != nil {
		glog.Infof("Sidecar initializer could not be deleted: %v", err)
	}

	if yaml, err := fill("initializer-configmap.yaml.tmpl", &infra.InjectConfig); err != nil {
		return err
	} else if err = infra.kubeApply(yaml, infra.IstioNamespace); err != nil {
		return err
	}
	if err := deploy("initializer.yaml.tmpl", infra.IstioNamespace); err != nil {
		return err
	}

	if err := deploy("pilot.yaml.tmpl", infra.IstioNamespace); err != nil {
		return err
	}
	if err := deploy("mixer.yaml.tmpl", infra.IstioNamespace); err != nil {
		return err
	}

	if infra.Auth != proxyconfig.ProxyMeshConfig_NONE {
		if err := deploy("ca.yaml.tmpl", infra.IstioNamespace); err != nil {
			return err
		}
	}
	if infra.Ingress {
		if err := deploy("ingress-proxy.yaml.tmpl", infra.IstioNamespace); err != nil {
			return err
		}
		// Update ingress key/cert in secret
		key, err := ioutil.ReadFile("docker/certs/cert.key")
		if err != nil {
			return err
		}
		crt, err := ioutil.ReadFile("docker/certs/cert.crt")
		if err != nil {
			return err
		}
		_, err = client.CoreV1().Secrets(infra.IstioNamespace).Update(&v1.Secret{
			ObjectMeta: meta_v1.ObjectMeta{Name: ingressSecretName},
			Data: map[string][]byte{
				"tls.key": key,
				"tls.crt": crt,
			},
		})
		if err != nil {
			return err
		}
	}
	if infra.Egress {
		if err := deploy("egress-proxy.yaml.tmpl", infra.IstioNamespace); err != nil {
			return err
		}
	}
	if infra.Zipkin {
		if err := deploy("zipkin.yaml", infra.IstioNamespace); err != nil {
			return err
		}
	}

	return nil
}

func (infra *infra) deployApps() error {
	// deploy a healthy mix of apps, with and without proxy
	if err := infra.deployApp("t", "t", 8080, 80, 9090, 90, 7070, 70, "unversioned", false); err != nil {
		return err
	}
	if err := infra.deployApp("a", "a", 8080, 80, 9090, 90, 7070, 70, "v1", true); err != nil {
		return err
	}
	if err := infra.deployApp("b", "b", 80, 8080, 90, 9090, 70, 7070, "unversioned", true); err != nil {
		return err
	}
	if err := infra.deployApp("c-v1", "c", 80, 8080, 90, 9090, 70, 7070, "v1", true); err != nil {
		return err
	}
	if err := infra.deployApp("c-v2", "c", 80, 8080, 90, 9090, 70, 7070, "v2", true); err != nil {
		return err
	}
	return nil
}

func (infra *infra) deployApp(deployment, svcName string, port1, port2, port3, port4, port5, port6 int,
	version string, injectProxy bool) error {
	w, err := fill("app.yaml.tmpl", map[string]string{
		"Hub":            infra.Hub,
		"Tag":            infra.Tag,
		"service":        svcName,
		"deployment":     deployment,
		"port1":          strconv.Itoa(port1),
		"port2":          strconv.Itoa(port2),
		"port3":          strconv.Itoa(port3),
		"port4":          strconv.Itoa(port4),
		"port5":          strconv.Itoa(port5),
		"port6":          strconv.Itoa(port6),
		"version":        version,
		"istioNamespace": infra.IstioNamespace,
		"injectProxy":    strconv.FormatBool(injectProxy),
	})
	if err != nil {
		return err
	}

	writer := new(bytes.Buffer)

	if injectProxy && !infra.UseInitializer {
		if err := inject.IntoResourceFile(infra.InjectConfig, strings.NewReader(w), writer); err != nil {
			return err
		}
	} else {
		if _, err := io.Copy(writer, strings.NewReader(w)); err != nil {
			return err
		}
	}

	return infra.kubeApply(writer.String(), infra.Namespace)
}

func (infra *infra) teardown() {
	if infra.namespaceCreated {
		util.DeleteNamespace(client, infra.Namespace)
		infra.Namespace = ""
	}
	if infra.istioNamespaceCreated {
		util.DeleteNamespace(client, infra.IstioNamespace)
		infra.IstioNamespace = ""
	}
}

func (infra *infra) kubeApply(yaml, namespace string) error {
	return util.RunInput(fmt.Sprintf("kubectl apply --kubeconfig %s -n %s -f -",
		kubeconfig, namespace), yaml)
}

func (infra *infra) kubeDelete(yaml, namespace string) error {
	return util.RunInput(fmt.Sprintf("kubectl delete --kubeconfig %s -n %s -f -",
		kubeconfig, namespace), yaml)
}

type response struct {
	body    string
	id      []string
	version []string
	port    []string
	code    []string
}

const httpOk = "200"

var (
	idRex      = regexp.MustCompile("(?i)X-Request-Id=(.*)")
	versionRex = regexp.MustCompile("ServiceVersion=(.*)")
	portRex    = regexp.MustCompile("ServicePort=(.*)")
	codeRex    = regexp.MustCompile("StatusCode=(.*)")
)

func (infra *infra) clientRequest(app, url string, count int, extra string) response {
	out := response{}
	if len(infra.apps[app]) == 0 {
		glog.Errorf("missing pod names for app %q", app)
		return out
	}

	pod := infra.apps[app][0]
	cmd := fmt.Sprintf("kubectl exec %s --kubeconfig %s -n %s -c app -- client -url %s -count %d %s",
		pod, kubeconfig, infra.Namespace, url, count, extra)
	request, err := util.Shell(cmd)

	if err != nil {
		glog.Errorf("client request error %v for %s in %s", err, url, app)
		return out
	}

	out.body = request

	ids := idRex.FindAllStringSubmatch(request, -1)
	for _, id := range ids {
		out.id = append(out.id, id[1])
	}

	versions := versionRex.FindAllStringSubmatch(request, -1)
	for _, version := range versions {
		out.version = append(out.version, version[1])
	}

	ports := portRex.FindAllStringSubmatch(request, -1)
	for _, port := range ports {
		out.port = append(out.port, port[1])
	}

	codes := codeRex.FindAllStringSubmatch(request, -1)
	for _, code := range codes {
		out.code = append(out.code, code[1])
	}

	return out
}

func (infra *infra) applyConfig(inFile string, data map[string]string) error {
	config, err := fill(inFile, data)
	if err != nil {
		return err
	}

	v, err := model.IstioConfigTypes.FromYAML([]byte(config))
	if err != nil {
		return err
	}

	istioClient, err := crd.NewClient(kubeconfig, model.IstioConfigTypes)
	if err != nil {
		return err
	}

	old, exists := istioClient.Get(v.Type, v.Name, v.Namespace)
	if exists {
		v.ResourceVersion = old.ResourceVersion
		_, err = istioClient.Update(*v)
	} else {
		_, err = istioClient.Create(*v)
	}
	if err != nil {
		return err
	}

	glog.Info("Sleeping for the config to propagate")
	time.Sleep(3 * time.Second)
	return nil
}

func (infra *infra) deleteAllConfigs() error {
	istioClient, err := crd.NewClient(kubeconfig, model.IstioConfigTypes)
	if err != nil {
		return err
	}
	for _, desc := range istioClient.ConfigDescriptor() {
		configs, err := istioClient.List(desc.Type, infra.Namespace)
		if err != nil {
			return err
		}
		for _, config := range configs {
			glog.Infof("Delete config %s", config.Key())
			if err = istioClient.Delete(desc.Type, config.Name, config.Namespace); err != nil {
				return err
			}
		}
	}
	return nil
}

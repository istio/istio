//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package namespace

import (
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"path"
	"sync"
	"time"

	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	k "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
)

var (
	idctr int64
	rnd   = rand.New(rand.NewSource(time.Now().UnixNano()))
	mu    sync.Mutex
)

// kubeNamespace represents a Kubernetes namespace. It is tracked as a resource.
type kubeNamespace struct {
	id   resource.ID
	name string
	a    *k.Accessor
	ctx  resource.Context
}

func (n *kubeNamespace) Dump() {
	scopes.CI.Errorf("=== Dumping Namespace %s State...", n.name)

	d, err := n.ctx.CreateTmpDirectory(n.name + "-state")
	if err != nil {
		scopes.CI.Errorf("Unable to create directory for dumping %s contents: %v", n.name, err)
		return
	}

	pods, err := n.a.GetPods(n.name)
	if err != nil {
		scopes.CI.Errorf("Unable to get pods from the namespace: %v", err)
		return
	}

	for _, pod := range pods {
		for _, container := range pod.Spec.Containers {
			l, err := n.a.Logs(pod.Namespace, pod.Name, container.Name, false /* previousLog */)
			if err != nil {
				scopes.CI.Errorf("Unable to get logs for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
				continue
			}

			fname := path.Join(d, fmt.Sprintf("%s-%s.log", pod.Name, container.Name))
			if err = ioutil.WriteFile(fname, []byte(l), os.ModePerm); err != nil {
				scopes.CI.Errorf("Unable to write logs for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
			}
		}
	}
}

var _ Instance = &kubeNamespace{}
var _ io.Closer = &kubeNamespace{}
var _ resource.Resource = &kubeNamespace{}
var _ resource.Dumper = &kubeNamespace{}

func (n *kubeNamespace) Name() string {
	return n.name
}

func (n *kubeNamespace) ID() resource.ID {
	return n.id
}

// Close implements io.Closer
func (n *kubeNamespace) Close() (err error) {
	if n.name != "" {
		scopes.Framework.Debugf("%s deleting namespace", n.id)
		ns := n.name
		n.name = ""
		err = n.a.DeleteNamespace(ns)
	}

	scopes.Framework.Debugf("%s close complete (err:%v)", n.id, err)
	return
}

func claimKube(ctx resource.Context, name string) (Instance, error) {
	env := ctx.Environment().(*kube.Environment)
	cfg, err := istio.DefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	if !env.Accessor.NamespaceExists(name) {
		nsConfig := Config{
			Inject:                  true,
			CustomInjectorNamespace: cfg.CustomSidecarInjectorNamespace,
		}
		nsLabels := createNamespaceLabels(&nsConfig)
		if err := env.CreateNamespaceWithLabels(name, "istio-test", nsLabels); err != nil {
			return nil, err
		}

	}
	return &kubeNamespace{name: name}, nil
}

// NewNamespace allocates a new testing namespace.
func newKube(ctx resource.Context, nsConfig *Config) (Instance, error) {
	mu.Lock()
	idctr++
	nsid := idctr
	r := rnd.Intn(99999)
	mu.Unlock()

	env := ctx.Environment().(*kube.Environment)
	ns := fmt.Sprintf("%s-%d-%d", nsConfig.Prefix, nsid, r)

	nsLabels := createNamespaceLabels(nsConfig)
	if err := env.CreateNamespaceWithLabels(ns, "istio-test", nsLabels); err != nil {
		return nil, err
	}

	n := &kubeNamespace{name: ns, a: env.Accessor, ctx: ctx}
	id := ctx.TrackResource(n)
	n.id = id

	return n, nil
}

// createNamespaceLabels will take a namespace config and generate the proper k8s labels
func createNamespaceLabels(cfg *Config) map[string]string {
	l := make(map[string]string)
	if cfg.Inject {
		l["istio-injection"] = "enabled"
		if cfg.CustomInjectorNamespace != "" {
			l["istio-env"] = cfg.CustomInjectorNamespace
		}
	}

	// bring over supplied labels
	for k, v := range cfg.Labels {
		l[k] = v
	}
	return l
}

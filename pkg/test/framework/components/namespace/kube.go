//  Copyright Istio Authors
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
	"context"
	"fmt"
	"io"
	"math/rand"
	"sync"
	"time"

	kubeApiCore "k8s.io/api/core/v1"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"

	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	kube2 "istio.io/istio/pkg/test/kube"
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
	ctx  resource.Context
}

func (n *kubeNamespace) Dump() {
	scopes.Framework.Errorf("=== Dumping Namespace %s State...", n.name)

	d, err := n.ctx.CreateTmpDirectory(n.name + "-state")
	if err != nil {
		scopes.Framework.Errorf("Unable to create directory for dumping %s contents: %v", n.name, err)
		return
	}

	for _, cluster := range n.ctx.Clusters() {
		kube2.DumpPods(cluster, d, n.name)
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

		for _, cluster := range n.ctx.Clusters() {
			err = cluster.CoreV1().Namespaces().Delete(context.TODO(), ns, kube2.DeleteOptionsForeground())
		}
	}

	scopes.Framework.Debugf("%s close complete (err:%v)", n.id, err)
	return
}

func claimKube(ctx resource.Context, name string, injectSidecar bool) (Instance, error) {
	env := ctx.Environment().(*kube.Environment)
	cfg, err := istio.DefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	for _, cluster := range env.KubeClusters {
		if !kube2.NamespaceExists(cluster, name) {
			nsConfig := Config{
				Inject:   injectSidecar,
				Revision: cfg.CustomSidecarInjectorNamespace,
			}

			if _, err := cluster.CoreV1().Namespaces().Create(context.TODO(), &kubeApiCore.Namespace{
				ObjectMeta: kubeApiMeta.ObjectMeta{
					Name:   name,
					Labels: createNamespaceLabels(&nsConfig),
				},
			}, kubeApiMeta.CreateOptions{}); err != nil {
				return nil, err
			}
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

	ns := fmt.Sprintf("%s-%d-%d", nsConfig.Prefix, nsid, r)
	n := &kubeNamespace{
		name: ns,
		ctx:  ctx,
	}
	id := ctx.TrackResource(n)
	n.id = id

	for _, cluster := range n.ctx.Clusters() {
		if _, err := cluster.CoreV1().Namespaces().Create(context.TODO(), &kubeApiCore.Namespace{
			ObjectMeta: kubeApiMeta.ObjectMeta{
				Name:   ns,
				Labels: createNamespaceLabels(nsConfig),
			},
		}, kubeApiMeta.CreateOptions{}); err != nil {
			return nil, err
		}
	}

	return n, nil
}

// createNamespaceLabels will take a namespace config and generate the proper k8s labels
func createNamespaceLabels(cfg *Config) map[string]string {
	l := make(map[string]string)
	l["istio-testing"] = "istio-test"
	if cfg.Inject {
		if cfg.Revision != "" {
			l[label.IstioRev] = cfg.Revision
		} else {
			l["istio-injection"] = "enabled"
		}
	}

	// bring over supplied labels
	for k, v := range cfg.Labels {
		l[k] = v
	}
	return l
}

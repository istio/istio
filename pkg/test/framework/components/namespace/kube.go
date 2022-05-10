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
	"strings"
	"sync"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/go-multierror"
	kubeApiCore "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/label"
	"istio.io/istio/pkg/test/framework/resource"
	kube2 "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/pkg/log"
)

var (
	idctr int64
	rnd   = rand.New(rand.NewSource(time.Now().UnixNano()))
	mu    sync.Mutex
)

// kubeNamespace represents a Kubernetes namespace. It is tracked as a resource.
type kubeNamespace struct {
	id           resource.ID
	name         string
	prefix       string
	cleanupFuncs []func() error
	ctx          resource.Context
}

func (n *kubeNamespace) Dump(ctx resource.Context) {
	scopes.Framework.Errorf("=== Dumping Namespace %s State...", n.name)

	d, err := ctx.CreateTmpDirectory(n.name + "-state")
	if err != nil {
		scopes.Framework.Errorf("Unable to create directory for dumping %s contents: %v", n.name, err)
		return
	}

	kube2.DumpPods(n.ctx, d, n.name, []string{})
	kube2.DumpDeployments(n.ctx, d, n.name)
}

var (
	_ Instance          = &kubeNamespace{}
	_ io.Closer         = &kubeNamespace{}
	_ resource.Resource = &kubeNamespace{}
	_ resource.Dumper   = &kubeNamespace{}
)

func (n *kubeNamespace) Name() string {
	return n.name
}

func (n *kubeNamespace) Prefix() string {
	return n.prefix
}

func (n *kubeNamespace) Labels() (map[string]string, error) {
	perCluster := make([]map[string]string, len(n.ctx.Clusters()))
	errG := multierror.Group{}
	for i, cluster := range n.ctx.Clusters() {
		i, cluster := i, cluster
		errG.Go(func() error {
			ns, err := cluster.Kube().CoreV1().Namespaces().Get(context.TODO(), n.Name(), metav1.GetOptions{})
			if err != nil {
				return err
			}
			perCluster[i] = ns.Labels
			return nil
		})
	}
	if err := errG.Wait().ErrorOrNil(); err != nil {
		return nil, err
	}
	for i, clusterLabels := range perCluster {
		if i == 0 {
			continue
		}
		if diff := cmp.Diff(perCluster[0], clusterLabels); diff != "" {
			log.Warnf("namespace labels are different across clusters:\n%s", diff)
		}
	}
	return perCluster[0], nil
}

func (n *kubeNamespace) SetLabel(key, value string) error {
	return n.setNamespaceLabel(key, value)
}

func (n *kubeNamespace) RemoveLabel(key string) error {
	return n.removeNamespaceLabel(key)
}

func (n *kubeNamespace) ID() resource.ID {
	return n.id
}

// Close implements io.Closer
func (n *kubeNamespace) Close() (err error) {
	// Get non-nil cleanup funcs.
	var cleanupFuncs []func() error
	for _, fn := range n.cleanupFuncs {
		if fn != nil {
			cleanupFuncs = append(cleanupFuncs, fn)
		}
	}

	// Clear out the cleanup funcs and name to mark this namespace as closed.
	n.cleanupFuncs = nil
	n.name = ""

	if len(cleanupFuncs) > 0 {
		scopes.Framework.Debugf("%s deleting namespace", n.id)

		g := multierror.Group{}
		for _, cleanup := range cleanupFuncs {
			g.Go(cleanup)
		}

		err = g.Wait().ErrorOrNil()
	}

	scopes.Framework.Debugf("%s close complete (err:%v)", n.id, err)
	return
}

func claimKube(ctx resource.Context, nsConfig *Config) (Instance, error) {
	clusters := ctx.Clusters().Kube()
	n := &kubeNamespace{
		ctx:          ctx,
		prefix:       nsConfig.Prefix,
		name:         nsConfig.Prefix,
		cleanupFuncs: make([]func() error, len(clusters)),
	}

	id := ctx.TrackResource(n)
	n.id = id
	name := nsConfig.Prefix

	g := multierror.Group{}
	for i, cluster := range clusters {
		i := i
		cluster := cluster
		if !kube2.NamespaceExists(cluster.Kube(), name) {
			g.Go(func() error {
				if _, err := cluster.Kube().CoreV1().Namespaces().Create(context.TODO(), &kubeApiCore.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   name,
						Labels: createNamespaceLabels(ctx, nsConfig),
					},
				}, metav1.CreateOptions{}); err != nil {
					return err
				}

				n.cleanupFuncs[i] = func() error {
					return cluster.Kube().CoreV1().Namespaces().Delete(context.TODO(), name, kube2.DeleteOptionsForeground())
				}
				return nil
			})
		}
	}

	return n, nil
}

// setNamespaceLabel labels a namespace with the given key, value pair
func (n *kubeNamespace) setNamespaceLabel(key, value string) error {
	// need to convert '/' to '~1' as per the JSON patch spec http://jsonpatch.com/#operations
	jsonPatchEscapedKey := strings.ReplaceAll(key, "/", "~1")
	name := n.name

	g := multierror.Group{}
	for _, cluster := range n.ctx.Clusters().Kube() {
		cluster := cluster
		nsLabelPatch := fmt.Sprintf(`[{"op":"replace","path":"/metadata/labels/%s","value":"%s"}]`, jsonPatchEscapedKey, value)
		g.Go(func() error {
			_, err := cluster.Kube().CoreV1().Namespaces().Patch(context.TODO(), name, types.JSONPatchType, []byte(nsLabelPatch), metav1.PatchOptions{})
			return err
		})
	}

	return g.Wait().ErrorOrNil()
}

// removeNamespaceLabel removes namespace label with the given key
func (n *kubeNamespace) removeNamespaceLabel(key string) error {
	// need to convert '/' to '~1' as per the JSON patch spec http://jsonpatch.com/#operations
	jsonPatchEscapedKey := strings.ReplaceAll(key, "/", "~1")
	name := n.name

	g := multierror.Group{}
	for _, cluster := range n.ctx.Clusters().Kube() {
		cluster := cluster
		nsLabelPatch := fmt.Sprintf(`[{"op":"remove","path":"/metadata/labels/%s"}]`, jsonPatchEscapedKey)
		g.Go(func() error {
			_, err := cluster.Kube().CoreV1().Namespaces().Patch(context.TODO(), name, types.JSONPatchType, []byte(nsLabelPatch), metav1.PatchOptions{})
			return err
		})
	}

	return g.Wait().ErrorOrNil()
}

// NewNamespace allocates a new testing namespace.
func newKube(ctx resource.Context, nsConfig *Config) (Instance, error) {
	mu.Lock()
	idctr++
	nsid := idctr
	r := rnd.Intn(99999)
	mu.Unlock()

	clusters := ctx.Clusters().Kube()
	name := fmt.Sprintf("%s-%d-%d", nsConfig.Prefix, nsid, r)
	n := &kubeNamespace{
		name:         name,
		prefix:       nsConfig.Prefix,
		ctx:          ctx,
		cleanupFuncs: make([]func() error, len(clusters)),
	}
	id := ctx.TrackResource(n)
	n.id = id

	s := ctx.Settings()
	g := multierror.Group{}
	for i, cluster := range clusters {
		i := i
		cluster := cluster

		g.Go(func() error {
			if _, err := cluster.Kube().CoreV1().Namespaces().Create(context.TODO(), &kubeApiCore.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: createNamespaceLabels(ctx, nsConfig),
				},
			}, metav1.CreateOptions{}); err != nil {
				return err
			}

			n.cleanupFuncs[i] = func() error {
				return cluster.Kube().CoreV1().Namespaces().Delete(context.TODO(), name, kube2.DeleteOptionsForeground())
			}

			if s.Image.PullSecret != "" {
				if err := cluster.ApplyYAMLFiles(n.name, s.Image.PullSecret); err != nil {
					return err
				}
			}
			return nil
		})
	}

	if err := g.Wait().ErrorOrNil(); err != nil {
		return nil, err
	}

	return n, nil
}

// createNamespaceLabels will take a namespace config and generate the proper k8s labels
func createNamespaceLabels(ctx resource.Context, cfg *Config) map[string]string {
	l := make(map[string]string)
	l["istio-testing"] = "istio-test"
	if cfg.Inject {
		// do not add namespace labels when running compatibility tests since
		// this disables the necessary object selectors
		if !ctx.Settings().Compatibility {
			if cfg.Revision != "" {
				l[label.IoIstioRev.Name] = cfg.Revision
			} else {
				l["istio-injection"] = "enabled"
			}
		}
	} else {
		// if we're running compatibility tests, disable injection in the namespace
		// explicitly so that object selectors are ignored
		if ctx.Settings().Compatibility {
			l["istio-injection"] = "disabled"
		}
	}

	// bring over supplied labels
	for k, v := range cfg.Labels {
		l[k] = v
	}
	return l
}

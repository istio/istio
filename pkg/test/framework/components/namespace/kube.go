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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/label"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
	kube2 "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

// nolint: gosec
// Test only code
var (
	idctr int64
	rnd   = rand.New(rand.NewSource(time.Now().UnixNano()))
	mu    sync.Mutex
)

// kubeNamespace represents a Kubernetes namespace. It is tracked as a resource.
type kubeNamespace struct {
	ctx          resource.Context
	id           resource.ID
	name         string
	prefix       string
	cleanupMutex sync.Mutex
	cleanupFuncs []func() error
	skipDump     bool
}

func (n *kubeNamespace) Dump(ctx resource.Context) {
	if n.skipDump {
		scopes.Framework.Debugf("=== Skip dumping Namespace %s State for %v...", n.name, ctx.ID())
		return
	}
	scopes.Framework.Errorf("=== Dumping Namespace %s State for %v...", n.name, ctx.ID())

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
	perCluster := make([]map[string]string, len(n.ctx.AllClusters()))
	if err := n.forEachCluster(func(i int, c cluster.Cluster) error {
		ns, err := c.Kube().CoreV1().Namespaces().Get(context.TODO(), n.Name(), metav1.GetOptions{})
		if err != nil {
			return err
		}
		perCluster[i] = ns.Labels
		return nil
	}); err != nil {
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

func (n *kubeNamespace) Close() error {
	// Get the cleanup funcs and clear the array to prevent us from cleaning up multiple times.
	n.cleanupMutex.Lock()
	cleanupFuncs := n.cleanupFuncs
	n.cleanupFuncs = nil
	n.cleanupMutex.Unlock()

	// Perform the cleanup across all clusters concurrently.
	var err error
	if len(cleanupFuncs) > 0 {
		scopes.Framework.Debugf("%s deleting namespace %v", n.id, n.name)

		g := multierror.Group{}
		for _, cleanup := range cleanupFuncs {
			g.Go(cleanup)
		}

		err = g.Wait().ErrorOrNil()
	}

	scopes.Framework.Debugf("%s close complete (err:%v)", n.id, err)
	return err
}

func claimKube(ctx resource.Context, cfg Config) (Instance, error) {
	name := cfg.Prefix
	n := &kubeNamespace{
		ctx:      ctx,
		prefix:   name,
		name:     name,
		skipDump: cfg.SkipDump,
	}

	id := ctx.TrackResource(n)
	n.id = id

	if err := n.forEachCluster(func(_ int, c cluster.Cluster) error {
		if !kube2.NamespaceExists(c.Kube(), name) {
			return n.createInCluster(c, cfg)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return n, nil
}

// setNamespaceLabel labels a namespace with the given key, value pair
func (n *kubeNamespace) setNamespaceLabel(key, value string) error {
	// need to convert '/' to '~1' as per the JSON patch spec http://jsonpatch.com/#operations
	jsonPatchEscapedKey := strings.ReplaceAll(key, "/", "~1")
	nsLabelPatch := fmt.Sprintf(`[{"op":"replace","path":"/metadata/labels/%s","value":"%s"}]`, jsonPatchEscapedKey, value)

	return n.forEachCluster(func(_ int, c cluster.Cluster) error {
		_, err := c.Kube().CoreV1().Namespaces().Patch(context.TODO(), n.name, types.JSONPatchType, []byte(nsLabelPatch), metav1.PatchOptions{})
		return err
	})
}

// removeNamespaceLabel removes namespace label with the given key
func (n *kubeNamespace) removeNamespaceLabel(key string) error {
	// need to convert '/' to '~1' as per the JSON patch spec http://jsonpatch.com/#operations
	jsonPatchEscapedKey := strings.ReplaceAll(key, "/", "~1")
	nsLabelPatch := fmt.Sprintf(`[{"op":"remove","path":"/metadata/labels/%s"}]`, jsonPatchEscapedKey)
	name := n.name

	return n.forEachCluster(func(_ int, c cluster.Cluster) error {
		_, err := c.Kube().CoreV1().Namespaces().Patch(context.TODO(), name, types.JSONPatchType, []byte(nsLabelPatch), metav1.PatchOptions{})
		return err
	})
}

// NewNamespace allocates a new testing namespace.
func newKube(ctx resource.Context, cfg Config) (Instance, error) {
	mu.Lock()
	idctr++
	nsid := idctr
	r := rnd.Intn(99999)
	mu.Unlock()

	name := fmt.Sprintf("%s-%d-%d", cfg.Prefix, nsid, r)
	n := &kubeNamespace{
		name:   name,
		prefix: cfg.Prefix,
		ctx:    ctx,
	}
	id := ctx.TrackResource(n)
	n.id = id

	if err := n.forEachCluster(func(_ int, c cluster.Cluster) error {
		return n.createInCluster(c, cfg)
	}); err != nil {
		return nil, err
	}

	return n, nil
}

func (n *kubeNamespace) createInCluster(c cluster.Cluster, cfg Config) error {
	if _, err := c.Kube().CoreV1().Namespaces().Create(context.TODO(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   n.name,
			Labels: createNamespaceLabels(n.ctx, cfg),
		},
	}, metav1.CreateOptions{}); err != nil {
		return err
	}

	if !cfg.SkipCleanup {
		n.addCleanup(func() error {
			return c.Kube().CoreV1().Namespaces().Delete(context.TODO(), n.name, kube2.DeleteOptionsForeground())
		})
	}

	s := n.ctx.Settings()
	if s.Image.PullSecret != "" {
		if err := c.ApplyYAMLFiles(n.name, s.Image.PullSecret); err != nil {
			return err
		}
		err := retry.UntilSuccess(func() error {
			_, err := c.Kube().CoreV1().ServiceAccounts(n.name).Patch(context.TODO(),
				"default",
				types.JSONPatchType,
				[]byte(`[{"op": "add", "path": "/imagePullSecrets", "value": [{"name": "test-gcr-secret"}]}]`),
				metav1.PatchOptions{})
			return err
		}, retry.Delay(1*time.Second), retry.Timeout(10*time.Second))
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *kubeNamespace) forEachCluster(fn func(i int, c cluster.Cluster) error) error {
	errG := multierror.Group{}
	for i, c := range n.ctx.AllClusters() {
		errG.Go(func() error {
			return fn(i, c)
		})
	}
	return errG.Wait().ErrorOrNil()
}

func (n *kubeNamespace) addCleanup(fn func() error) {
	n.cleanupMutex.Lock()
	defer n.cleanupMutex.Unlock()
	n.cleanupFuncs = append(n.cleanupFuncs, fn)
}

func (n *kubeNamespace) IsAmbient() bool {
	// TODO cache labels and invalidate on SetLabel to avoid a ton of kube calls
	labels, err := n.Labels()
	if err != nil {
		scopes.Framework.Warnf("failed getting labels for namespace %s, assuming ambient is on", n.name)
	}
	return err != nil || labels["istio.io/dataplane-mode"] == "ambient"
}

func (n *kubeNamespace) IsInjected() bool {
	if n == nil {
		return false
	}
	// TODO cache labels and invalidate on SetLabel to avoid a ton of kube calls
	labels, err := n.Labels()
	if err != nil {
		scopes.Framework.Warnf("failed getting labels for namespace %s, assuming injection is on", n.name)
		return true
	}
	_, hasRevision := labels[label.IoIstioRev.Name]
	return hasRevision || labels["istio-injection"] == "enabled"
}

// createNamespaceLabels will take a namespace config and generate the proper k8s labels
func createNamespaceLabels(ctx resource.Context, cfg Config) map[string]string {
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
		if cfg.Revision != "" {
			l[label.IoIstioRev.Name] = cfg.Revision
			l["istio-injection"] = "disabled"
		}
	}

	// bring over supplied labels
	for k, v := range cfg.Labels {
		l[k] = v
	}
	return l
}

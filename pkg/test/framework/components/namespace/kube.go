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

	kubeApiCore "k8s.io/api/core/v1"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/label"
	"istio.io/istio/pkg/test/framework/image"
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

func (n *kubeNamespace) Dump(ctx resource.Context) {
	scopes.Framework.Errorf("=== Dumping Namespace %s State...", n.name)

	d, err := ctx.CreateTmpDirectory(n.name + "-state")
	if err != nil {
		scopes.Framework.Errorf("Unable to create directory for dumping %s contents: %v", n.name, err)
		return
	}

	kube2.DumpPods(n.ctx, d, n.name)
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
	if n.name != "" {
		scopes.Framework.Debugf("%s deleting namespace", n.id)
		ns := n.name
		n.name = ""

		for _, c := range n.ctx.Clusters().Kube() {
			err = c.CoreV1().Namespaces().Delete(context.TODO(), ns, kube2.DeleteOptionsForeground())
		}
	}

	scopes.Framework.Debugf("%s close complete (err:%v)", n.id, err)
	return
}

func claimKube(ctx resource.Context, nsConfig *Config) (Instance, error) {
	for _, cluster := range ctx.Clusters().Kube() {
		if !kube2.NamespaceExists(cluster, nsConfig.Prefix) {
			if _, err := cluster.CoreV1().Namespaces().Create(context.TODO(), &kubeApiCore.Namespace{
				ObjectMeta: kubeApiMeta.ObjectMeta{
					Name:   nsConfig.Prefix,
					Labels: createNamespaceLabels(ctx, nsConfig),
				},
			}, kubeApiMeta.CreateOptions{}); err != nil {
				return nil, err
			}
		}
	}
	return &kubeNamespace{name: nsConfig.Prefix}, nil
}

// setNamespaceLabel labels a namespace with the given key, value pair
func (n *kubeNamespace) setNamespaceLabel(key, value string) error {
	// need to convert '/' to '~1' as per the JSON patch spec http://jsonpatch.com/#operations
	jsonPatchEscapedKey := strings.ReplaceAll(key, "/", "~1")
	for _, cluster := range n.ctx.Clusters().Kube() {
		nsLabelPatch := fmt.Sprintf(`[{"op":"replace","path":"/metadata/labels/%s","value":"%s"}]`, jsonPatchEscapedKey, value)
		if _, err := cluster.CoreV1().Namespaces().Patch(context.TODO(), n.name, types.JSONPatchType, []byte(nsLabelPatch), kubeApiMeta.PatchOptions{}); err != nil {
			return err
		}
	}

	return nil
}

// removeNamespaceLabel removes namespace label with the given key
func (n *kubeNamespace) removeNamespaceLabel(key string) error {
	// need to convert '/' to '~1' as per the JSON patch spec http://jsonpatch.com/#operations
	jsonPatchEscapedKey := strings.ReplaceAll(key, "/", "~1")
	for _, cluster := range n.ctx.Clusters().Kube() {
		nsLabelPatch := fmt.Sprintf(`[{"op":"remove","path":"/metadata/labels/%s"}]`, jsonPatchEscapedKey)
		if _, err := cluster.CoreV1().Namespaces().Patch(context.TODO(), n.name, types.JSONPatchType, []byte(nsLabelPatch), kubeApiMeta.PatchOptions{}); err != nil {
			return err
		}
	}

	return nil
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

	for _, cluster := range n.ctx.Clusters().Kube() {
		if _, err := cluster.CoreV1().Namespaces().Create(context.TODO(), &kubeApiCore.Namespace{
			ObjectMeta: kubeApiMeta.ObjectMeta{
				Name:   ns,
				Labels: createNamespaceLabels(ctx, nsConfig),
			},
		}, kubeApiMeta.CreateOptions{}); err != nil {
			return nil, err
		}
		settings, err := image.SettingsFromCommandLine()
		if err != nil {
			return nil, err
		}
		if settings.ImagePullSecret != "" {
			if err := cluster.ApplyYAMLFiles(n.name, settings.ImagePullSecret); err != nil {
				return nil, err
			}
		}
	}

	return n, nil
}

// createNamespaceLabels will take a namespace config and generate the proper k8s labels
func createNamespaceLabels(ctx resource.Context, cfg *Config) map[string]string {
	l := make(map[string]string)
	l["istio-testing"] = "istio-test"
	if cfg.Inject {
		// do not add namespace labels when dealing with multiple revisions since
		// this disables the necessary object selectors
		if !ctx.Settings().IstioVersions.IsMultiVersion() {
			if cfg.Revision != "" {
				l[label.IoIstioRev.Name] = cfg.Revision
			} else {
				l["istio-injection"] = "enabled"
			}
		}
	} else {
		// for multiversion environments, disable the entire namespace explicitly
		// so that object selectors are ignored
		if ctx.Settings().IstioVersions.IsMultiVersion() {
			l["istio-injection"] = "disabled"
		}
	}

	// bring over supplied labels
	for k, v := range cfg.Labels {
		l[k] = v
	}
	return l
}

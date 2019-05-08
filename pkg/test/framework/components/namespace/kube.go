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
	"math/rand"
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
}

var _ Instance = &kubeNamespace{}
var _ io.Closer = &kubeNamespace{}
var _ resource.Resource = &kubeNamespace{}

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
		if err := env.CreateNamespaceWithInjectionEnabled(name, "istio-test", cfg.ConfigNamespace); err != nil {
			return nil, err
		}

	}
	return &kubeNamespace{name: name}, nil
}

// NewNamespace allocates a new testing namespace.
func newKube(ctx resource.Context, prefix string, inject bool) (Instance, error) {
	mu.Lock()
	idctr++
	nsid := idctr
	r := rnd.Intn(99999)
	mu.Unlock()

	env := ctx.Environment().(*kube.Environment)
	ns := fmt.Sprintf("%s-%d-%d", prefix, nsid, r)

	if inject {
		cfg, err := istio.DefaultConfig(ctx)
		if err != nil {
			return nil, err
		}
		err = env.CreateNamespaceWithInjectionEnabled(ns, "istio-test", cfg.ConfigNamespace)
		if err != nil {
			return nil, err
		}
	} else {
		err := env.CreateNamespace(ns, "istio-test")
		if err != nil {
			return nil, err
		}
	}

	n := &kubeNamespace{name: ns, a: env.Accessor}
	id := ctx.TrackResource(n)
	n.id = id

	return n, nil
}

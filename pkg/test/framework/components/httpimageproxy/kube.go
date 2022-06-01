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

package httpimageproxy

import (
	"fmt"
	"io"
	"net"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
)

const (
	service = "httpimageproxy"
	ns      = "httpimageproxy"
)

var (
	_ Instance  = &kubeComponent{}
	_ io.Closer = &kubeComponent{}
)

type kubeComponent struct {
	id      resource.ID
	ns      namespace.Instance
	cluster cluster.Cluster
	address string
}

func newKube(ctx resource.Context, cfg Config) (Instance, error) {
	c := &kubeComponent{
		cluster: ctx.Clusters().GetOrDefault(cfg.Cluster),
	}
	c.id = ctx.TrackResource(c)
	var err error
	scopes.Framework.Info("=== BEGIN: Deploy httpimageproxy ===")
	defer func() {
		if err != nil {
			err = fmt.Errorf("httpimageproxy deployment failed: %v", err)
			scopes.Framework.Infof("=== FAILED: Deploy httpimageproxy ===")
			_ = c.Close()
		} else {
			scopes.Framework.Info("=== SUCCEEDED: Deploy httpimageproxy ===")
		}
	}()

	c.ns, err = namespace.New(ctx, namespace.Config{
		Prefix: ns,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create %q namespace for httpimageproxy install; err: %v", ns, err)
	}

	args := map[string]interface{}{}

	if len(cfg.Image) != 0 {
		args["Image"] = cfg.Image
	}

	// apply YAML
	if err := ctx.ConfigKube(c.cluster).EvalFile(c.ns.Name(), args, env.HTTPImageProxyInstallFilePath).Apply(); err != nil {
		return nil, fmt.Errorf("failed to apply rendered %s, err: %v", env.HTTPImageProxyInstallFilePath, err)
	}

	if _, _, err = testKube.WaitUntilServiceEndpointsAreReady(c.cluster.Kube(), c.ns.Name(), service); err != nil {
		scopes.Framework.Infof("Error waiting for httpimageproxy to be available: %v", err)
		return nil, err
	}

	c.address = net.JoinHostPort(fmt.Sprintf("%s.%s", service, c.ns.Name()), "8080")
	scopes.Framework.Infof("httpimageproxy in-cluster address: %s", c.address)

	return c, nil
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

// Close implements io.Closer.
func (c *kubeComponent) Close() error {
	return nil
}

func (c *kubeComponent) Address() string {
	return c.address
}

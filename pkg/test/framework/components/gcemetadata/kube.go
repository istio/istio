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

package gcemetadata

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	environ "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
)

const (
	ns   = "gce-metadata"
	port = 8080
)

var (
	_ Instance  = &kubeComponent{}
	_ io.Closer = &kubeComponent{}
)

type kubeComponent struct {
	id        resource.ID
	ns        namespace.Instance
	cluster   kube.Cluster
	clusterIP string
}

func newKube(ctx resource.Context, cfg Config) (Instance, error) {
	c := &kubeComponent{
		cluster: kube.ClusterOrDefault(cfg.Cluster, ctx.Environment()),
	}
	c.id = ctx.TrackResource(c)
	var err error
	scopes.Framework.Info("=== BEGIN: Deploy GCE Metadata Server ===")
	defer func() {
		if err != nil {
			err = fmt.Errorf("gcemetadata deployment failed: %v", err) // nolint:golint
			scopes.Framework.Infof("=== FAILED: Deploy GCE Metadata Server ===")
			_ = c.Close()
		} else {
			scopes.Framework.Info("=== SUCCEEDED: Deploy GCE Metadata Server ===")
		}
	}()

	c.ns, err = namespace.New(ctx, namespace.Config{
		Prefix: ns,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create %q namespace for GCE Metadata Server install; err: %v", ns, err)
	}

	// apply YAML
	yamlContent, err := ioutil.ReadFile(environ.GCEMetadataServerInstallFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %q, err: %v", environ.GCEMetadataServerInstallFilePath, err)
	}

	if _, err := c.cluster.ApplyContents(c.ns.Name(), string(yamlContent)); err != nil {
		return nil, fmt.Errorf("failed to apply rendered %q, err: %v", environ.GCEMetadataServerInstallFilePath, err)
	}

	fetchFn := testKube.NewSinglePodFetch(c.cluster.Accessor, c.ns.Name(), "app=gce-metadata")
	_, err = c.cluster.WaitUntilPodsAreReady(fetchFn)
	if err != nil {
		return nil, err
	}

	// Now retrieve the service information to find the ClusterIP
	s, err := c.cluster.CoreV1().Services(c.ns.Name()).Get(context.TODO(), "gce-metadata-server", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	c.clusterIP = s.Spec.ClusterIP

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
	return c.clusterIP
}

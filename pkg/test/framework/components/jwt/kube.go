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

package jwt

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/yml"
)

const (
	serviceName = "jwt-server"
	httpPort    = 8000
	httpsPort   = 8443
)

var (
	_ resource.Resource = &serverImpl{}
	_ Server            = &serverImpl{}
)

func newKubeServer(ctx resource.Context, ns namespace.Instance) (server *serverImpl, err error) {
	start := time.Now()
	scopes.Framework.Info("=== BEGIN: Deploy JWT server ===")
	defer func() {
		if err != nil {
			scopes.Framework.Error("=== FAILED: Deploy JWT server ===")
			scopes.Framework.Error(err)
		} else {
			scopes.Framework.Infof("=== SUCCEEDED: Deploy JWT server in %v ===", time.Since(start))
		}
	}()

	// Create the namespace, if unspecified.
	if ns == nil {
		ns, err = namespace.New(ctx, namespace.Config{
			Prefix: "jwt",
			Inject: true,
		})
		if err != nil {
			return
		}
	}

	server = &serverImpl{
		ns: ns,
	}
	server.id = ctx.TrackResource(server)

	// Deploy the server.
	if err = server.deploy(ctx); err != nil {
		return
	}

	return
}

func readDeploymentYAML() (string, error) {
	// Read the samples file.
	filePath := filepath.Join(env.IstioSrc, "samples/jwt-server", "jwt-server.yaml")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	yamlText := string(data)

	return yamlText, nil
}

func (s *serverImpl) deploy(ctx resource.Context) error {
	yamlText, err := readDeploymentYAML()
	if err != nil {
		return err
	}

	image := ctx.Settings().Image
	if image.PullSecret != "" {
		var imageSpec resource.ImageSettings
		imageSpec.PullSecret = image.PullSecret
		secretName, err := imageSpec.PullSecretName()
		if err != nil {
			return err
		}
		yamlText, err = addPullSecret(yamlText, secretName)
		if err != nil {
			return err
		}
	}

	if err := ctx.ConfigKube(ctx.Clusters()...).
		YAML(s.ns.Name(), yamlText).
		Apply(apply.CleanupConditionally); err != nil {
		return err
	}

	// Wait for the endpoints to be ready.
	var g multierror.Group
	for _, c := range ctx.Clusters() {
		c := c
		g.Go(func() error {
			fetchFn := kube.NewPodFetch(c, s.ns.Name(), "app=jwt-server")
			_, err := kube.WaitUntilPodsAreReady(fetchFn)
			if err != nil {
				return fmt.Errorf("jwt-server pod not ready in cluster %s: %v", c.Name(), err)
			}

			_, _, err = kube.WaitUntilServiceEndpointsAreReady(c.Kube(), s.ns.Name(), "jwt-server")
			return err
		})
	}

	return g.Wait().ErrorOrNil()
}

func addPullSecret(resource string, pullSecret string) (string, error) {
	res := yml.SplitString(resource)
	updatedYaml, err := yml.ApplyPullSecret(res[2], pullSecret)
	if err != nil {
		return "", err
	}
	mergedYaml := yml.JoinString(res[0], res[1], updatedYaml)
	return mergedYaml, nil
}

type serverImpl struct {
	id resource.ID
	ns namespace.Instance
}

func (s *serverImpl) FQDN() string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, s.ns.Name())
}

func (s *serverImpl) HTTPPort() int {
	return httpPort
}

func (s *serverImpl) HTTPSPort() int {
	return httpsPort
}

func (s *serverImpl) JwksURI() string {
	uri := fmt.Sprintf("http://%s:%d/jwks", s.FQDN(), s.HTTPPort())
	return uri
}

func (s *serverImpl) ID() resource.ID {
	return s.id
}

func (s *serverImpl) Namespace() namespace.Instance {
	return s.ns
}

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

package endpointpicker

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	defaultServiceName = "endpoint-picker"
	defaultPort        = 9002
)

var _ Instance = &kubeComponent{}

type kubeComponent struct {
	id          resource.ID
	ns          namespace.Instance
	serviceName string
	port        int
}

func newKube(ctx resource.Context, cfg Config) (Instance, error) {
	// Set defaults
	if cfg.ServiceName == "" {
		cfg.ServiceName = defaultServiceName
	}
	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}

	// Create namespace if not provided
	ns := cfg.Namespace
	if ns == nil {
		var err error
		ns, err = namespace.New(ctx, namespace.Config{
			Prefix: "endpoint-picker",
			Inject: false, // EPP doesn't need sidecar injection
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create namespace: %v", err)
		}
	}

	c := &kubeComponent{
		ns:          ns,
		serviceName: cfg.ServiceName,
		port:        cfg.Port,
	}
	c.id = ctx.TrackResource(c)

	// Determine image to use
	image := cfg.Image
	if image == "" {
		image = fmt.Sprintf("%s/endpoint-picker:%s", ctx.Settings().Image.Hub, ctx.Settings().Image.Tag)
	}

	// Deploy the endpoint picker
	if err := c.deploy(ctx, image); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *kubeComponent) deploy(ctx resource.Context, image string) error {
	// Create the deployment and service manifests
	manifest := fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
spec:
  ports:
  - name: grpc
    port: %d
    protocol: TCP
    targetPort: %d
  selector:
    app: %s
  type: ClusterIP
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
spec:
  replicas: 1
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      labels:
        app: %s
      annotations:
        sidecar.istio.io/inject: "false"
    spec:
      containers:
      - name: endpoint-picker
        image: %s
        imagePullPolicy: IfNotPresent
        ports:
        - containerPort: %d
`, c.serviceName, c.ns.Name(),
		c.port, c.port, c.serviceName,
		c.serviceName, c.ns.Name(),
		c.serviceName,
		c.serviceName,
		image,
		c.port)

	// Apply the manifest
	if err := ctx.ConfigIstio().YAML(c.ns.Name(), manifest).Apply(); err != nil {
		return fmt.Errorf("failed to deploy endpoint picker: %v", err)
	}

	// Wait for the deployment to be ready
	if err := c.waitForReady(ctx); err != nil {
		return fmt.Errorf("endpoint picker failed to become ready: %v", err)
	}

	return nil
}

func (c *kubeComponent) waitForReady(ctx resource.Context) error {
	client := ctx.Clusters().Default().Kube()

	return retry.UntilSuccess(func() error {
		// Check if deployment is ready
		deploy, err := client.AppsV1().Deployments(c.ns.Name()).Get(context.TODO(), c.serviceName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get deployment: %v", err)
		}

		if deploy.Status.ReadyReplicas < 1 {
			return fmt.Errorf("waiting for deployment to have ready replicas, current: %d", deploy.Status.ReadyReplicas)
		}

		// Check if service exists
		_, err = client.CoreV1().Services(c.ns.Name()).Get(context.TODO(), c.serviceName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get service: %v", err)
		}

		return nil
	}, retry.Timeout(2*time.Minute), retry.Delay(2*time.Second))
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

func (c *kubeComponent) Namespace() namespace.Instance {
	return c.ns
}

func (c *kubeComponent) ServiceName() string {
	return c.serviceName
}

func (c *kubeComponent) Port() int {
	return c.port
}

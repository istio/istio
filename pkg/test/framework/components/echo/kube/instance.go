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

package kube

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/hashicorp/go-multierror"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	appEcho "istio.io/istio/pkg/test/echo/client"
	echoCommon "istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"

	kubeCore "k8s.io/api/core/v1"
)

const (
	tcpHealthPort     = 3333
	httpReadinessPort = 8080
	defaultDomain     = constants.DefaultKubernetesDomain
)

var (
	_ echo.Instance = &instance{}
	_ io.Closer     = &instance{}
)

type instance struct {
	id        resource.ID
	cfg       echo.Config
	clusterIP string
	workloads []*workload
	grpcPort  uint16
	ctx       resource.Context
	tls       *echoCommon.TLSSettings
	cluster   resource.Cluster
}

func newInstance(ctx resource.Context, cfg echo.Config) (out *instance, err error) {
	// Fill in defaults for any missing values.
	common.AddPortIfMissing(&cfg, protocol.GRPC)
	if err = common.FillInDefaults(ctx, defaultDomain, &cfg); err != nil {
		return nil, err
	}

	c := &instance{
		cfg:     cfg,
		ctx:     ctx,
		cluster: cfg.Cluster,
	}
	c.id = ctx.TrackResource(c)

	// Save the GRPC port.
	grpcPort := common.GetPortForProtocol(&cfg, protocol.GRPC)
	if grpcPort == nil {
		return nil, errors.New("unable fo find GRPC command port")
	}
	c.grpcPort = uint16(grpcPort.InstancePort)
	if grpcPort.TLS {
		c.tls = cfg.TLSSettings
	}

	// Generate the service and deployment YAML.
	serviceYAML, deploymentYAML, err := generateYAML(cfg, c.cluster)
	if err != nil {
		return nil, fmt.Errorf("generate yaml: %v", err)
	}

	// Apply the service definition to all clusters.
	if err := ctx.Config().ApplyYAML(cfg.Namespace.Name(), serviceYAML); err != nil {
		return nil, fmt.Errorf("failed deploying echo service %s to clusters: %v",
			cfg.FQDN(), err)
	}

	// Deploy the YAML.
	if err = ctx.Config(c.cluster).ApplyYAML(cfg.Namespace.Name(), deploymentYAML); err != nil {
		return nil, fmt.Errorf("failed deploying echo %s to cluster %s: %v",
			cfg.FQDN(), c.cluster.Name(), err)
	}

	if cfg.DeployAsVM {
		serviceAccount := cfg.Service
		if !cfg.ServiceAccount {
			serviceAccount = "default"
		}
		token, err := createServiceAccountToken(c.cluster, cfg.Namespace.Name(), serviceAccount)
		if err != nil {
			return nil, err
		}
		if _, err := c.cluster.CoreV1().Secrets(cfg.Namespace.Name()).Create(context.TODO(), &kubeCore.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cfg.Service + "-istio-token",
				Namespace: cfg.Namespace.Name(),
			},
			Data: map[string][]byte{
				"istio-token": []byte(token),
			},
		}, metav1.CreateOptions{}); err != nil {
			return nil, err
		}
	}

	if cfg.DeployAsVM {
		var pods *kubeCore.PodList
		if err := retry.UntilSuccess(func() error {
			pods, err = c.cluster.PodsForSelector(context.TODO(), cfg.Namespace.Name(),
				fmt.Sprintf("istio.io/test-vm=%s", cfg.Service))
			if err != nil {
				return err
			}
			if len(pods.Items) == 0 {
				return fmt.Errorf("0 pods found for istio.io/test-vm:%s", cfg.Service)
			}
			for _, vmPod := range pods.Items {
				if vmPod.Status.PodIP == "" {
					return fmt.Errorf("empty pod ip for pod %v", vmPod.Name)
				}
			}
			return nil
		}, retry.Timeout(cfg.ReadinessTimeout)); err != nil {
			return nil, err
		}
		serviceAccount := cfg.Service
		if !cfg.ServiceAccount {
			serviceAccount = "default"
		}

		// One workload entry for each VM pod
		for _, vmPod := range pods.Items {
			wle := fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: WorkloadEntry
metadata:
  name: %s
spec:
  address: %s
  serviceAccount: %s
  labels:
    app: %s
    version: %s
`, vmPod.Name, vmPod.Status.PodIP, serviceAccount, cfg.Service, vmPod.Labels["istio.io/test-vm-version"])
			// Deploy the workload entry.
			if err = ctx.Config(c.cluster).ApplyYAML(cfg.Namespace.Name(), wle); err != nil {
				return nil, fmt.Errorf("failed deploying workload entry: %v", err)
			}
		}
	}

	// Now retrieve the service information to find the ClusterIP
	s, err := c.cluster.CoreV1().Services(cfg.Namespace.Name()).Get(context.TODO(), cfg.Service, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	c.clusterIP = s.Spec.ClusterIP
	switch c.clusterIP {
	case kubeCore.ClusterIPNone, "":
		if !cfg.Headless {
			return nil, fmt.Errorf("invalid ClusterIP %s for non-headless service %s/%s",
				c.clusterIP,
				c.cfg.Namespace.Name(),
				c.cfg.Service)
		}
		c.clusterIP = ""
	}

	return c, nil
}

func createServiceAccountToken(client kubernetes.Interface, ns string, serviceAccount string) (string, error) {
	scopes.Framework.Debugf("Creating service account token for: %s/%s", ns, serviceAccount)

	token, err := client.CoreV1().ServiceAccounts(ns).CreateToken(context.TODO(), serviceAccount,
		&authenticationv1.TokenRequest{
			Spec: authenticationv1.TokenRequestSpec{
				Audiences: []string{"istio-ca"},
			},
		}, metav1.CreateOptions{})

	if err != nil {
		return "", err
	}
	return token.Status.Token, nil
}

// getContainerPorts converts the ports to a port list of container ports.
// Adds ports for health/readiness if necessary.
func getContainerPorts(ports []echo.Port) echoCommon.PortList {
	containerPorts := make(echoCommon.PortList, 0, len(ports))
	var healthPort *echoCommon.Port
	var readyPort *echoCommon.Port
	for _, p := range ports {
		// Add the port to the set of application ports.
		cport := &echoCommon.Port{
			Name:     p.Name,
			Protocol: p.Protocol,
			Port:     p.InstancePort,
			TLS:      p.TLS,
		}
		containerPorts = append(containerPorts, cport)

		switch p.Protocol {
		case protocol.GRPC:
			continue
		case protocol.HTTP:
			if p.InstancePort == httpReadinessPort {
				readyPort = cport
			}
		default:
			if p.InstancePort == tcpHealthPort {
				healthPort = cport
			}
		}
	}

	// If we haven't added the readiness/health ports, do so now.
	if readyPort == nil {
		containerPorts = append(containerPorts, &echoCommon.Port{
			Name:     "http-readiness-port",
			Protocol: protocol.HTTP,
			Port:     httpReadinessPort,
		})
	}
	if healthPort == nil {
		containerPorts = append(containerPorts, &echoCommon.Port{
			Name:     "tcp-health-port",
			Protocol: protocol.HTTP,
			Port:     tcpHealthPort,
		})
	}
	return containerPorts
}

func (c *instance) ID() resource.ID {
	return c.id
}

func (c *instance) Address() string {
	return c.clusterIP
}

func (c *instance) Workloads() ([]echo.Workload, error) {
	out := make([]echo.Workload, 0, len(c.workloads))
	for _, w := range c.workloads {
		out = append(out, w)
	}
	return out, nil
}

func (c *instance) WorkloadsOrFail(t test.Failer) []echo.Workload {
	t.Helper()
	out, err := c.Workloads()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (c *instance) WaitUntilCallable(instances ...echo.Instance) error {
	// Wait for the outbound config to be received by each workload from Pilot.
	for _, w := range c.workloads {
		if w.sidecar != nil {
			if err := w.sidecar.WaitForConfig(common.OutboundConfigAcceptFunc(c, instances...)); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *instance) WaitUntilCallableOrFail(t test.Failer, instances ...echo.Instance) {
	t.Helper()
	if err := c.WaitUntilCallable(instances...); err != nil {
		t.Fatal(err)
	}
}

// WorkloadHasSidecar returns true if the input endpoint is deployed with sidecar injected based on the config.
func workloadHasSidecar(cfg echo.Config, podName string) bool {
	// Match workload first.
	for _, w := range cfg.Subsets {
		if strings.HasPrefix(podName, fmt.Sprintf("%v-%v", cfg.Service, w.Version)) {
			return w.Annotations.GetBool(echo.SidecarInject)
		}
	}
	return true
}

func (c *instance) initialize(pods []kubeCore.Pod) error {
	if c.workloads != nil {
		// Already ready.
		return nil
	}

	workloads := make([]*workload, 0)
	for _, pod := range pods {
		workload, err := newWorkload(pod, workloadHasSidecar(c.cfg, pod.Name), c.grpcPort, c.cluster, c.tls, c.ctx)
		if err != nil {
			return err
		}
		workloads = append(workloads, workload)
	}

	if len(workloads) == 0 {
		return fmt.Errorf("no workloads found for service %s/%s/%s, from %v pods", c.cfg.Namespace.Name(), c.cfg.Service, c.cfg.Version, len(pods))
	}

	c.workloads = workloads
	return nil
}

func (c *instance) Close() (err error) {
	for _, w := range c.workloads {
		err = multierror.Append(err, w.Close()).ErrorOrNil()
	}
	c.workloads = nil
	return
}

func (c *instance) Config() echo.Config {
	return c.cfg
}

func (c *instance) Call(opts echo.CallOptions) (appEcho.ParsedResponses, error) {
	out, err := common.CallEcho(c.workloads[0].Instance, &opts, common.IdentityOutboundPortSelector)
	if err != nil {
		if opts.Port != nil {
			err = fmt.Errorf("failed calling %s->'%s://%s:%d/%s': %v",
				c.Config().Service,
				strings.ToLower(string(opts.Port.Protocol)),
				opts.Host,
				opts.Port.ServicePort,
				opts.Path,
				err)
		}
		return nil, err
	}
	return out, nil
}

func (c *instance) CallOrFail(t test.Failer, opts echo.CallOptions) appEcho.ParsedResponses {
	t.Helper()
	r, err := c.Call(opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

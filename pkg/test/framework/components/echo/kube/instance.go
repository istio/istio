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
	"io/ioutil"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	kubeCore "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	appEcho "istio.io/istio/pkg/test/echo/client"
	echoCommon "istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/resource"
	kubeTest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/util/gogoprotomarshal"
)

const (
	tcpHealthPort     = 3333
	httpReadinessPort = 8080
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
	cluster   cluster.Cluster
}

func newInstance(ctx resource.Context, originalCfg echo.Config) (out *instance, err error) {
	cfg := originalCfg.DeepCopy()
	if !cfg.Cluster.IsPrimary() && cfg.DeployAsVM {
		return nil, fmt.Errorf("cannot deploy %s as VM on non-primary %s", cfg.Service, cfg.Cluster.Name())
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

	if cfg.DeployAsVM {
		if err := createVMConfig(ctx, c, cfg); err != nil {
			return nil, fmt.Errorf("failed creating config for vm: %v", err)
		}
	}

	// Generate the deployment YAML.
	deploymentYAML, err := generateDeployment(cfg)
	if err != nil {
		return nil, fmt.Errorf("generate yaml: %v", err)
	}

	// Apply the deployment to the configured cluster.
	if err = ctx.Config(c.cluster).ApplyYAML(cfg.Namespace.Name(), deploymentYAML); err != nil {
		return nil, fmt.Errorf("failed deploying echo %s to cluster %s: %v",
			cfg.FQDN(), c.cluster.Name(), err)
	}

	if cfg.DeployAsVM {
		if err := registerVMs(ctx, c, cfg); err != nil {
			return nil, err
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

// createVMConfig sets up a Service account,
func createVMConfig(ctx resource.Context, c *instance, cfg echo.Config) error {
	serviceAccount := cfg.Service
	if !cfg.ServiceAccount {
		serviceAccount = "default"
	}
	istioCtl, err := istioctl.New(ctx, istioctl.Config{Cluster: cfg.Cluster})
	if err != nil {
		return err
	}
	// generate config files for VM bootstrap
	dirname := fmt.Sprintf("%s-vm-config-", cfg.Service)
	dir, err := ctx.CreateDirectory(dirname)
	if err != nil {
		return err
	}

	wg := tmpl.MustEvaluate(`
apiVersion: networking.istio.io/v1alpha3
kind: WorkloadGroup
metadata:
  name: {{.name}}
  namespace: {{.namespace}}
spec:
  metadata:
    labels:
      app: {{.name}}
  template:
    serviceAccount: {{.serviceaccount}}
    network: "{{.network}}"
  probe:
    failureThreshold: 5
    httpGet:
      path: /
      port: 8080
    periodSeconds: 2
    successThreshold: 1
    timeoutSeconds: 2

`, map[string]string{
		"name":           cfg.Service,
		"namespace":      cfg.Namespace.Name(),
		"serviceaccount": serviceAccount,
		"network":        cfg.Cluster.NetworkName(),
	})

	// Push the WorkloadGroup for auto-registration
	if cfg.AutoRegisterVM {
		if err := ctx.Config(cfg.Cluster).ApplyYAML(cfg.Namespace.Name(), wg); err != nil {
			return err
		}
	}

	if cfg.ServiceAccount {
		// create service account, the next workload command will use it to generate a token
		err = createServiceAccount(cfg.Cluster, cfg.Namespace.Name(), serviceAccount)
		if err != nil && !kerrors.IsAlreadyExists(err) {
			return err
		}
	}

	if err := ioutil.WriteFile(path.Join(dir, "workloadgroup.yaml"), []byte(wg), 0o600); err != nil {
		return err
	}

	ist, err := istio.Get(ctx)
	if err != nil {
		return err
	}
	// this will wait until the eastwest gateway has an IP before running the next command
	istiodAddr, err := ist.RemoteDiscoveryAddressFor(cfg.Cluster)
	if err != nil {
		return err
	}

	var subsetDir string
	for _, subset := range cfg.Subsets {
		subsetDir, err = ioutil.TempDir(dir, subset.Version+"-")
		if err != nil {
			return err
		}
		cmd := []string{
			"x", "workload", "entry", "configure",
			"-f", path.Join(dir, "workloadgroup.yaml"),
			"-o", subsetDir,
		}
		if ctx.Clusters().IsMulticluster() {
			// When VMs talk about "cluster", they refer to the cluster they connect to for discovery
			cmd = append(cmd, "--clusterID", c.cluster.Name())
		}
		if cfg.AutoRegisterVM {
			cmd = append(cmd, "--autoregister")
		}
		if !ctx.Environment().(*kube.Environment).Settings().LoadBalancerSupported {
			// LoadBalancer may not be suppported and the command doesn't have NodePort fallback logic that the tests do
			cmd = append(cmd, "--ingressIP", istiodAddr.IP.String())
		}
		// make sure namespace controller has time to create root-cert ConfigMap
		if err := retry.UntilSuccess(func() error {
			_, _, err = istioCtl.Invoke(cmd)
			return err
		}, retry.Timeout(20*time.Second)); err != nil {
			return err
		}

		// support proxyConfig customizations on VMs via annotation in the echo API.
		for k, v := range subset.Annotations {
			if k.Name == "proxy.istio.io/config" {
				if err := patchProxyConfigFile(path.Join(subsetDir, "mesh.yaml"), v.Value); err != nil {
					return err
				}
			}
		}

		if err := customizeVMEnvironment(ctx, cfg, path.Join(subsetDir, "cluster.env"), istiodAddr); err != nil {
			return err
		}

		// push boostrap config as a ConfigMap so we can mount it on our "vm" pods
		cmData := map[string][]byte{}
		for _, file := range []string{"cluster.env", "mesh.yaml", "root-cert.pem", "hosts"} {
			cmData[file], err = ioutil.ReadFile(path.Join(subsetDir, file))
			if err != nil {
				return err
			}
		}
		cmName := fmt.Sprintf("%s-%s-vm-bootstrap", cfg.Service, subset.Version)
		cm := &kubeCore.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: cmName}, BinaryData: cmData}
		_, err = c.cluster.CoreV1().ConfigMaps(cfg.Namespace.Name()).Create(context.TODO(), cm, metav1.CreateOptions{})
		if err != nil && !kerrors.IsAlreadyExists(err) {
			return err
		}
	}

	// push the generated token as a Secret (only need one, they should be identical)
	token, err := ioutil.ReadFile(path.Join(subsetDir, "istio-token"))
	if err != nil {
		return err
	}
	secret := &kubeCore.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.Service + "-istio-token",
			Namespace: cfg.Namespace.Name(),
		},
		Data: map[string][]byte{
			"istio-token": token,
		},
	}
	if _, err := c.cluster.CoreV1().Secrets(cfg.Namespace.Name()).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
		if kerrors.IsAlreadyExists(err) {
			if _, err := c.cluster.CoreV1().Secrets(cfg.Namespace.Name()).Update(context.TODO(), secret, metav1.UpdateOptions{}); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	return nil
}

func customizeVMEnvironment(ctx resource.Context, cfg echo.Config, clusterEnv string, istiodAddr net.TCPAddr) (err error) {
	f, err := os.OpenFile(clusterEnv, os.O_APPEND|os.O_WRONLY, os.ModeAppend)
	defer func() {
		if closeErr := f.Close(); err != nil {
			err = closeErr
		}
	}()
	if cfg.VMEnvironment != nil {
		for k, v := range cfg.VMEnvironment {
			_, err = f.Write([]byte(fmt.Sprintf("%s=%s\n", k, v)))
			if err != nil {
				return err
			}
		}
	}
	if !ctx.Environment().(*kube.Environment).Settings().LoadBalancerSupported {
		// customize cluster.env with NodePort mapping
		if err != nil {
			return err
		}
		_, err = f.Write([]byte(fmt.Sprintf("ISTIO_PILOT_PORT=%d\n", istiodAddr.Port)))
		if err != nil {
			return err
		}
	}
	return
}

func patchProxyConfigFile(file string, overrides string) error {
	config, err := readMeshConfig(file)
	if err != nil {
		return err
	}
	overrideYAML := "defaultConfig:\n"
	overrideYAML += istio.Indent(overrides, "  ")
	if err := gogoprotomarshal.ApplyYAML(overrideYAML, config.DefaultConfig); err != nil {
		return err
	}
	outYAML, err := gogoprotomarshal.ToYAML(config)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(file, []byte(outYAML), 0o744)
}

func readMeshConfig(file string) (*meshconfig.MeshConfig, error) {
	baseYAML, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, err
	}
	config := &meshconfig.MeshConfig{}
	if err := gogoprotomarshal.ApplyYAML(string(baseYAML), config); err != nil {
		return nil, err
	}
	return config, nil
}

// registerVMs creates a WorkloadEntry for each "vm" pod similar to manual VM registration
func registerVMs(ctx resource.Context, c *instance, cfg echo.Config) error {
	if cfg.AutoRegisterVM {
		return nil
	}

	serviceAccount := cfg.Service
	if !cfg.ServiceAccount {
		serviceAccount = "default"
	}

	var pods *kubeCore.PodList
	if err := retry.UntilSuccess(func() error {
		var err error
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
		return err
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
  network: %q
  labels:
    app: %s
    version: %s
`, vmPod.Name, vmPod.Status.PodIP, serviceAccount, cfg.Cluster.NetworkName(), cfg.Service, vmPod.Labels["istio.io/test-vm-version"])
		// Deploy the workload entry to the primary cluster. We will read WorkloadEntry across clusters.
		if err := ctx.Config(cfg.Cluster.Primary()).ApplyYAML(cfg.Namespace.Name(), wle); err != nil {
			return err
		}
	}

	return nil
}

func createServiceAccount(client kubernetes.Interface, ns string, serviceAccount string) error {
	scopes.Framework.Debugf("Creating service account for: %s/%s", ns, serviceAccount)
	_, err := client.CoreV1().ServiceAccounts(ns).Create(context.TODO(), &kubeCore.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: serviceAccount},
	}, metav1.CreateOptions{})
	return err
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
			Name:        p.Name,
			Protocol:    p.Protocol,
			Port:        p.InstancePort,
			TLS:         p.TLS,
			ServerFirst: p.ServerFirst,
			InstanceIP:  p.InstanceIP,
			LocalhostIP: p.LocalhostIP,
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
	out, err := common.ForwardEcho(c.cfg.Service, c.workloads[0].Instance, &opts, false)
	if err != nil {
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

func (c *instance) CallWithRetry(opts echo.CallOptions,
	retryOptions ...retry.Option) (appEcho.ParsedResponses, error) {
	out, err := common.ForwardEcho(c.cfg.Service, c.workloads[0].Instance, &opts, true, retryOptions...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *instance) CallWithRetryOrFail(t test.Failer, opts echo.CallOptions,
	retryOptions ...retry.Option) appEcho.ParsedResponses {
	t.Helper()
	r, err := c.CallWithRetry(opts, retryOptions...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (c *instance) Restart() error {
	if err := c.Close(); err != nil {
		return err
	}

	// fetch the original pods so we can know when the rollout is complete
	fetch := kubeTest.NewPodMustFetch(c.cluster, c.Config().Namespace.Name(), fmt.Sprintf("%s=%s", "app", c.Config().Service))
	origPods, err := fetch()
	if err != nil {
		return err
	}
	if err := c.restartEchoDeployments(); err != nil {
		return err
	}

	// wait until we have the same number of pods as before in ready state
	var pods []kubeCore.Pod
	err = retry.UntilSuccess(func() error {
		pods, err = kubeTest.CheckPodsAreReady(fetch)
		if err != nil {
			return err
		}
		if len(pods) != len(origPods) {
			return fmt.Errorf("expected %d pods, got %d", len(origPods), len(pods))
		}
		return nil
	}, retry.Delay(time.Second*2), retry.Timeout(c.Config().ReadinessTimeout))
	if err != nil {
		return fmt.Errorf("failed waiting for rollout pods ready")
	}

	// nil out the workloads and reinitialize
	c.workloads = nil
	if err := c.initialize(pods); err != nil {
		return err
	}
	return nil
}

//restartEchoDeployments performs a `kubectl rollout restart` on the echo deployment and waits for
// `kubectl rollout status` to complete before returning.
func (c *instance) restartEchoDeployments() error {
	var errs error
	var echoDeployments []string
	for _, s := range c.cfg.Subsets {
		// TODO(Monkeyanator) move to common place so doesn't fall out of sync with templates
		echoDeployments = append(echoDeployments, fmt.Sprintf("%s-%s", c.cfg.Service, s.Version))
	}
	for _, d := range echoDeployments {
		rolloutCmd := fmt.Sprintf("kubectl rollout restart deployment/%s -n %s", d, c.cfg.Namespace.Name())
		_, err := shell.Execute(true, rolloutCmd)
		errs = multierror.Append(errs, err).ErrorOrNil()
		if err != nil {
			continue
		}
		waitCmd := fmt.Sprintf("kubectl rollout status deployment/%s -n %s", d, c.cfg.Namespace.Name())
		_, err = shell.Execute(true, waitCmd)
		errs = multierror.Append(errs, err).ErrorOrNil()
	}
	return errs
}

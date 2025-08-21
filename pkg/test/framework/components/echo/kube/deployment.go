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
	"fmt"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/hashicorp/go-multierror"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	istioctlcmd "istio.io/istio/istioctl/pkg/workload"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/log"
	echoCommon "istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/util/protomarshal"
)

const (
	// for proxyless we add a special gRPC server that doesn't get configured with xDS for test-runner use
	grpcMagicPort = 17171
	// for non-Go implementations of gRPC echo, this is the port used to forward non-gRPC requests to the Go server
	grpcFallbackPort = 17777
)

var echoKubeTemplatesDir = path.Join(env.IstioSrc, "pkg/test/framework/components/echo/kube/templates")

func getTemplate(tmplFilePath string) *template.Template {
	yamlPath := path.Join(echoKubeTemplatesDir, tmplFilePath)
	if filepath.IsAbs(tmplFilePath) {
		yamlPath = tmplFilePath
	}
	return tmpl.MustParse(file.MustAsString(yamlPath))
}

var _ workloadHandler = &deployment{}

type deployment struct {
	ctx             resource.Context
	cfg             echo.Config
	shouldCreateWLE bool
}

func newDeployment(ctx resource.Context, cfg echo.Config) (*deployment, error) {
	if !cfg.Cluster.IsConfig() && cfg.DeployAsVM {
		return nil, fmt.Errorf("cannot deploy %s/%s as VM on non-config %s",
			cfg.Namespace.Name(),
			cfg.Service,
			cfg.Cluster.Name())
	}

	if cfg.DeployAsVM {
		if err := createVMConfig(ctx, cfg); err != nil {
			return nil, fmt.Errorf("failed creating vm config for %s/%s: %v",
				cfg.Namespace.Name(),
				cfg.Service,
				err)
		}
	}

	deploymentYAML, err := GenerateDeployment(ctx, cfg, ctx.Settings())
	if err != nil {
		return nil, fmt.Errorf("failed generating echo deployment YAML for %s/%s: %v",
			cfg.Namespace.Name(),
			cfg.Service, err)
	}

	// Apply the deployment to the configured cluster.
	if err = ctx.ConfigKube(cfg.Cluster).
		YAML(cfg.Namespace.Name(), deploymentYAML).
		Apply(apply.NoCleanup); err != nil {
		return nil, fmt.Errorf("failed deploying echo %s to cluster %s: %v",
			cfg.ClusterLocalFQDN(), cfg.Cluster.Name(), err)
	}

	return &deployment{
		ctx:             ctx,
		cfg:             cfg,
		shouldCreateWLE: cfg.DeployAsVM && !cfg.AutoRegisterVM,
	}, nil
}

// Restart performs restarts of all the pod of the deployment.
// This is analogous to `kubectl rollout restart` on the echo deployment and waits for
// `kubectl rollout status` to complete before returning, but uses direct API calls.
func (d *deployment) Restart() error {
	var deploymentNames []string
	for _, s := range d.cfg.Subsets {
		// TODO(Monkeyanator) move to common place so doesn't fall out of sync with templates
		deploymentNames = append(deploymentNames, fmt.Sprintf("%s-%s", d.cfg.Service, s.Version))
	}
	curTimestamp := time.Now().Format(time.RFC3339)

	g := multierror.Group{}
	for _, deploymentName := range deploymentNames {
		g.Go(func() error {
			patchOpts := metav1.PatchOptions{}
			patchData := fmt.Sprintf(`{
			"spec": {
				"template": {
					"metadata": {
						"annotations": {
							"kubectl.kubernetes.io/restartedAt": %q
						}
					}
				}
			}
		}`, curTimestamp) // e.g., “2006-01-02T15:04:05Z07:00”
			var err error
			appsv1Client := d.cfg.Cluster.Kube().AppsV1()

			if d.cfg.IsStatefulSet() {
				_, err = appsv1Client.StatefulSets(d.cfg.Namespace.Name()).Patch(context.TODO(), deploymentName,
					types.StrategicMergePatchType, []byte(patchData), patchOpts)
			} else {
				_, err = appsv1Client.Deployments(d.cfg.Namespace.Name()).Patch(context.TODO(), deploymentName,
					types.StrategicMergePatchType, []byte(patchData), patchOpts)
			}
			if err != nil {
				return fmt.Errorf("failed to rollout restart %v/%v: %v (timestamp:%q)", d.cfg.Namespace.Name(), deploymentName, err, curTimestamp)
			}

			if err := retry.UntilSuccess(func() error {
				if d.cfg.IsStatefulSet() {
					sts, err := appsv1Client.StatefulSets(d.cfg.Namespace.Name()).Get(context.TODO(), deploymentName, metav1.GetOptions{})
					if err != nil {
						return err
					}
					if sts.Spec.Replicas == nil || !statefulsetComplete(sts) {
						return fmt.Errorf("rollout is not yet done (updated replicas:%v)", sts.Status.UpdatedReplicas)
					}
				} else {
					dep, err := appsv1Client.Deployments(d.cfg.Namespace.Name()).Get(context.TODO(), deploymentName, metav1.GetOptions{})
					if err != nil {
						return err
					}
					if dep.Spec.Replicas == nil || !deploymentComplete(dep) {
						return fmt.Errorf("rollout is not yet done (updated replicas: %v)", dep.Status.UpdatedReplicas)
					}
				}
				return nil
			}, retry.Timeout(60*time.Second), retry.Delay(2*time.Second)); err != nil {
				return fmt.Errorf("failed to wait rollout status for %v/%v: %v",
					d.cfg.Namespace.Name(), deploymentName, err)
			}
			return nil
		})
	}
	return g.Wait().ErrorOrNil()
}

func (d *deployment) WorkloadReady(w *workload) {
	if !d.shouldCreateWLE {
		return
	}

	// Deploy the workload entry to the primary cluster. We will read WorkloadEntry across clusters.
	wle := d.workloadEntryYAML(w)
	if err := d.ctx.ConfigKube(d.cfg.Cluster.Primary()).
		YAML(d.cfg.Namespace.Name(), wle).
		Apply(apply.NoCleanup); err != nil {
		log.Warnf("failed deploying echo WLE for %s/%s to primary cluster: %v",
			d.cfg.Namespace.Name(),
			d.cfg.Service,
			err)
	}
}

func (d *deployment) WorkloadNotReady(w *workload) {
	if !d.shouldCreateWLE {
		return
	}

	wle := d.workloadEntryYAML(w)
	if err := d.ctx.ConfigKube(d.cfg.Cluster.Primary()).YAML(d.cfg.Namespace.Name(), wle).Delete(); err != nil {
		log.Warnf("failed deleting echo WLE for %s/%s from primary cluster: %v",
			d.cfg.Namespace.Name(),
			d.cfg.Service,
			err)
	}
}

func (d *deployment) workloadEntryYAML(w *workload) string {
	name := w.pod.Name
	podIP := w.pod.Status.PodIP
	sa := serviceAccount(d.cfg)
	network := d.cfg.Cluster.NetworkName()
	service := d.cfg.Service
	version := w.pod.Labels[constants.TestVMVersionLabel]

	return fmt.Sprintf(`
apiVersion: networking.istio.io/v1
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
`, name, podIP, sa, network, service, version)
}

func GenerateDeployment(ctx resource.Context, cfg echo.Config, settings *resource.Settings) (string, error) {
	if settings == nil {
		var err error
		settings, err = resource.SettingsFromCommandLine("template")
		if err != nil {
			return "", err
		}
	}

	params, err := deploymentParams(ctx, cfg, settings)
	if err != nil {
		return "", err
	}

	deploy := getTemplate(deploymentTemplateFile)
	if cfg.DeployAsVM {
		deploy = getTemplate(vmDeploymentTemplateFile)
	}

	return tmpl.Execute(deploy, params)
}

func GenerateService(cfg echo.Config, isOpenShift bool) (string, error) {
	params := serviceParams(cfg, isOpenShift)
	return tmpl.Execute(getTemplate(serviceTemplateFile), params)
}

var VMImages = map[echo.VMDistro]string{
	echo.UbuntuBionic: "app_sidecar_ubuntu_bionic",
	echo.UbuntuNoble:  "app_sidecar_ubuntu_noble",
	echo.Debian12:     "app_sidecar_debian_12",
	echo.Rockylinux9:  "app_sidecar_rockylinux_9",
}

// ArmVMImages is the subset of images that work on arm64. These fail because Istio's arm64 build has a higher GLIBC requirement
var ArmVMImages = map[echo.VMDistro]string{
	echo.UbuntuNoble: "app_sidecar_ubuntu_noble",
	echo.Debian12:    "app_sidecar_debian_12",
	echo.Rockylinux9: "app_sidecar_rockylinux_9",
}

var RevVMImages = func() map[string]echo.VMDistro {
	r := map[string]echo.VMDistro{}
	for k, v := range VMImages {
		r[v] = k
	}
	return r
}()

// getVMOverrideForIstiodDNS returns the DNS alias to use for istiod on VMs. VMs always access
// istiod via the east-west gateway, even though they are installed on the same cluster as istiod.
func getVMOverrideForIstiodDNS(ctx resource.Context, cfg echo.Config) (istioHost string, istioIP string) {
	if ctx == nil {
		return
	}

	ist, err := istio.Get(ctx)
	if err != nil {
		log.Warnf("VM config failed to get Istio component for %s: %v", cfg.Cluster.Name(), err)
		return
	}

	// Generate the istiod host the same way as istioctl.
	istioNS := ist.Settings().SystemNamespace
	istioRevision := getIstioRevision(cfg.Namespace)
	istioHost = istioctlcmd.IstiodHost(istioNS, istioRevision)

	istioIPAddr := ist.EastWestGatewayFor(cfg.Cluster).DiscoveryAddresses()[0].Addr()
	if !istioIPAddr.IsValid() {
		log.Warnf("VM config failed to get east-west gateway IP for %s", cfg.Cluster.Name())
		istioHost, istioIP = "", ""
	} else {
		istioIP = istioIPAddr.String()
	}
	return
}

func deploymentParams(ctx resource.Context, cfg echo.Config, settings *resource.Settings) (map[string]any, error) {
	supportStartupProbe := cfg.Cluster.MinKubeVersion(0)
	imagePullSecretName, err := settings.Image.PullSecretName()
	if err != nil {
		return nil, err
	}

	containerPorts := getContainerPorts(cfg)
	appContainers := []map[string]any{{
		"Name":           appContainerName,
		"ImageFullPath":  settings.EchoImage, // This overrides image hub/tag if it's not empty.
		"ContainerPorts": containerPorts,
	}}

	// Only use the custom image for proxyless gRPC instances. This will bind the gRPC ports on one container
	// and all other ports on another. Additionally, we bind one port for communication between the custom image
	// container, and the regular Go server.
	if cfg.IsProxylessGRPC() && settings.CustomGRPCEchoImage != "" {
		var grpcPorts, otherPorts echoCommon.PortList
		for _, port := range containerPorts {
			if port.Protocol == protocol.GRPC {
				grpcPorts = append(grpcPorts, port)
			} else {
				otherPorts = append(otherPorts, port)
			}
		}
		otherPorts = append(otherPorts, &echoCommon.Port{
			Name:     "grpc-fallback",
			Protocol: protocol.GRPC,
			Port:     grpcFallbackPort,
		})
		appContainers[0]["ContainerPorts"] = otherPorts
		appContainers = append(appContainers, map[string]any{
			"Name":           "custom-grpc-" + appContainerName,
			"ImageFullPath":  settings.CustomGRPCEchoImage, // This overrides image hub/tag if it's not empty.
			"ContainerPorts": grpcPorts,
			"FallbackPort":   grpcFallbackPort,
		})
	}

	if cfg.WorkloadWaypointProxy != "" {
		for _, subset := range cfg.Subsets {
			if subset.Labels == nil {
				subset.Labels = make(map[string]string)
			}
			subset.Labels[label.IoIstioUseWaypoint.Name] = cfg.WorkloadWaypointProxy
		}
	}

	params := map[string]any{
		"ImageHub":                settings.Image.Hub,
		"ImageTag":                settings.Image.Tag,
		"ImagePullPolicy":         settings.Image.PullPolicy,
		"ImagePullSecretName":     imagePullSecretName,
		"Service":                 cfg.Service,
		"StatefulSet":             cfg.StatefulSet,
		"ProxylessGRPC":           cfg.IsProxylessGRPC(),
		"GRPCMagicPort":           grpcMagicPort,
		"Locality":                cfg.Locality,
		"ServiceAccount":          cfg.ServiceAccount,
		"DisableAutomountSAToken": cfg.DisableAutomountSAToken,
		"AppContainers":           appContainers,
		"ContainerPorts":          containerPorts,
		"Subsets":                 cfg.Subsets,
		"TLSSettings":             cfg.TLSSettings,
		"Cluster":                 cfg.Cluster.Name(),
		"ReadinessTCPPort":        cfg.ReadinessTCPPort,
		"ReadinessGRPCPort":       cfg.ReadinessGRPCPort,
		"StartupProbe":            supportStartupProbe,
		"IncludeExtAuthz":         cfg.IncludeExtAuthz,
		"Revisions":               settings.Revisions.TemplateMap(),
		"Compatibility":           settings.Compatibility,
		"WorkloadClass":           cfg.WorkloadClass(),
		"OverlayIstioProxy":       canCreateIstioProxy(settings.Revisions.Minimum()) && !settings.Ambient,
		"Ambient":                 settings.Ambient,
		"BindFamily":              cfg.BindFamily,
		"OpenShift":               settings.OpenShift,
	}

	vmIstioHost, vmIstioIP := "", ""
	if cfg.IsVM() {
		vmImage := VMImages[cfg.VMDistro]
		_, knownImage := RevVMImages[cfg.VMDistro]
		if vmImage == "" {
			if knownImage {
				vmImage = cfg.VMDistro
			} else {
				vmImage = VMImages[echo.DefaultVMDistro]
			}
			log.Debugf("no image for distro %s, defaulting to %s", cfg.VMDistro, echo.DefaultVMDistro)
		}

		vmIstioHost, vmIstioIP = getVMOverrideForIstiodDNS(ctx, cfg)

		params["VM"] = map[string]any{
			"Image":     vmImage,
			"IstioHost": vmIstioHost,
			"IstioIP":   vmIstioIP,
		}
	}

	return params, nil
}

func serviceParams(cfg echo.Config, isOpenShift bool) map[string]any {
	if cfg.ServiceWaypointProxy != "" {
		if cfg.ServiceLabels == nil {
			cfg.ServiceLabels = make(map[string]string)
		}
		cfg.ServiceLabels[label.IoIstioUseWaypoint.Name] = cfg.ServiceWaypointProxy
	}
	return map[string]any{
		"Service":            cfg.Service,
		"Headless":           cfg.Headless,
		"ServiceAccount":     cfg.ServiceAccount,
		"ServicePorts":       cfg.Ports.GetServicePorts(),
		"ServiceLabels":      cfg.ServiceLabels,
		"IPFamilies":         cfg.IPFamilies,
		"IPFamilyPolicy":     cfg.IPFamilyPolicy,
		"ServiceAnnotations": cfg.ServiceAnnotations,
		"Namespace":          cfg.Namespace.Name(),
		"OpenShift":          isOpenShift,
	}
}

// createVMConfig sets up a Service account,
func createVMConfig(ctx resource.Context, cfg echo.Config) error {
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
apiVersion: networking.istio.io/v1
kind: WorkloadGroup
metadata:
  name: {{.name}}
  namespace: {{.namespace}}
spec:
  metadata:
    labels:
      app: {{.name}}
      test.istio.io/class: {{ .workloadClass }}
  template:
    serviceAccount: {{.serviceAccount}}
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
		"serviceAccount": serviceAccount(cfg),
		"network":        cfg.Cluster.NetworkName(),
		"workloadClass":  cfg.WorkloadClass(),
	})

	// Push the WorkloadGroup for auto-registration
	if cfg.AutoRegisterVM {
		if err := ctx.ConfigKube(cfg.Cluster).
			YAML(cfg.Namespace.Name(), wg).
			Apply(apply.NoCleanup); err != nil {
			return err
		}
	}

	if cfg.ServiceAccount {
		// create service account, the next workload command will use it to generate a token
		err = createServiceAccount(cfg.Cluster.Kube(), cfg.Namespace.Name(), serviceAccount(cfg))
		if err != nil && !kerrors.IsAlreadyExists(err) {
			return err
		}
	}

	if err := os.WriteFile(path.Join(dir, "workloadgroup.yaml"), []byte(wg), 0o600); err != nil {
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
		subsetDir, err = os.MkdirTemp(dir, subset.Version+"-")
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
			cmd = append(cmd, "--clusterID", cfg.Cluster.Name())
		}
		if cfg.AutoRegisterVM {
			cmd = append(cmd, "--autoregister")
		}
		if !ctx.Environment().(*kube.Environment).Settings().LoadBalancerSupported {
			// LoadBalancer may not be supported and the command doesn't have NodePort fallback logic that the tests do
			cmd = append(cmd, "--ingressIP", istiodAddr.Addr().String())
		}
		if rev := getIstioRevision(cfg.Namespace); len(rev) > 0 {
			cmd = append(cmd, "--revision", rev)
		}
		// make sure namespace controller has time to create root-cert ConfigMap
		if err := retry.UntilSuccess(func() error {
			stdout, stderr, err := istioCtl.Invoke(cmd)
			if err != nil {
				return fmt.Errorf("%v:\nstdout: %s\nstderr: %s", err, stdout, stderr)
			}
			return nil
		}, retry.Timeout(20*time.Second)); err != nil {
			return err
		}

		// support proxyConfig customizations on VMs via annotation in the echo API.
		for k, v := range subset.Annotations {
			if k == "proxy.istio.io/config" {
				if err := patchProxyConfigFile(path.Join(subsetDir, "mesh.yaml"), v); err != nil {
					return fmt.Errorf("failed patching proxyconfig: %v", err)
				}
			}
		}

		if err := customizeVMEnvironment(ctx, cfg, path.Join(subsetDir, "cluster.env"), istiodAddr); err != nil {
			return fmt.Errorf("failed customizing cluster.env: %v", err)
		}

		// push bootstrap config as a ConfigMap so we can mount it on our "vm" pods
		cmData := map[string][]byte{}
		generatedFiles, err := os.ReadDir(subsetDir)
		if err != nil {
			return err
		}
		for _, file := range generatedFiles {
			if file.IsDir() {
				continue
			}
			cmData[file.Name()], err = os.ReadFile(path.Join(subsetDir, file.Name()))
			if err != nil {
				return err
			}
		}
		cmName := fmt.Sprintf("%s-%s-vm-bootstrap", cfg.Service, subset.Version)
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: cmName}, BinaryData: cmData}
		_, err = cfg.Cluster.Kube().CoreV1().ConfigMaps(cfg.Namespace.Name()).Create(context.TODO(), cm, metav1.CreateOptions{})
		if err != nil && !kerrors.IsAlreadyExists(err) {
			return fmt.Errorf("failed creating configmap %s: %v", cm.Name, err)
		}
	}

	// push the generated token as a Secret (only need one, they should be identical)
	token, err := os.ReadFile(path.Join(subsetDir, "istio-token"))
	if err != nil {
		return err
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.Service + "-istio-token",
			Namespace: cfg.Namespace.Name(),
		},
		Data: map[string][]byte{
			"istio-token": token,
		},
	}
	if _, err := cfg.Cluster.Kube().CoreV1().Secrets(cfg.Namespace.Name()).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
		if kerrors.IsAlreadyExists(err) {
			if _, err := cfg.Cluster.Kube().CoreV1().Secrets(cfg.Namespace.Name()).Update(context.TODO(), secret, metav1.UpdateOptions{}); err != nil {
				return fmt.Errorf("failed updating secret %s: %v", secret.Name, err)
			}
		} else {
			return fmt.Errorf("failed creating secret %s: %v", secret.Name, err)
		}
	}

	return nil
}

func patchProxyConfigFile(file string, overrides string) error {
	config, err := readMeshConfig(file)
	if err != nil {
		return err
	}
	overrideYAML := "defaultConfig:\n"
	overrideYAML += istio.Indent(overrides, "  ")
	if err := protomarshal.ApplyYAML(overrideYAML, config.DefaultConfig); err != nil {
		return err
	}
	outYAML, err := protomarshal.ToYAML(config)
	if err != nil {
		return err
	}
	return os.WriteFile(file, []byte(outYAML), 0o744)
}

func readMeshConfig(file string) (*meshconfig.MeshConfig, error) {
	baseYAML, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	config := &meshconfig.MeshConfig{}
	if err := protomarshal.ApplyYAML(string(baseYAML), config); err != nil {
		return nil, err
	}
	return config, nil
}

func createServiceAccount(client kubernetes.Interface, ns string, serviceAccount string) error {
	scopes.Framework.Debugf("Creating service account for: %s/%s", ns, serviceAccount)
	_, err := client.CoreV1().ServiceAccounts(ns).Create(context.TODO(), &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: serviceAccount},
	}, metav1.CreateOptions{})
	return err
}

// getContainerPorts converts the ports to a port list of container ports.
// Adds ports for health/readiness if necessary.
func getContainerPorts(cfg echo.Config) echoCommon.PortList {
	ports := cfg.Ports
	containerPorts := make(echoCommon.PortList, 0, len(ports))
	var healthPort *echoCommon.Port
	var readyPort *echoCommon.Port
	for _, p := range ports {
		// Add the port to the set of application ports.
		cport := &echoCommon.Port{
			Name:              p.Name,
			Protocol:          p.Protocol,
			Port:              p.WorkloadPort,
			TLS:               p.TLS,
			RequireClientCert: p.MTLS,
			ServerFirst:       p.ServerFirst,
			InstanceIP:        p.InstanceIP,
			LocalhostIP:       p.LocalhostIP,
			ProxyProtocol:     p.ProxyProtocol,
		}
		containerPorts = append(containerPorts, cport)

		switch p.Protocol {
		case protocol.GRPC:
			if cfg.IsProxylessGRPC() {
				cport.XDSServer = true
			}
			continue
		case protocol.HTTP:
			if p.WorkloadPort == httpReadinessPort {
				readyPort = cport
			}
		default:
			if p.WorkloadPort == tcpHealthPort {
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

	// gives something the test runner to connect to without being in the mesh
	if cfg.IsProxylessGRPC() {
		containerPorts = append(containerPorts, &echoCommon.Port{
			Name:        "grpc-magic-port",
			Protocol:    protocol.GRPC,
			Port:        grpcMagicPort,
			LocalhostIP: true,
		})
	}
	return containerPorts
}

func customizeVMEnvironment(ctx resource.Context, cfg echo.Config, clusterEnv string, istiodAddr netip.AddrPort) error {
	f, err := os.OpenFile(clusterEnv, os.O_APPEND|os.O_WRONLY, os.ModeAppend)
	if err != nil {
		return fmt.Errorf("failed opening %s: %v", clusterEnv, err)
	}
	defer f.Close()

	if cfg.VMEnvironment != nil {
		for k, v := range cfg.VMEnvironment {
			addition := fmt.Sprintf("%s=%s\n", k, v)
			_, err = f.WriteString(addition)
			if err != nil {
				return fmt.Errorf("failed writing %q to %s: %v", addition, clusterEnv, err)
			}
		}
	}
	if !ctx.Environment().(*kube.Environment).Settings().LoadBalancerSupported {
		// customize cluster.env with NodePort mapping
		_, err = f.WriteString(fmt.Sprintf("ISTIO_PILOT_PORT=%d\n", istiodAddr.Port()))
		if err != nil {
			return err
		}
	}
	return err
}

func canCreateIstioProxy(version resource.IstioVersion) bool {
	// if no revision specified create the istio-proxy
	if string(version) == "" {
		return true
	}
	if minor := strings.Split(string(version), ".")[1]; minor > "8" || len(minor) > 1 {
		return true
	}
	return false
}

func getIstioRevision(n namespace.Instance) string {
	nsLabels, err := n.Labels()
	if err != nil {
		log.Warnf("failed fetching labels for %s; assuming no-revision (can cause failures): %v", n.Name(), err)
		return ""
	}
	return nsLabels[label.IoIstioRev.Name]
}

func statefulsetComplete(statefulset *appsv1.StatefulSet) bool {
	return statefulset.Status.UpdatedReplicas == *(statefulset.Spec.Replicas) &&
		statefulset.Status.Replicas == *(statefulset.Spec.Replicas) &&
		statefulset.Status.AvailableReplicas == *(statefulset.Spec.Replicas) &&
		statefulset.Status.ReadyReplicas == *(statefulset.Spec.Replicas) &&
		statefulset.Status.ObservedGeneration >= statefulset.Generation
}

func deploymentComplete(deployment *appsv1.Deployment) bool {
	return deployment.Status.UpdatedReplicas == *(deployment.Spec.Replicas) &&
		deployment.Status.Replicas == *(deployment.Spec.Replicas) &&
		deployment.Status.AvailableReplicas == *(deployment.Spec.Replicas) &&
		deployment.Status.ReadyReplicas == *(deployment.Spec.Replicas) &&
		deployment.Status.ObservedGeneration >= deployment.Generation
}

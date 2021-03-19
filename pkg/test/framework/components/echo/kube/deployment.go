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
	"bufio"
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/hashicorp/go-multierror"
	"gopkg.in/yaml.v3"
	kubeCore "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	echoCommon "istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/util/gogoprotomarshal"
	"istio.io/pkg/log"
)

const (
	serviceYAML = `
{{- if .ServiceAccount }}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .Service }}
---
{{- end }}
apiVersion: v1
kind: Service
metadata:
  name: {{ .Service }}
  labels:
    app: {{ .Service }}
{{- if .ServiceAnnotations }}
  annotations:
{{- range $name, $value := .ServiceAnnotations }}
    {{ $name.Name }}: {{ printf "%q" $value.Value }}
{{- end }}
{{- end }}
spec:
{{- if .Headless }}
  clusterIP: None
{{- end }}
  ports:
{{- range $i, $p := .Ports }}
  - name: {{ $p.Name }}
    port: {{ $p.ServicePort }}
    targetPort: {{ $p.InstancePort }}
{{- end }}
  selector:
    app: {{ .Service }}
`

	deploymentYAML = `
{{- $revVerMap := .IstioVersions }}
{{- $subsets := .Subsets }}
{{- $cluster := .Cluster }}
{{- range $i, $subset := $subsets }}
{{- range $revision, $version := $revVerMap }}
apiVersion: apps/v1
kind: Deployment
metadata:
{{- if $.IsMultiVersion }}
  name: {{ $.Service }}-{{ $subset.Version }}-{{ $revision }}
{{- else }}
  name: {{ $.Service }}-{{ $subset.Version }}
{{- end }}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: {{ $.Service }}
      version: {{ $subset.Version }}
{{- if ne $.Locality "" }}
      istio-locality: {{ $.Locality }}
{{- end }}
  template:
    metadata:
      labels:
        app: {{ $.Service }}
        version: {{ $subset.Version }}
{{- if $.IsMultiVersion }}
        istio.io/rev: {{ $revision }}
{{- end }}
{{- if ne $.Locality "" }}
        istio-locality: {{ $.Locality }}
{{- end }}
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "15014"
{{- range $name, $value := $subset.Annotations }}
        {{ $name.Name }}: {{ printf "%q" $value.Value }}
{{- end }}
    spec:
{{- if $.ServiceAccount }}
      serviceAccountName: {{ $.Service }}
{{- end }}
{{- if ne $.ImagePullSecret "" }}
      imagePullSecrets:
      - name: {{ $.ImagePullSecret }}
{{- end }}
      containers:
{{- if ne ($subset.Annotations.GetByName "sidecar.istio.io/inject") "false" }}
      - name: istio-proxy
        image: auto
        securityContext: # to allow core dumps
          readOnlyRootFilesystem: false
{{- end }}
{{- if $.IncludeExtAuthz }}
      - name: ext-authz
        image: docker.io/istio/ext-authz:0.6
        imagePullPolicy: {{ $.PullPolicy }}
        ports:
        - containerPort: 8000
        - containerPort: 9000
{{- end }}
      - name: app
        image: {{ $.Hub }}/app:{{ $.Tag }}
        imagePullPolicy: {{ $.PullPolicy }}
        securityContext:
          runAsUser: 1338
          runAsGroup: 1338
        args:
          - --metrics=15014
          - --cluster
          - "{{ $cluster }}"
{{- range $i, $p := $.ContainerPorts }}
{{- if eq .Protocol "GRPC" }}
          - --grpc
{{- else if eq .Protocol "TCP" }}
          - --tcp
{{- else }}
          - --port
{{- end }}
          - "{{ $p.Port }}"
{{- if $p.TLS }}
          - --tls={{ $p.Port }}
{{- end }}
{{- if $p.ServerFirst }}
          - --server-first={{ $p.Port }}
{{- end }}
{{- if $p.InstanceIP }}
          - --bind-ip={{ $p.Port }}
{{- end }}
{{- if $p.LocalhostIP }}
          - --bind-localhost={{ $p.Port }}
{{- end }}
{{- end }}
{{- range $i, $p := $.WorkloadOnlyPorts }}
{{- if eq .Protocol "TCP" }}
          - --tcp
{{- else }}
          - --port
{{- end }}
          - "{{ $p.Port }}"
{{- if $p.TLS }}
          - --tls={{ $p.Port }}
{{- end }}
{{- if $p.ServerFirst }}
          - --server-first={{ $p.Port }}
{{- end }}
{{- end }}
          - --version
          - "{{ $subset.Version }}"
          - --istio-version
          - "{{ $version }}"
{{- if $.TLSSettings }}
          - --crt=/etc/certs/custom/cert-chain.pem
          - --key=/etc/certs/custom/key.pem
{{- else }}
          - --crt=/cert.crt
          - --key=/cert.key
{{- end }}
        ports:
{{- range $i, $p := $.ContainerPorts }}
        - containerPort: {{ $p.Port }} 
{{- if eq .Port 3333 }}
          name: tcp-health-port
{{- end }}
{{- end }}
        env:
        - name: INSTANCE_IP
          valueFrom:
            fieldRef:
              fieldPath: status.podIP
        readinessProbe:
          httpGet:
            path: /
            port: 8080
          initialDelaySeconds: 1
          periodSeconds: 2
          failureThreshold: 10
        livenessProbe:
          tcpSocket:
            port: tcp-health-port
          initialDelaySeconds: 10
          periodSeconds: 10
          failureThreshold: 10
{{- if $.StartupProbe }}
        startupProbe:
          tcpSocket:
            port: tcp-health-port
          periodSeconds: 10
          failureThreshold: 10
{{- end }}
{{- if $.TLSSettings }}
        volumeMounts:
        - mountPath: /etc/certs/custom
          name: custom-certs
      volumes:
      - configMap:
          name: {{ $.Service }}-certs
        name: custom-certs
{{- end }}
---
{{- end }}
{{- end }}
{{- if .TLSSettings }}
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ $.Service }}-certs
data:
  root-cert.pem: |
{{ .TLSSettings.RootCert | indent 4 }}
  cert-chain.pem: |
{{ .TLSSettings.ClientCert | indent 4 }}
  key.pem: |
{{.TLSSettings.Key | indent 4}}
---
{{- end}}
`

	// vmDeploymentYaml aims to simulate a VM, but instead of managing the complex test setup of spinning up a VM,
	// connecting, etc we run it inside a pod. The pod has pretty much all Kubernetes features disabled (DNS and SA token mount)
	// such that we can adequately simulate a VM and DIY the bootstrapping.
	vmDeploymentYaml = `
{{- $subsets := .Subsets }}
{{- $cluster := .Cluster }}
{{- range $i, $subset := $subsets }}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ $.Service }}-{{ $subset.Version }}
spec:
  replicas: 1
  selector:
    matchLabels:
      istio.io/test-vm: {{ $.Service }}
      istio.io/test-vm-version: {{ $subset.Version }}
  template:
    metadata:
      annotations:
        # Sidecar is inside the pod to simulate VMs - do not inject
        sidecar.istio.io/inject: "false"
      labels:
        # Label should not be selected. We will create a workload entry instead
        istio.io/test-vm: {{ $.Service }}
        istio.io/test-vm-version: {{ $subset.Version }}
    spec:
      # Disable kube-dns, to mirror VM
      # we set policy to none and explicitly provide a set of invalid values
      # for nameservers, search namespaces, etc. ndots is set to 1 so that
      # the application will first try to resolve the hostname (a, a.ns, etc.) as is
      # before attempting to add the search namespaces.
      dnsPolicy: None
      dnsConfig:
        nameservers:
        - "8.8.8.8"
        searches:
        - "com"
        options:
        - name: "ndots"
          value: "1"
      # Disable service account mount, to mirror VM
      automountServiceAccountToken: false
      {{- if $.ImagePullSecret }}
      imagePullSecrets:
      - name: {{ $.ImagePullSecret }}
      {{- end }}
      containers:
      - name: istio-proxy
        image: {{ $.Hub }}/{{ $.VM.Image }}:{{ $.Tag }}
        imagePullPolicy: {{ $.PullPolicy }}
        securityContext:
          capabilities:
            add:
            - NET_ADMIN
          runAsUser: 1338
          runAsGroup: 1338
        command:
        - bash
        - -c
        - |-
          # Read root cert from and place signed certs here (can't mount directly or the dir would be unwritable)
          sudo mkdir -p /var/run/secrets/istio

          # hack: remove certs that are bundled in the image
          sudo rm /var/run/secrets/istio/cert-chain.pem
          sudo rm /var/run/secrets/istio/key.pem
          sudo chown -R istio-proxy /var/run/secrets

          # place mounted bootstrap files (token is mounted directly to the correct location)
          sudo cp /var/run/secrets/istio/bootstrap/root-cert.pem /var/run/secrets/istio/root-cert.pem
          sudo cp /var/run/secrets/istio/bootstrap/cluster.env /var/lib/istio/envoy/cluster.env
          sudo cp /var/run/secrets/istio/bootstrap/mesh.yaml /etc/istio/config/mesh
          sudo sh -c 'cat /var/run/secrets/istio/bootstrap/hosts >> /etc/hosts'

          # read certs from correct directory
          sudo sh -c 'echo PROV_CERT=/var/run/secrets/istio >> /var/lib/istio/envoy/cluster.env'
          sudo sh -c 'echo OUTPUT_CERTS=/var/run/secrets/istio >> /var/lib/istio/envoy/cluster.env'
          # Block standard inbound ports
          sudo sh -c 'echo ISTIO_LOCAL_EXCLUDE_PORTS="15090,15021,15020" >> /var/lib/istio/envoy/cluster.env'

          # TODO: run with systemctl?
          export ISTIO_AGENT_FLAGS="--concurrency 2"
          sudo -E /usr/local/bin/istio-start.sh&
          /usr/local/bin/server --cluster "{{ $cluster }}" --version "{{ $subset.Version }}" \
{{- range $i, $p := $.ContainerPorts }}
{{- if eq .Protocol "GRPC" }}
             --grpc \
{{- else if eq .Protocol "TCP" }}
             --tcp \
{{- else }}
             --port \
{{- end }}
             "{{ $p.Port }}" \
{{- if $p.ServerFirst }}
             --server-first={{ $p.Port }} \
{{- end }}
{{- if $p.TLS }}
             --tls={{ $p.Port }} \
{{- end }}
{{- if $p.InstanceIP }}
             --bind-ip={{ $p.Port }} \
{{- end }}
{{- if $p.LocalhostIP }}
             --bind-localhost={{ $p.Port }} \
{{- end }}
{{- end }}
             --crt=/var/lib/istio/cert.crt \
             --key=/var/lib/istio/cert.key
        env:
        - name: INSTANCE_IP
          valueFrom:
            fieldRef:
              fieldPath: status.podIP
        volumeMounts:
        - mountPath: /var/run/secrets/tokens
          name: {{ $.Service }}-istio-token
        - mountPath: /var/run/secrets/istio/bootstrap
          name: istio-vm-bootstrap
        {{- range $name, $value := $subset.Annotations }}
        {{- if eq $name.Name "sidecar.istio.io/bootstrapOverride" }}
        - mountPath: /etc/istio/custom-bootstrap
          name: custom-bootstrap-volume
        {{- end }}
        {{- end }}
      volumes:
      - secret:
          secretName: {{ $.Service }}-istio-token
        name: {{ $.Service }}-istio-token
      - configMap:
          name: {{ $.Service }}-{{ $subset.Version }}-vm-bootstrap
        name: istio-vm-bootstrap
      {{- range $name, $value := $subset.Annotations }}
      {{- if eq $name.Name "sidecar.istio.io/bootstrapOverride" }}
      - name: custom-bootstrap-volume
        configMap:
          name: {{ $value.Value }}
      {{- end }}
      {{- end }}
{{- end}}
`
)

var (
	serviceTemplate      *template.Template
	deploymentTemplate   *template.Template
	vmDeploymentTemplate *template.Template
)

func init() {
	serviceTemplate = template.New("echo_service")
	if _, err := serviceTemplate.Funcs(sprig.TxtFuncMap()).Parse(serviceYAML); err != nil {
		panic(fmt.Sprintf("unable to parse echo service template: %v", err))
	}

	deploymentTemplate = template.New("echo_deployment")
	if _, err := deploymentTemplate.Funcs(sprig.TxtFuncMap()).Parse(deploymentYAML); err != nil {
		panic(fmt.Sprintf("unable to parse echo deployment template: %v", err))
	}

	vmDeploymentTemplate = template.New("echo_vm_deployment")
	if _, err := vmDeploymentTemplate.Funcs(sprig.TxtFuncMap()).Funcs(template.FuncMap{"Lines": lines}).Parse(vmDeploymentYaml); err != nil {
		panic(fmt.Sprintf("unable to parse echo vm deployment template: %v", err))
	}
}

var _ workloadHandler = &deployment{}

type deployment struct {
	ctx             resource.Context
	cfg             echo.Config
	shouldCreateWLE bool
}

func newDeployment(ctx resource.Context, cfg echo.Config) (*deployment, error) {
	if !cfg.Cluster.IsPrimary() && cfg.DeployAsVM {
		return nil, fmt.Errorf("cannot deploy %s/%s as VM on non-primary %s",
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

	deploymentYAML, err := generateDeploymentYAML(cfg, nil, ctx.Settings().IstioVersions)
	if err != nil {
		return nil, fmt.Errorf("failed generating echo deployment YAML for %s/%s",
			cfg.Namespace.Name(),
			cfg.Service)
	}

	// Apply the deployment to the configured cluster.
	if err = ctx.Config(cfg.Cluster).ApplyYAMLNoCleanup(cfg.Namespace.Name(), deploymentYAML); err != nil {
		return nil, fmt.Errorf("failed deploying echo %s to cluster %s: %v",
			cfg.FQDN(), cfg.Cluster.Name(), err)
	}

	return &deployment{
		ctx:             ctx,
		cfg:             cfg,
		shouldCreateWLE: cfg.DeployAsVM && !cfg.AutoRegisterVM,
	}, nil
}

// Restart performs a `kubectl rollout restart` on the echo deployment and waits for
// `kubectl rollout status` to complete before returning.
func (d *deployment) Restart() error {
	var errs error
	var deploymentNames []string
	for _, s := range d.cfg.Subsets {
		// TODO(Monkeyanator) move to common place so doesn't fall out of sync with templates
		deploymentNames = append(deploymentNames, fmt.Sprintf("%s-%s", d.cfg.Service, s.Version))
	}
	for _, deploymentName := range deploymentNames {
		rolloutCmd := fmt.Sprintf("kubectl rollout restart deployment/%s -n %s",
			deploymentName, d.cfg.Namespace.Name())
		_, err := shell.Execute(true, rolloutCmd)
		errs = multierror.Append(errs, err).ErrorOrNil()
		if err != nil {
			continue
		}
		waitCmd := fmt.Sprintf("kubectl rollout status deployment/%s -n %s",
			deploymentName, d.cfg.Namespace.Name())
		_, err = shell.Execute(true, waitCmd)
		errs = multierror.Append(errs, err).ErrorOrNil()
	}
	return errs
}

func (d *deployment) WorkloadReady(w *workload) {
	if !d.shouldCreateWLE {
		return
	}

	// Deploy the workload entry to the primary cluster. We will read WorkloadEntry across clusters.
	wle := d.workloadEntryYAML(w)
	if err := d.ctx.Config(d.cfg.Cluster.Primary()).ApplyYAMLNoCleanup(d.cfg.Namespace.Name(), wle); err != nil {
		log.Warnf("failed deploying echo WLE for %s/%s to pimary cluster: %v",
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
	if err := d.ctx.Config(d.cfg.Cluster.Primary()).DeleteYAML(d.cfg.Namespace.Name(), wle); err != nil {
		log.Warnf("failed deleting echo WLE for %s/%s from pimary cluster: %v",
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
`, name, podIP, sa, network, service, version)
}

func generateDeploymentYAML(cfg echo.Config, settings *image.Settings, versions resource.RevVerMap) (string, error) {
	params, err := templateParams(cfg, settings, versions)
	if err != nil {
		return "", err
	}

	deploy := deploymentTemplate
	if cfg.DeployAsVM {
		deploy = vmDeploymentTemplate
	}

	return tmpl.Execute(deploy, params)
}

func GenerateService(cfg echo.Config) (string, error) {
	params, err := templateParams(cfg, nil, resource.RevVerMap{})
	if err != nil {
		return "", err
	}

	return tmpl.Execute(serviceTemplate, params)
}

const DefaultVMImage = "app_sidecar_ubuntu_bionic"

func templateParams(cfg echo.Config, settings *image.Settings, versions resource.RevVerMap) (map[string]interface{}, error) {
	if settings == nil {
		var err error
		settings, err = image.SettingsFromCommandLine()
		if err != nil {
			return nil, err
		}
	}
	supportStartupProbe := cfg.Cluster.MinKubeVersion(16, 0)

	// if image is not provided, default to app_sidecar
	vmImage := DefaultVMImage
	if cfg.VMImage != "" {
		vmImage = cfg.VMImage
	}
	namespace := ""
	if cfg.Namespace != nil {
		namespace = cfg.Namespace.Name()
	}
	imagePullSecret := ""
	if settings.ImagePullSecret != "" {
		data, err := ioutil.ReadFile(settings.ImagePullSecret)
		if err != nil {
			return nil, err
		}
		secret := unstructured.Unstructured{Object: map[string]interface{}{}}
		if err := yaml.Unmarshal(data, secret.Object); err != nil {
			return nil, err
		}
		imagePullSecret = secret.GetName()
	}
	params := map[string]interface{}{
		"Hub":                settings.Hub,
		"Tag":                strings.TrimSuffix(settings.Tag, "-distroless"),
		"PullPolicy":         settings.PullPolicy,
		"Service":            cfg.Service,
		"Version":            cfg.Version,
		"Headless":           cfg.Headless,
		"Locality":           cfg.Locality,
		"ServiceAccount":     cfg.ServiceAccount,
		"Ports":              cfg.Ports,
		"WorkloadOnlyPorts":  cfg.WorkloadOnlyPorts,
		"ContainerPorts":     getContainerPorts(cfg.Ports),
		"ServiceAnnotations": cfg.ServiceAnnotations,
		"Subsets":            cfg.Subsets,
		"TLSSettings":        cfg.TLSSettings,
		"Cluster":            cfg.Cluster.Name(),
		"Namespace":          namespace,
		"ImagePullSecret":    imagePullSecret,
		"VM": map[string]interface{}{
			"Image": vmImage,
		},
		"StartupProbe":    supportStartupProbe,
		"IncludeExtAuthz": cfg.IncludeExtAuthz,
		"IstioVersions":   versions.TemplateMap(),
		"IsMultiVersion":  versions.IsMultiVersion(),
	}
	return params, nil
}

func lines(input string) []string {
	out := make([]string, 0)
	scanner := bufio.NewScanner(strings.NewReader(input))
	for scanner.Scan() {
		out = append(out, scanner.Text())
	}
	return out
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
		"serviceaccount": serviceAccount(cfg),
		"network":        cfg.Cluster.NetworkName(),
	})

	// Push the WorkloadGroup for auto-registration
	if cfg.AutoRegisterVM {
		if err := ctx.Config(cfg.Cluster).ApplyYAMLNoCleanup(cfg.Namespace.Name(), wg); err != nil {
			return err
		}
	}

	if cfg.ServiceAccount {
		// create service account, the next workload command will use it to generate a token
		err = createServiceAccount(cfg.Cluster, cfg.Namespace.Name(), serviceAccount(cfg))
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
			cmd = append(cmd, "--clusterID", cfg.Cluster.Name())
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
		_, err = cfg.Cluster.CoreV1().ConfigMaps(cfg.Namespace.Name()).Create(context.TODO(), cm, metav1.CreateOptions{})
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
	if _, err := cfg.Cluster.CoreV1().Secrets(cfg.Namespace.Name()).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
		if kerrors.IsAlreadyExists(err) {
			if _, err := cfg.Cluster.CoreV1().Secrets(cfg.Namespace.Name()).Update(context.TODO(), secret, metav1.UpdateOptions{}); err != nil {
				return err
			}
		} else {
			return err
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

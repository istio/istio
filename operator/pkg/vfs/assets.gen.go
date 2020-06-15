// Code generated for package vfs by go-bindata DO NOT EDIT. (@generated)
// sources:
// examples/customresource/istio_v1alpha1_istiooperator_cr.yaml
// examples/multicluster/values-istio-multicluster-gateways.yaml
// examples/multicluster/values-istio-multicluster-primary.yaml
// examples/user-gateway/ingress-gateway-only.yaml
// examples/vm/values-istio-meshexpansion-gateways.yaml
// examples/vm/values-istio-meshexpansion.yaml
// profiles/default.yaml
// profiles/demo.yaml
// profiles/empty.yaml
// profiles/minimal.yaml
// profiles/preview.yaml
// profiles/remote.yaml
// translateConfig/names-1.5.yaml
// translateConfig/names-1.6.yaml
// translateConfig/reverseTranslateConfig-1.4.yaml
// translateConfig/reverseTranslateConfig-1.5.yaml
// translateConfig/reverseTranslateConfig-1.6.yaml
// translateConfig/translateConfig-1.3.yaml
// translateConfig/translateConfig-1.4.yaml
// translateConfig/translateConfig-1.5.yaml
// translateConfig/translateConfig-1.6.yaml
// versions.yaml
package vfs

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)
type asset struct {
	bytes []byte
	info  os.FileInfo
}

type bindataFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

// Name return file name
func (fi bindataFileInfo) Name() string {
	return fi.name
}

// Size return file size
func (fi bindataFileInfo) Size() int64 {
	return fi.size
}

// Mode return file mode
func (fi bindataFileInfo) Mode() os.FileMode {
	return fi.mode
}

// Mode return file modify time
func (fi bindataFileInfo) ModTime() time.Time {
	return fi.modTime
}

// IsDir return file whether a directory
func (fi bindataFileInfo) IsDir() bool {
	return fi.mode&os.ModeDir != 0
}

// Sys return file is sys mode
func (fi bindataFileInfo) Sys() interface{} {
	return nil
}

var _examplesCustomresourceIstio_v1alpha1_istiooperator_crYaml = []byte(`---
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  namespace: istio-system
  name: example-istiocontrolplane
spec:
  profile: demo
...
`)

func examplesCustomresourceIstio_v1alpha1_istiooperator_crYamlBytes() ([]byte, error) {
	return _examplesCustomresourceIstio_v1alpha1_istiooperator_crYaml, nil
}

func examplesCustomresourceIstio_v1alpha1_istiooperator_crYaml() (*asset, error) {
	bytes, err := examplesCustomresourceIstio_v1alpha1_istiooperator_crYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "examples/customresource/istio_v1alpha1_istiooperator_cr.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _examplesMulticlusterValuesIstioMulticlusterGatewaysYaml = []byte(`apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  addonComponents:
    istiocoredns:
      enabled: true

  components:
    egressGateways:
      - name: istio-egressgateway
        enabled: true

  values:
    global:
      # Provides dns resolution for global services
      podDNSSearchNamespaces:
        - global
        - "{{ valueOrDefault .DeploymentMeta.Namespace \"default\" }}.global"

      multiCluster:
        enabled: true

      controlPlaneSecurityEnabled: true

    gateways:
      istio-egressgateway:
        env:
          # Needed to route traffic via egress gateway if desired.
          ISTIO_META_REQUESTED_NETWORK_VIEW: "external"
`)

func examplesMulticlusterValuesIstioMulticlusterGatewaysYamlBytes() ([]byte, error) {
	return _examplesMulticlusterValuesIstioMulticlusterGatewaysYaml, nil
}

func examplesMulticlusterValuesIstioMulticlusterGatewaysYaml() (*asset, error) {
	bytes, err := examplesMulticlusterValuesIstioMulticlusterGatewaysYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "examples/multicluster/values-istio-multicluster-gateways.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _examplesMulticlusterValuesIstioMulticlusterPrimaryYaml = []byte(`apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  # https://istio.io/docs/setup/install/multicluster/shared/#main-cluster
  values:
    global:
      multiCluster:
        # unique cluster name, must be a DNS label name
        clusterName: main0
      network: network1

      # Mesh network configuration. This is optional and may be omitted if
      # all clusters are on the same network.
      meshNetworks:
        network1:
          endpoints:
          # Always use Kubernetes as the registry name for the main cluster in the mesh network configuration
          - fromRegistry: Kubernetes
          gateways:
          - registry_service_name: istio-ingressgateway.istio-system.svc.cluster.local
            port: 443

        network2:
          endpoints:
          - fromRegistry: remote0
          gateways:
          - registry_service_name: istio-ingressgateway.istio-system.svc.cluster.local
            port: 443

      # Use the existing istio-ingressgateway.
      meshExpansion:
        enabled: true
`)

func examplesMulticlusterValuesIstioMulticlusterPrimaryYamlBytes() ([]byte, error) {
	return _examplesMulticlusterValuesIstioMulticlusterPrimaryYaml, nil
}

func examplesMulticlusterValuesIstioMulticlusterPrimaryYaml() (*asset, error) {
	bytes, err := examplesMulticlusterValuesIstioMulticlusterPrimaryYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "examples/multicluster/values-istio-multicluster-primary.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _examplesUserGatewayIngressGatewayOnlyYaml = []byte(`apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  profile: empty
  gateways:
    enabled: true
    components:
      namespace: my-namespace
      ingressGateway:
        enabled: true
`)

func examplesUserGatewayIngressGatewayOnlyYamlBytes() ([]byte, error) {
	return _examplesUserGatewayIngressGatewayOnlyYaml, nil
}

func examplesUserGatewayIngressGatewayOnlyYaml() (*asset, error) {
	bytes, err := examplesUserGatewayIngressGatewayOnlyYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "examples/user-gateway/ingress-gateway-only.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _examplesVmValuesIstioMeshexpansionGatewaysYaml = []byte(`apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  values:
    global:
      multiCluster:
        enabled: true

      meshExpansion:
        enabled: true

      controlPlaneSecurityEnabled: true

    # Provides dns resolution for service entries of form
    # name.namespace.global
    istiocoredns:
      enabled: true
`)

func examplesVmValuesIstioMeshexpansionGatewaysYamlBytes() ([]byte, error) {
	return _examplesVmValuesIstioMeshexpansionGatewaysYaml, nil
}

func examplesVmValuesIstioMeshexpansionGatewaysYaml() (*asset, error) {
	bytes, err := examplesVmValuesIstioMeshexpansionGatewaysYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "examples/vm/values-istio-meshexpansion-gateways.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _examplesVmValuesIstioMeshexpansionYaml = []byte(`apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  values:
    global:
      meshExpansion:
        enabled: true

      controlPlaneSecurityEnabled: true
`)

func examplesVmValuesIstioMeshexpansionYamlBytes() ([]byte, error) {
	return _examplesVmValuesIstioMeshexpansionYaml, nil
}

func examplesVmValuesIstioMeshexpansionYaml() (*asset, error) {
	bytes, err := examplesVmValuesIstioMeshexpansionYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "examples/vm/values-istio-meshexpansion.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _profilesDefaultYaml = []byte(`apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  namespace: istio-system
spec:
  hub: gcr.io/istio-testing
  tag: latest

  # You may override parts of meshconfig by uncommenting the following lines.
  meshConfig:
    enablePrometheusMerge: false
    # Opt-out of global http2 upgrades.
    # Destination rule is used to opt-in.
    # h2_upgrade_policy: DO_NOT_UPGRADE

  # Traffic management feature
  components:
    base:
      enabled: true
    pilot:
      enabled: true
      k8s:
        env:
          - name: POD_NAME
            valueFrom:
              fieldRef:
                apiVersion: v1
                fieldPath: metadata.name
          - name: POD_NAMESPACE
            valueFrom:
              fieldRef:
                apiVersion: v1
                fieldPath: metadata.namespace
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 1
          periodSeconds: 3
          timeoutSeconds: 5
        strategy:
          rollingUpdate:
            maxSurge: "100%"
            maxUnavailable: "25%"

    # Policy feature
    policy:
      enabled: false
      k8s:
        hpaSpec:
          maxReplicas: 5
          minReplicas: 1
          scaleTargetRef:
            apiVersion: apps/v1
            kind: Deployment
            name: istio-policy
          metrics:
            - type: Resource
              resource:
                name: cpu
                targetAverageUtilization: 80
        env:
          - name: POD_NAMESPACE
            valueFrom:
              fieldRef:
                apiVersion: v1
                fieldPath: metadata.namespace
        strategy:
          rollingUpdate:
            maxSurge: "100%"
            maxUnavailable: "25%"

    # Telemetry feature
    telemetry:
      enabled: false
      k8s:
        env:
          - name: POD_NAMESPACE
            valueFrom:
              fieldRef:
                apiVersion: v1
                fieldPath: metadata.namespace
          - name: GOMAXPROCS
            value: "6"
        hpaSpec:
          maxReplicas: 5
          minReplicas: 1
          scaleTargetRef:
            apiVersion: apps/v1
            kind: Deployment
            name: istio-telemetry
          metrics:
            - type: Resource
              resource:
                name: cpu
                targetAverageUtilization: 80
        replicaCount: 1
        resources:
          requests:
            cpu: 1000m
            memory: 1G
          limits:
            cpu: 4800m
            memory: 4G
        strategy:
          rollingUpdate:
            maxSurge: "100%"
            maxUnavailable: "25%"

    # Security feature
    citadel:
      enabled: false
      k8s:
        strategy:
          rollingUpdate:
            maxSurge: "100%"
            maxUnavailable: "25%"

    # Istio Gateway feature
    ingressGateways:
    - name: istio-ingressgateway
      enabled: true
      k8s:
        env:
          - name: ISTIO_META_ROUTER_MODE
            value: "sni-dnat"
        service:
          ports:
            - port: 15021
              targetPort: 15021
              name: status-port
            - port: 80
              targetPort: 8080
              name: http2
            - port: 443
              targetPort: 8443
              name: https
            - port: 15443
              targetPort: 15443
              name: tls
        hpaSpec:
          maxReplicas: 5
          minReplicas: 1
          scaleTargetRef:
            apiVersion: apps/v1
            kind: Deployment
            name: istio-ingressgateway
          metrics:
            - type: Resource
              resource:
                name: cpu
                targetAverageUtilization: 80
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 2000m
            memory: 1024Mi
        strategy:
          rollingUpdate:
            maxSurge: "100%"
            maxUnavailable: "25%"

    egressGateways:
    - name: istio-egressgateway
      enabled: false
      k8s:
        env:
          - name: ISTIO_META_ROUTER_MODE
            value: "sni-dnat"
        service:
          ports:
            - port: 80
              name: http2
            - port: 443
              name: https
            - port: 15443
              targetPort: 15443
              name: tls
        hpaSpec:
          maxReplicas: 5
          minReplicas: 1
          scaleTargetRef:
            apiVersion: apps/v1
            kind: Deployment
            name: istio-egressgateway
          metrics:
            - type: Resource
              resource:
                name: cpu
                targetAverageUtilization: 80
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 2000m
            memory: 1024Mi
        strategy:
          rollingUpdate:
            maxSurge: "100%"
            maxUnavailable: "25%"
    # Istio CNI feature
    cni:
      enabled: false
    
    # istiod remote configuration wwhen istiod isn't installed on the cluster
    istiodRemote:
      enabled: false

  addonComponents:
    prometheus:
      enabled: true
      k8s:
        replicaCount: 1
    kiali:
      enabled: false
      k8s:
        replicaCount: 1
    grafana:
      enabled: false
      k8s:
        replicaCount: 1
    tracing:
      enabled: false
    istiocoredns:
      enabled: false

  # Global values passed through to helm global.yaml.
  # Please keep this in sync with manifests/charts/global.yaml
  values:
    global:
      istioNamespace: istio-system
      istiod:
        enabled: true
        enableAnalysis: false
      logging:
        level: "default:info"
      logAsJson: false
      pilotCertProvider: istiod
      jwtPolicy: third-party-jwt
      proxy:
        image: proxyv2
        clusterDomain: "cluster.local"
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 2000m
            memory: 1024Mi
        logLevel: warning
        componentLogLevel: "misc:error"
        privileged: false
        enableCoreDump: false
        statusPort: 15020
        readinessInitialDelaySeconds: 1
        readinessPeriodSeconds: 2
        readinessFailureThreshold: 30
        includeIPRanges: "*"
        excludeIPRanges: ""
        excludeOutboundPorts: ""
        excludeInboundPorts: ""
        autoInject: enabled
        envoyStatsd:
          enabled: false
          host: # example: statsd-svc.istio-system
          port: # example: 9125
        tracer: "zipkin"
      proxy_init:
        image: proxyv2
        resources:
          limits:
            cpu: 100m
            memory: 50Mi
          requests:
            cpu: 10m
            memory: 10Mi
      # Specify image pull policy if default behavior isn't desired.
      # Default behavior: latest images will be Always else IfNotPresent.
      imagePullPolicy: ""
      operatorManageWebhooks: false
      controlPlaneSecurityEnabled: true
      tracer:
        lightstep:
          address: ""                # example: lightstep-satellite:443
          accessToken: ""            # example: abcdefg1234567
        zipkin:
          address: ""
        datadog:
          address: "$(HOST_IP):8126"
        stackdriver:
          debug: false
          maxNumberOfAttributes: 200
          maxNumberOfAnnotations: 200
          maxNumberOfMessageEvents: 200
      imagePullSecrets: []
      arch:
        amd64: 2
        s390x: 2
        ppc64le: 2
      oneNamespace: false
      defaultNodeSelector: {}
      configValidation: true
      meshExpansion:
        enabled: false
        useILB: false
      multiCluster:
        enabled: false
        clusterName: ""
      omitSidecarInjectorConfigMap: false
      network: ""
      defaultResources:
        requests:
          cpu: 10m
      defaultPodDisruptionBudget:
        enabled: true
      priorityClassName: ""
      useMCP: false
      trustDomain: "cluster.local"
      sds:
        token:
          aud: istio-ca
      sts:
        servicePort: 0
      meshNetworks: {}
      enableHelmTest: false
      mountMtlsCerts: false
    base:
      validationURL: ""
    pilot:
      autoscaleEnabled: true
      autoscaleMin: 1
      autoscaleMax: 5
      replicaCount: 1
      image: pilot
      traceSampling: 1.0
      configNamespace: istio-config
      appNamespaces: []
      env: {}
      cpu:
        targetAverageUtilization: 80
      nodeSelector: {}
      tolerations: []
      podAntiAffinityLabelSelector: []
      podAntiAffinityTermLabelSelector: []
      keepaliveMaxServerConnectionAge: 30m
      enableProtocolSniffingForOutbound: true
      enableProtocolSniffingForInbound: true
      deploymentLabels:
      configMap: true
      policy:
        enabled: false

    telemetry:
      enabled: true
      v1:
        enabled: false
      v2:
        enabled: true
        metadataExchange: {}
        prometheus:
          enabled: true
        stackdriver:
          enabled: false
          logging: false
          monitoring: false
          topology: false
          configOverride: {}
    mixer:
      adapters:
        stdio:
          enabled: false
          outputAsJson: false
        prometheus:
          enabled: true
          metricsExpiryDuration: 10m
        kubernetesenv:
          enabled: true
        stackdriver:
          enabled: false
          auth:
            appCredentials: false
            apiKey: ""
            serviceAccountPath: ""
          tracer:
            enabled: false
            sampleProbability: 1
        useAdapterCRDs: false

      telemetry:
        image: mixer
        replicaCount: 1
        autoscaleEnabled: true
        sessionAffinityEnabled: false
        loadshedding:
          mode: enforce
          latencyThreshold: 100ms
        env:
          GOMAXPROCS: "6"
        nodeSelector: {}
        tolerations: []
        podAntiAffinityLabelSelector: []
        podAntiAffinityTermLabelSelector: []

      policy:
        autoscaleEnabled: true
        image: mixer
        sessionAffinityEnabled: false
        adapters:
          kubernetesenv:
            enabled: true
          useAdapterCRDs: false

    istiodRemote:
      injectionURL: ""
      
    gateways:
      istio-egressgateway:
        zvpn: {}
        env: {}
        autoscaleEnabled: true
        type: ClusterIP
        name: istio-egressgateway
        secretVolumes:
          - name: egressgateway-certs
            secretName: istio-egressgateway-certs
            mountPath: /etc/istio/egressgateway-certs
          - name: egressgateway-ca-certs
            secretName: istio-egressgateway-ca-certs
            mountPath: /etc/istio/egressgateway-ca-certs

      istio-ingressgateway:
        autoscaleEnabled: true
        applicationPorts: ""
        debug: info
        domain: ""
        type: LoadBalancer
        name: istio-ingressgateway
        zvpn: {}
        env: {}
        meshExpansionPorts:
          - port: 15011
            targetPort: 15011
            name: tcp-pilot-grpc-tls
          - port: 15012
            targetPort: 15012
            name: tcp-istiod
          - port: 8060
            targetPort: 8060
            name: tcp-citadel-grpc-tls
          - port: 853
            targetPort: 8853
            name: tcp-dns-tls
        secretVolumes:
          - name: ingressgateway-certs
            secretName: istio-ingressgateway-certs
            mountPath: /etc/istio/ingressgateway-certs
          - name: ingressgateway-ca-certs
            secretName: istio-ingressgateway-ca-certs
            mountPath: /etc/istio/ingressgateway-ca-certs

    sidecarInjectorWebhook:
      enableNamespacesByDefault: false
      rewriteAppHTTPProbe: true
      injectLabel: istio-injection
      objectSelector:
        enabled: false
        autoInject: true

    prometheus:
      hub: docker.io/prom
      tag: v2.15.1
      retention: 6h
      scrapeInterval: 15s
      contextPath: /prometheus

      security:
        enabled: true
      nodeSelector: {}
      tolerations: []
      podAntiAffinityLabelSelector: []
      podAntiAffinityTermLabelSelector: []
      provisionPrometheusCert: true

    grafana:
      image:
        repository: grafana/grafana
        tag: 6.5.2
      persist: false
      storageClassName: ""
      accessMode: ReadWriteMany
      security:
        enabled: false
        secretName: grafana
        usernameKey: username
        passphraseKey: passphrase
      contextPath: /grafana
      service:
        annotations: {}
        name: http
        type: ClusterIP
        externalPort: 3000
        loadBalancerIP:
        loadBalancerSourceRanges:
      datasources:
        datasources.yaml:
          apiVersion: 1
          datasources:
      dashboardProviders:
        dashboardproviders.yaml:
          apiVersion: 1
          providers:
            - name: 'istio'
              orgId: 1
              folder: 'istio'
              type: file
              disableDeletion: false
              options:
                path: /var/lib/grafana/dashboards/istio
      nodeSelector: {}
      tolerations: []
      podAntiAffinityLabelSelector: []
      podAntiAffinityTermLabelSelector: []
      env: {}
      envSecrets: {}

    tracing:
      provider: jaeger
      nodeSelector: {}
      podAntiAffinityLabelSelector: []
      podAntiAffinityTermLabelSelector: []
      jaeger:
        hub: docker.io/jaegertracing
        tag: "1.16"
        memory:
          max_traces: 50000
        spanStorageType: badger
        persist: false
        storageClassName: ""
        accessMode: ReadWriteMany
      zipkin:
        hub: docker.io/openzipkin
        tag: 2.20.0
        probeStartupDelay: 10
        queryPort: 9411
        resources:
          limits:
            cpu: 1000m
            memory: 2048Mi
          requests:
            cpu: 150m
            memory: 900Mi
        javaOptsHeap: 700
        maxSpans: 500000
        node:
          cpus: 2
      opencensus:
        hub: docker.io/omnition
        tag: 0.1.9
        resources:
          limits:
            cpu: "1"
            memory: 2Gi
          requests:
            cpu: 200m
            memory: 400Mi
        exporters:
          stackdriver:
            enable_tracing: true
      service:
        annotations: {}
        name: http-query
        type: ClusterIP
        externalPort: 9411
    istiocoredns:
      coreDNSImage: coredns/coredns
      coreDNSTag: 1.6.2
      coreDNSPluginImage: istio/coredns-plugin:0.2-istio-1.1

    kiali:
      hub: quay.io/kiali
      tag: v1.18
      contextPath: /kiali
      nodeSelector: {}
      podAntiAffinityLabelSelector: []
      podAntiAffinityTermLabelSelector: []
      dashboard:
        secretName: kiali
        usernameKey: username
        passphraseKey: passphrase
        viewOnlyMode: false
        grafanaURL:
        grafanaInClusterURL: http://grafana:3000
        jaegerURL:
        jaegerInClusterURL: http://tracing/jaeger
        auth:
          strategy: login
      prometheusNamespace:
      createDemoSecret: false
      security:
        enabled: false
        cert_file: /kiali-cert/cert-chain.pem
        private_key_file: /kiali-cert/key.pem
      service:
        annotations: {}

    # TODO: derive from operator API
    version: ""
    clusterResources: true
`)

func profilesDefaultYamlBytes() ([]byte, error) {
	return _profilesDefaultYaml, nil
}

func profilesDefaultYaml() (*asset, error) {
	bytes, err := profilesDefaultYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "profiles/default.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _profilesDemoYaml = []byte(`apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  meshConfig:
    accessLogFile: /dev/stdout
    disablePolicyChecks: false
  components:
    egressGateways:
    - name: istio-egressgateway
      enabled: true
      k8s:
        resources:
          requests:
            cpu: 10m
            memory: 40Mi

    ingressGateways:
    - name: istio-ingressgateway
      enabled: true
      k8s:
        resources:
          requests:
            cpu: 10m
            memory: 40Mi
        service:
          ports:
            ## You can add custom gateway ports in user values overrides, but it must include those ports since helm replaces.
            # Note that AWS ELB will by default perform health checks on the first port
            # on this list. Setting this to the health check port will ensure that health
            # checks always work. https://github.com/istio/istio/issues/12503
            - port: 15020
              targetPort: 15020
              name: status-port
            - port: 80
              targetPort: 8080
              name: http2
            - port: 443
              targetPort: 8443
              name: https
            - port: 31400
              targetPort: 31400
              name: tcp
              # This is the port where sni routing happens
            - port: 15443
              targetPort: 15443
              name: tls

    policy:
      enabled: false
      k8s:
        resources:
          requests:
            cpu: 10m
            memory: 100Mi

    telemetry:
      k8s:
        resources:
          requests:
            cpu: 50m
            memory: 100Mi

    pilot:
      k8s:
        env:
          - name: POD_NAME
            valueFrom:
              fieldRef:
                apiVersion: v1
                fieldPath: metadata.name
          - name: POD_NAMESPACE
            valueFrom:
              fieldRef:
                apiVersion: v1
                fieldPath: metadata.namespace
          - name: GODEBUG
            value: gctrace=1
          - name: PILOT_TRACE_SAMPLING
            value: "100"
          - name: CONFIG_NAMESPACE
            value: istio-config
        resources:
          requests:
            cpu: 10m
            memory: 100Mi

  addonComponents:
    kiali:
      enabled: true
    grafana:
      enabled: true
    tracing:
      enabled: true

  values:
    global:
      proxy:
        resources:
          requests:
            cpu: 10m
            memory: 40Mi

    pilot:
      autoscaleEnabled: false

    mixer:
      adapters:
        useAdapterCRDs: false
        kubernetesenv:
          enabled: true
        prometheus:
          enabled: true
          metricsExpiryDuration: 10m
        stackdriver:
          enabled: false
        stdio:
          enabled: true
          outputAsJson: false
      policy:
        autoscaleEnabled: false
      telemetry:
        autoscaleEnabled: false

    gateways:
      istio-egressgateway:
        autoscaleEnabled: false
      istio-ingressgateway:
        autoscaleEnabled: false
    kiali:
      createDemoSecret: true
`)

func profilesDemoYamlBytes() ([]byte, error) {
	return _profilesDemoYaml, nil
}

func profilesDemoYaml() (*asset, error) {
	bytes, err := profilesDemoYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "profiles/demo.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _profilesEmptyYaml = []byte(`# The empty profile has everything disabled
# This is useful as a base for custom user configuration
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  components:
    base:
      enabled: false
    pilot:
      enabled: false
    ingressGateways:

  addonComponents:
    prometheus:
      enabled: false
`)

func profilesEmptyYamlBytes() ([]byte, error) {
	return _profilesEmptyYaml, nil
}

func profilesEmptyYaml() (*asset, error) {
	bytes, err := profilesEmptyYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "profiles/empty.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _profilesMinimalYaml = []byte(`# The minimal profile will install just the core control plane
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  components:
    ingressGateways:

  addonComponents:
    prometheus:
      enabled: false

  values:
    global:
      mtls:
        auto: false
`)

func profilesMinimalYamlBytes() ([]byte, error) {
	return _profilesMinimalYaml, nil
}

func profilesMinimalYaml() (*asset, error) {
	bytes, err := profilesMinimalYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "profiles/minimal.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _profilesPreviewYaml = []byte(`# The preview profile contains features that are experimental.
# This is intended to explore new features coming to Istio.
# Stability, security, and performance are not guaranteed - use at your own risk.
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  values:
    telemetry:
      v2:
        metadataExchange:
          wasmEnabled: true
        prometheus:
          wasmEnabled: true`)

func profilesPreviewYamlBytes() ([]byte, error) {
	return _profilesPreviewYaml, nil
}

func profilesPreviewYaml() (*asset, error) {
	bytes, err := profilesPreviewYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "profiles/preview.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _profilesRemoteYaml = []byte(`apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  addonComponents:
    prometheus:
      enabled: false`)

func profilesRemoteYamlBytes() ([]byte, error) {
	return _profilesRemoteYaml, nil
}

func profilesRemoteYaml() (*asset, error) {
	bytes, err := profilesRemoteYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "profiles/remote.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigNames15Yaml = []byte(`DeprecatedComponentNames:
  - "Injector"
  - "CertManager"
  - "NodeAgent"
  - "SidecarInjector"

`)

func translateconfigNames15YamlBytes() ([]byte, error) {
	return _translateconfigNames15Yaml, nil
}

func translateconfigNames15Yaml() (*asset, error) {
	bytes, err := translateconfigNames15YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/names-1.5.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigNames16Yaml = []byte(`DeprecatedComponentNames:
  - "Injector"
  - "CertManager"
  - "NodeAgent"
  - "SidecarInjector"
`)

func translateconfigNames16YamlBytes() ([]byte, error) {
	return _translateconfigNames16Yaml, nil
}

func translateconfigNames16Yaml() (*asset, error) {
	bytes, err := translateconfigNames16YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/names-1.6.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigReversetranslateconfig14Yaml = []byte(`kubernetesPatternMapping:
  "{{.ValueComponentName}}.env":                   "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.Env"
  "{{.ValueComponentName}}.autoscaleEnabled":      "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.HpaSpec"
  "{{.ValueComponentName}}.imagePullPolicy":       "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.ImagePullPolicy"
  "{{.ValueComponentName}}.nodeSelector":          "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.NodeSelector"
  "{{.ValueComponentName}}.tolerations":           "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.Tolerations"
  "{{.ValueComponentName}}.podDisruptionBudget":   "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.PodDisruptionBudget"
  "{{.ValueComponentName}}.podAnnotations":        "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.PodAnnotations"
  "{{.ValueComponentName}}.priorityClassName":     "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.PriorityClassName"
  "{{.ValueComponentName}}.readinessProbe":        "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.ReadinessProbe"
  "{{.ValueComponentName}}.replicaCount":          "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.ReplicaCount"
  "{{.ValueComponentName}}.resources":             "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.Resources"
  "{{.ValueComponentName}}.rollingMaxSurge":       "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.Strategy"
  "{{.ValueComponentName}}.rollingMaxUnavailable": "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.Strategy"`)

func translateconfigReversetranslateconfig14YamlBytes() ([]byte, error) {
	return _translateconfigReversetranslateconfig14Yaml, nil
}

func translateconfigReversetranslateconfig14Yaml() (*asset, error) {
	bytes, err := translateconfigReversetranslateconfig14YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/reverseTranslateConfig-1.4.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigReversetranslateconfig15Yaml = []byte(`kubernetesPatternMapping:
  "{{.ValueComponentName}}.env":                   "Components.{{.ComponentName}}.K8s.Env"
  "{{.ValueComponentName}}.autoscaleEnabled":      "Components.{{.ComponentName}}.K8s.HpaSpec"
  "{{.ValueComponentName}}.imagePullPolicy":       "Components.{{.ComponentName}}.K8s.ImagePullPolicy"
  "{{.ValueComponentName}}.nodeSelector":          "Components.{{.ComponentName}}.K8s.NodeSelector"
  "{{.ValueComponentName}}.tolerations":           "Components.{{.ComponentName}}.K8s.Tolerations"
  "{{.ValueComponentName}}.podDisruptionBudget":   "Components.{{.ComponentName}}.K8s.PodDisruptionBudget"
  "{{.ValueComponentName}}.podAnnotations":        "Components.{{.ComponentName}}.K8s.PodAnnotations"
  "{{.ValueComponentName}}.priorityClassName":     "Components.{{.ComponentName}}.K8s.PriorityClassName"
  "{{.ValueComponentName}}.readinessProbe":        "Components.{{.ComponentName}}.K8s.ReadinessProbe"
  "{{.ValueComponentName}}.replicaCount":          "Components.{{.ComponentName}}.K8s.ReplicaCount"
  "{{.ValueComponentName}}.resources":             "Components.{{.ComponentName}}.K8s.Resources"
  "{{.ValueComponentName}}.rollingMaxSurge":       "Components.{{.ComponentName}}.K8s.Strategy"
  "{{.ValueComponentName}}.rollingMaxUnavailable": "Components.{{.ComponentName}}.K8s.Strategy"
  "{{.ValueComponentName}}.serviceAnnotations":    "Components.{{.ComponentName}}.K8s.ServiceAnnotations"`)

func translateconfigReversetranslateconfig15YamlBytes() ([]byte, error) {
	return _translateconfigReversetranslateconfig15Yaml, nil
}

func translateconfigReversetranslateconfig15Yaml() (*asset, error) {
	bytes, err := translateconfigReversetranslateconfig15YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/reverseTranslateConfig-1.5.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigReversetranslateconfig16Yaml = []byte(`kubernetesPatternMapping:
  "{{.ValueComponentName}}.env":                   "Components.{{.ComponentName}}.K8s.Env"
  "{{.ValueComponentName}}.autoscaleEnabled":      "Components.{{.ComponentName}}.K8s.HpaSpec"
  "{{.ValueComponentName}}.imagePullPolicy":       "Components.{{.ComponentName}}.K8s.ImagePullPolicy"
  "{{.ValueComponentName}}.nodeSelector":          "Components.{{.ComponentName}}.K8s.NodeSelector"
  "{{.ValueComponentName}}.tolerations":           "Components.{{.ComponentName}}.K8s.Tolerations"
  "{{.ValueComponentName}}.podDisruptionBudget":   "Components.{{.ComponentName}}.K8s.PodDisruptionBudget"
  "{{.ValueComponentName}}.podAnnotations":        "Components.{{.ComponentName}}.K8s.PodAnnotations"
  "{{.ValueComponentName}}.priorityClassName":     "Components.{{.ComponentName}}.K8s.PriorityClassName"
  "{{.ValueComponentName}}.readinessProbe":        "Components.{{.ComponentName}}.K8s.ReadinessProbe"
  "{{.ValueComponentName}}.replicaCount":          "Components.{{.ComponentName}}.K8s.ReplicaCount"
  "{{.ValueComponentName}}.resources":             "Components.{{.ComponentName}}.K8s.Resources"
  "{{.ValueComponentName}}.rollingMaxSurge":       "Components.{{.ComponentName}}.K8s.Strategy"
  "{{.ValueComponentName}}.rollingMaxUnavailable": "Components.{{.ComponentName}}.K8s.Strategy"
  "{{.ValueComponentName}}.serviceAnnotations":    "Components.{{.ComponentName}}.K8s.ServiceAnnotations"
`)

func translateconfigReversetranslateconfig16YamlBytes() ([]byte, error) {
	return _translateconfigReversetranslateconfig16Yaml, nil
}

func translateconfigReversetranslateconfig16Yaml() (*asset, error) {
	bytes, err := translateconfigReversetranslateconfig16YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/reverseTranslateConfig-1.6.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigTranslateconfig13Yaml = []byte(`apiMapping:
  Hub:
    outPath: "global.hub"
  Tag:
    outPath: "global.tag"
  K8SDefaults:
    outPath: "global.resources"
  DefaultNamespace:
    outPath: "global.istioNamespace"
  Values.Proxy:
    outPath: "global.proxy"
  ConfigManagement.Components.Namespace:
    outPath: "global.configNamespace"
  Policy.Components.Namespace:
    outPath: "global.policyNamespace"
  Telemetry.Components.Namespace:
    outPath: "global.telemetryNamespace"
  Security.Components.Namespace:
    outPath: "global.securityNamespace"
kubernetesMapping:
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Affinity":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.affinity"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Env":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].env"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.HpaSpec":
    outPath: "[HorizontalPodAutoscaler:{{.ResourceName}}].spec"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.ImagePullPolicy":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].imagePullPolicy"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.NodeSelector":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.nodeSelector"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.PodDisruptionBudget":
    outPath: "[PodDisruptionBudget:{{.ResourceName}}].spec"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.PodAnnotations":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.metadata.annotations"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.PriorityClassName":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.priorityClassName."
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.ReadinessProbe":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].readinessProbe"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.ReplicaCount":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.replicas"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Resources":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].resources"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Strategy":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.strategy"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Tolerations":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.tolerations"
toFeature:
    crds:               Base
    Pilot:              TrafficManagement
    Galley:             ConfigManagement
    Injector:           AutoInjection
    Policy:             Policy
    Telemetry:          Telemetry
    Citadel:            Security
    CertManager:        Security
    NodeAgent:          Security
    IngressGateway:     Gateways
    EgressGateway:      Gateways
    Cni:                Cni
    Grafana:            ThirdParty
    Prometheus:         ThirdParty
    Tracing:            ThirdParty
    PrometheusOperator: ThirdParty
    Kiali:              ThirdParty
globalNamespaces:
  Pilot:      "istioNamespace"
  Galley:     "configNamespace"
  Telemetry:  "telemetryNamespace"
  Policy:     "policyNamespace"
  Prometheus: "prometheusNamespace"
  Citadel:    "securityNamespace"
featureMaps:
  Base:
    alwaysEnabled: true
    Components:
      - crds
  TrafficManagement:
    Components:
      - Pilot
  Policy:
    Components:
      - Policy
  Telemetry:
    Components:
      - Telemetry
  Security:
    Components:
      - Citadel
      - CertManager
      - NodeAgent
  ConfigManagement:
    Components:
      - Galley
  AutoInjection:
    Components:
      - Injector
  Gateways:
    Components:
      - IngressGateway
      - EgressGateway
  Cni:
    Components:
      - Cni
  ThirdParty:
    Components:
      - Grafana
      - Prometheus
      - Tracing
      - PrometheusOperator
      - Kiali

componentMaps:
  crds:
    ToHelmValuesTreeRoot: "global"
    HelmSubdir:           "crds"
    AlwaysEnabled:        true
  Pilot:
    ResourceType:         "Deployment"
    ResourceName:         "istio-pilot"
    ContainerName:        "discovery"
    HelmSubdir:           "istio-control/istio-discovery"
    ToHelmValuesTreeRoot: "pilot"
  Galley:
    ResourceType:         "Deployment"
    ResourceName:         "istio-galley"
    ContainerName:        "galley"
    HelmSubdir:           "istio-control/istio-config"
    ToHelmValuesTreeRoot: "galley"
  Injector:
    ResourceType:         "Deployment"
    ResourceName:         "istio-sidecar-injector"
    ContainerName:        "sidecar-injector-webhook"
    HelmSubdir:           "istio-control/istio-autoinject"
    ToHelmValuesTreeRoot: "sidecarInjectorWebhook"
  Policy:
    ResourceType:         "Deployment"
    ResourceName:         "istio-policy"
    ContainerName:        "mixer"
    HelmSubdir:           "istio-policy"
    ToHelmValuesTreeRoot: "mixer.policy"
  Telemetry:
    ResourceType:        "Deployment"
    ResourceName:         "istio-telemetry"
    ContainerName:        "mixer"
    HelmSubdir:           "istio-telemetry/mixer-telemetry"
    ToHelmValuesTreeRoot: "mixer.telemetry"
  Citadel:
    ResourceType:        "Deployment"
    ResourceName:         "istio-citadel"
    ContainerName:        "citadel"
    HelmSubdir:           "security/citadel"
    ToHelmValuesTreeRoot: "security"
  NodeAgent:
    ResourceType:         "DaemonSet"
    ResourceName:         "istio-nodeagent"
    ContainerName:        "nodeagent"
    HelmSubdir:           "security/nodeagent"
    ToHelmValuesTreeRoot: "nodeagent"
  CertManager:
    ResourceType:        "Deployment"
    ResourceName:         "certmanager"
    ContainerName:        "certmanager"
    HelmSubdir:           "security/certmanager"
    ToHelmValuesTreeRoot: "certmanager"
  IngressGateway:
    ResourceType:         "Deployment"
    ResourceName:         "istio-ingressgateway"
    ContainerName:        "istio-proxy"
    HelmSubdir:           "gateways/istio-ingress"
    ToHelmValuesTreeRoot: "gateways.istio-ingressgateway"
  EgressGateway:
    ResourceType:         "Deployment"
    ResourceName:         "istio-egressgateway"
    ContainerName:        "istio-proxy"
    HelmSubdir:           "gateways/istio-egress"
    ToHelmValuesTreeRoot: "gateways.istio-egressgateway"
  Cni:
    ResourceType:         "DaemonSet"
    ResourceName:         "istio-cni-node"
    ContainerName:        "install-cni"
    HelmSubdir:           "istio-cni"
    ToHelmValuesTreeRoot: "cni"
  Tracing:
    ResourceType:         "Deployment"
    ResourceName:         "istio-tracing"
    ContainerName:        "jaeger"
    HelmSubdir:           "istio-telemetry/tracing"
    ToHelmValuesTreeRoot: "tracing.jaeger"
  PrometheusOperator:
    ResourceType:         "Deployment"
    ResourceName:         "prometheus"
    ContainerName:        "prometheus"
    HelmSubdir:           "istio-telemetry/prometheusOperator"
    ToHelmValuesTreeRoot: "prometheus"
  Kiali:
    ResourceType:         "Deployment"
    ResourceName:         "kiali"
    ContainerName:        "kiali"
    HelmSubdir:           "istio-telemetry/kiali"
    ToHelmValuesTreeRoot: "kiali"
  Grafana:
    ResourceType:        "Deployment"
    ResourceName:         "grafana"
    ContainerName:        "grafana"
    HelmSubdir:           "istio-telemetry/grafana"
    ToHelmValuesTreeRoot: "grafana"
  Prometheus:
    ResourceType:         "Deployment"
    ResourceName:         "prometheus"
    ContainerName:        "prometheus"
    HelmSubdir:           "istio-telemetry/prometheus"
    ToHelmValuesTreeRoot: "prometheus"
`)

func translateconfigTranslateconfig13YamlBytes() ([]byte, error) {
	return _translateconfigTranslateconfig13Yaml, nil
}

func translateconfigTranslateconfig13Yaml() (*asset, error) {
	bytes, err := translateconfigTranslateconfig13YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/translateConfig-1.3.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigTranslateconfig14Yaml = []byte(`apiMapping:
  Hub:
    outPath: "global.hub"
  Tag:
    outPath: "global.tag"
  K8SDefaults:
    outPath: "global.resources"
  DefaultNamespace:
    outPath: "global.istioNamespace"
  ConfigManagement.Components.Namespace:
    outPath: "global.configNamespace"
  Policy.Components.Namespace:
    outPath: "global.policyNamespace"
  Telemetry.Components.Namespace:
    outPath: "global.telemetryNamespace"
  Security.Components.Namespace:
    outPath: "global.securityNamespace"
kubernetesMapping:
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Affinity":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.affinity"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Env":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].env"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.HpaSpec":
    outPath: "[HorizontalPodAutoscaler:{{.ResourceName}}].spec"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.ImagePullPolicy":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].imagePullPolicy"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.NodeSelector":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.nodeSelector"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.PodDisruptionBudget":
    outPath: "[PodDisruptionBudget:{{.ResourceName}}].spec"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.PodAnnotations":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.metadata.annotations"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.PriorityClassName":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.priorityClassName."
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.ReadinessProbe":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].readinessProbe"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.ReplicaCount":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.replicas"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Resources":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].resources"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Strategy":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.strategy"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Tolerations":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.tolerations"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Service":
    outPath: "[Service:{{.ResourceName}}].spec"
toFeature:
    Base:               Base
    Pilot:              TrafficManagement
    Galley:             ConfigManagement
    Injector:           AutoInjection
    Policy:             Policy
    Telemetry:          Telemetry
    Citadel:            Security
    CertManager:        Security
    NodeAgent:          Security
    IngressGateway:     Gateways
    EgressGateway:      Gateways
    Cni:                Cni
    CoreDNS:            CoreDNS
    Grafana:            ThirdParty
    Prometheus:         ThirdParty
    Tracing:            ThirdParty
    PrometheusOperator: ThirdParty
    Kiali:              ThirdParty
globalNamespaces:
  Pilot:      "istioNamespace"
  Galley:     "configNamespace"
  Telemetry:  "telemetryNamespace"
  Policy:     "policyNamespace"
  Prometheus: "prometheusNamespace"
  Citadel:    "securityNamespace"
featureMaps:
  Base:
    Components:
      - Base
  TrafficManagement:
    Components:
      - Pilot
  Policy:
    Components:
      - Policy
  Telemetry:
    Components:
      - Telemetry
  Security:
    Components:
      - Citadel
      - CertManager
      - NodeAgent
  ConfigManagement:
    Components:
      - Galley
  AutoInjection:
    Components:
      - Injector
  Gateways:
    Components:
      - IngressGateway
      - EgressGateway
  Cni:
    Components:
      - Cni
  CoreDNS:
    Components:
      - CoreDNS
  ThirdParty:
    Components:
      - Grafana
      - Prometheus
      - Tracing
      - PrometheusOperator
      - Kiali

componentMaps:
  Base:
    ToHelmValuesTreeRoot: "global"
    HelmSubdir:           "base"
  Pilot:
    ResourceType:         "Deployment"
    ResourceName:         "istio-pilot"
    ContainerName:        "discovery"
    HelmSubdir:           "istio-control/istio-discovery"
    ToHelmValuesTreeRoot: "pilot"
  Galley:
    ResourceType:         "Deployment"
    ResourceName:         "istio-galley"
    ContainerName:        "galley"
    HelmSubdir:           "istio-control/istio-config"
    ToHelmValuesTreeRoot: "galley"
  Injector:
    ResourceType:         "Deployment"
    ResourceName:         "istio-sidecar-injector"
    ContainerName:        "sidecar-injector-webhook"
    HelmSubdir:           "istio-control/istio-autoinject"
    ToHelmValuesTreeRoot: "sidecarInjectorWebhook"
  Policy:
    ResourceType:         "Deployment"
    ResourceName:         "istio-policy"
    ContainerName:        "mixer"
    HelmSubdir:           "istio-policy"
    ToHelmValuesTreeRoot: "mixer.policy"
  Telemetry:
    ResourceType:        "Deployment"
    ResourceName:         "istio-telemetry"
    ContainerName:        "mixer"
    HelmSubdir:           "istio-telemetry/mixer-telemetry"
    ToHelmValuesTreeRoot: "mixer.telemetry"
  Citadel:
    ResourceType:        "Deployment"
    ResourceName:         "istio-citadel"
    ContainerName:        "citadel"
    HelmSubdir:           "security/citadel"
    ToHelmValuesTreeRoot: "security"
  NodeAgent:
    ResourceType:         "DaemonSet"
    ResourceName:         "istio-nodeagent"
    ContainerName:        "nodeagent"
    HelmSubdir:           "security/nodeagent"
    ToHelmValuesTreeRoot: "nodeagent"
  CertManager:
    ResourceType:        "Deployment"
    ResourceName:         "certmanager"
    ContainerName:        "certmanager"
    HelmSubdir:           "security/certmanager"
    ToHelmValuesTreeRoot: "certmanager"
  IngressGateway:
    ResourceType:         "Deployment"
    ResourceName:         "istio-ingressgateway"
    ContainerName:        "istio-proxy"
    HelmSubdir:           "gateways/istio-ingress"
    ToHelmValuesTreeRoot: "gateways.istio-ingressgateway"
  EgressGateway:
    ResourceType:         "Deployment"
    ResourceName:         "istio-egressgateway"
    ContainerName:        "istio-proxy"
    HelmSubdir:           "gateways/istio-egress"
    ToHelmValuesTreeRoot: "gateways.istio-egressgateway"
  Cni:
    ResourceType:         "DaemonSet"
    ResourceName:         "istio-cni-node"
    ContainerName:        "install-cni"
    HelmSubdir:           "istio-cni"
    ToHelmValuesTreeRoot: "cni"
  CoreDNS:
    ResourceType:         "Deployment"
    ResourceName:         "istiocoredns"
    ContainerName:        "coredns"
    HelmSubdir:           "istiocoredns"
    ToHelmValuesTreeRoot: "istiocoredns"
  Tracing:
    ResourceType:         "Deployment"
    ResourceName:         "istio-tracing"
    ContainerName:        "jaeger"
    HelmSubdir:           "istio-telemetry/tracing"
    ToHelmValuesTreeRoot: "tracing.jaeger"
  PrometheusOperator:
    ResourceType:         "Deployment"
    ResourceName:         "prometheus"
    ContainerName:        "prometheus"
    HelmSubdir:           "istio-telemetry/prometheusOperator"
    ToHelmValuesTreeRoot: "prometheus"
  Kiali:
    ResourceType:         "Deployment"
    ResourceName:         "kiali"
    ContainerName:        "kiali"
    HelmSubdir:           "istio-telemetry/kiali"
    ToHelmValuesTreeRoot: "kiali"
  Grafana:
    ResourceType:        "Deployment"
    ResourceName:         "grafana"
    ContainerName:        "grafana"
    HelmSubdir:           "istio-telemetry/grafana"
    ToHelmValuesTreeRoot: "grafana"
  Prometheus:
    ResourceType:         "Deployment"
    ResourceName:         "prometheus"
    ContainerName:        "prometheus"
    HelmSubdir:           "istio-telemetry/prometheus"
    ToHelmValuesTreeRoot: "prometheus"
`)

func translateconfigTranslateconfig14YamlBytes() ([]byte, error) {
	return _translateconfigTranslateconfig14Yaml, nil
}

func translateconfigTranslateconfig14Yaml() (*asset, error) {
	bytes, err := translateconfigTranslateconfig14YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/translateConfig-1.4.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigTranslateconfig15Yaml = []byte(`apiMapping:
  Hub:
    outPath: "global.hub"
  Tag:
    outPath: "global.tag"
  K8SDefaults:
    outPath: "global.resources"
  MeshConfig.rootNamespace:
    outPath: "global.istioNamespace"
  Revision:
    outPath: "revision"
kubernetesMapping:
  "Components.{{.ComponentName}}.K8S.Affinity":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.affinity"
  "Components.{{.ComponentName}}.K8S.Env":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].env"
  "Components.{{.ComponentName}}.K8S.HpaSpec":
    outPath: "[HorizontalPodAutoscaler:{{.ResourceName}}].spec"
  "Components.{{.ComponentName}}.K8S.ImagePullPolicy":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].imagePullPolicy"
  "Components.{{.ComponentName}}.K8S.NodeSelector":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.nodeSelector"
  "Components.{{.ComponentName}}.K8S.PodDisruptionBudget":
    outPath: "[PodDisruptionBudget:{{.ResourceName}}].spec"
  "Components.{{.ComponentName}}.K8S.PodAnnotations":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.metadata.annotations"
  "Components.{{.ComponentName}}.K8S.PriorityClassName":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.priorityClassName."
  "Components.{{.ComponentName}}.K8S.ReadinessProbe":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].readinessProbe"
  "Components.{{.ComponentName}}.K8S.ReplicaCount":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.replicas"
  "Components.{{.ComponentName}}.K8S.Resources":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].resources"
  "Components.{{.ComponentName}}.K8S.Strategy":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.strategy"
  "Components.{{.ComponentName}}.K8S.Tolerations":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.tolerations"
  "Components.{{.ComponentName}}.K8S.ServiceAnnotations":
    outPath: "[Service:{{.ResourceName}}].metadata.annotations"
  "Components.{{.ComponentName}}.K8S.Service":
    outPath: "[Service:{{.ResourceName}}].spec"
globalNamespaces:
  Pilot:      "istioNamespace"
  Galley:     "configNamespace"
  Telemetry:  "telemetryNamespace"
  Policy:     "policyNamespace"
  Prometheus: "prometheusNamespace"
  Citadel:    "securityNamespace"

componentMaps:
  Base:
    ToHelmValuesTreeRoot: "global"
    HelmSubdir:           "base"
    SkipReverseTranslate: true
  Pilot:
    ResourceType:         "Deployment"
    ResourceName:         "istiod"
    ContainerName:        "discovery"
    HelmSubdir:           "istio-control/istio-discovery"
    ToHelmValuesTreeRoot: "pilot"
  Galley:
    ResourceType:         "Deployment"
    ResourceName:         "istio-galley"
    ContainerName:        "galley"
    HelmSubdir:           "istio-control/istio-config"
    ToHelmValuesTreeRoot: "galley"
  SidecarInjector:
    ResourceType:         "Deployment"
    ResourceName:         "istio-sidecar-injector"
    ContainerName:        "sidecar-injector-webhook"
    HelmSubdir:           "istio-control/istio-autoinject"
    ToHelmValuesTreeRoot: "sidecarInjectorWebhook"
    SkipReverseTranslate: true
  Policy:
    ResourceType:         "Deployment"
    ResourceName:         "istio-policy"
    ContainerName:        "mixer"
    HelmSubdir:           "istio-policy"
    ToHelmValuesTreeRoot: "mixer.policy"
  Telemetry:
    ResourceType:        "Deployment"
    ResourceName:         "istio-telemetry"
    ContainerName:        "mixer"
    HelmSubdir:           "istio-telemetry/mixer-telemetry"
    ToHelmValuesTreeRoot: "mixer.telemetry"
  Citadel:
    ResourceType:        "Deployment"
    ResourceName:         "istio-citadel"
    ContainerName:        "citadel"
    HelmSubdir:           "security/citadel"
    ToHelmValuesTreeRoot: "security"
  NodeAgent:
    ResourceType:         "DaemonSet"
    ResourceName:         "istio-nodeagent"
    ContainerName:        "nodeagent"
    HelmSubdir:           "security/nodeagent"
    ToHelmValuesTreeRoot: "nodeagent"
    SkipReverseTranslate: true
  CertManager:
    ResourceType:        "Deployment"
    ResourceName:         "certmanager"
    ContainerName:        "certmanager"
    HelmSubdir:           "security/certmanager"
    ToHelmValuesTreeRoot: "certmanager"
    SkipReverseTranslate: true
  IngressGateways:
    ResourceType:         "Deployment"
    ResourceName:         "istio-ingressgateway"
    ContainerName:        "istio-proxy"
    HelmSubdir:           "gateways/istio-ingress"
    ToHelmValuesTreeRoot: "gateways.istio-ingressgateway"
  EgressGateways:
    ResourceType:         "Deployment"
    ResourceName:         "istio-egressgateway"
    ContainerName:        "istio-proxy"
    HelmSubdir:           "gateways/istio-egress"
    ToHelmValuesTreeRoot: "gateways.istio-egressgateway"
  Cni:
    ResourceType:         "DaemonSet"
    ResourceName:         "istio-cni-node"
    ContainerName:        "install-cni"
    HelmSubdir:           "istio-cni"
    ToHelmValuesTreeRoot: "cni"
  Istiocoredns:
    ResourceType:         "Deployment"
    ResourceName:         "istiocoredns"
    ContainerName:        "coredns"
    HelmSubdir:           "istiocoredns"
    ToHelmValuesTreeRoot: "istiocoredns"
  Tracing:
    ResourceType:         "Deployment"
    ResourceName:         "istio-tracing"
    ContainerName:        "jaeger"
    HelmSubdir:           "istio-telemetry/tracing"
    ToHelmValuesTreeRoot: "tracing.jaeger"
  PrometheusOperator:
    ResourceType:         "Deployment"
    ResourceName:         "prometheus"
    ContainerName:        "prometheus"
    HelmSubdir:           "istio-telemetry/prometheusOperator"
    ToHelmValuesTreeRoot: "prometheus"
    SkipReverseTranslate: true
  Kiali:
    ResourceType:         "Deployment"
    ResourceName:         "kiali"
    ContainerName:        "kiali"
    HelmSubdir:           "istio-telemetry/kiali"
    ToHelmValuesTreeRoot: "kiali"
  Grafana:
    ResourceType:        "Deployment"
    ResourceName:         "grafana"
    ContainerName:        "grafana"
    HelmSubdir:           "istio-telemetry/grafana"
    ToHelmValuesTreeRoot: "grafana"
  Prometheus:
    ResourceType:         "Deployment"
    ResourceName:         "prometheus"
    ContainerName:        "prometheus"
    HelmSubdir:           "istio-telemetry/prometheus"
    ToHelmValuesTreeRoot: "prometheus"
`)

func translateconfigTranslateconfig15YamlBytes() ([]byte, error) {
	return _translateconfigTranslateconfig15Yaml, nil
}

func translateconfigTranslateconfig15Yaml() (*asset, error) {
	bytes, err := translateconfigTranslateconfig15YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/translateConfig-1.5.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigTranslateconfig16Yaml = []byte(`apiMapping:
  Hub:
    outPath: "global.hub"
  Tag:
    outPath: "global.tag"
  K8SDefaults:
    outPath: "global.resources"
  Revision:
    outPath: "revision"
  MeshConfig:
    outPath: "meshConfig"
kubernetesMapping:
  "Components.{{.ComponentName}}.K8S.Affinity":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.affinity"
  "Components.{{.ComponentName}}.K8S.Env":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].env"
  "Components.{{.ComponentName}}.K8S.HpaSpec":
    outPath: "[HorizontalPodAutoscaler:{{.ResourceName}}].spec"
  "Components.{{.ComponentName}}.K8S.ImagePullPolicy":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].imagePullPolicy"
  "Components.{{.ComponentName}}.K8S.NodeSelector":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.nodeSelector"
  "Components.{{.ComponentName}}.K8S.PodDisruptionBudget":
    outPath: "[PodDisruptionBudget:{{.ResourceName}}].spec"
  "Components.{{.ComponentName}}.K8S.PodAnnotations":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.metadata.annotations"
  "Components.{{.ComponentName}}.K8S.PriorityClassName":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.priorityClassName."
  "Components.{{.ComponentName}}.K8S.ReadinessProbe":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].readinessProbe"
  "Components.{{.ComponentName}}.K8S.ReplicaCount":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.replicas"
  "Components.{{.ComponentName}}.K8S.Resources":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].resources"
  "Components.{{.ComponentName}}.K8S.Strategy":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.strategy"
  "Components.{{.ComponentName}}.K8S.Tolerations":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.tolerations"
  "Components.{{.ComponentName}}.K8S.ServiceAnnotations":
    outPath: "[Service:{{.ResourceName}}].metadata.annotations"
  "Components.{{.ComponentName}}.K8S.Service":
    outPath: "[Service:{{.ResourceName}}].spec"
globalNamespaces:
  Pilot:      "istioNamespace"
  Galley:     "configNamespace"
  Telemetry:  "telemetryNamespace"
  Policy:     "policyNamespace"
  Prometheus: "prometheusNamespace"
  Citadel:    "securityNamespace"

componentMaps:
  Base:
    ToHelmValuesTreeRoot: "global"
    HelmSubdir:           "base"
    SkipReverseTranslate: true
  Pilot:
    ResourceType:         "Deployment"
    ResourceName:         "istiod"
    ContainerName:        "discovery"
    HelmSubdir:           "istio-control/istio-discovery"
    ToHelmValuesTreeRoot: "pilot"
  Galley:
    ResourceType:         "Deployment"
    ResourceName:         "istio-galley"
    ContainerName:        "galley"
    HelmSubdir:           "istio-control/istio-config"
    ToHelmValuesTreeRoot: "galley"
  SidecarInjector:
    ResourceType:         "Deployment"
    ResourceName:         "istio-sidecar-injector"
    ContainerName:        "sidecar-injector-webhook"
    HelmSubdir:           "istio-control/istio-autoinject"
    ToHelmValuesTreeRoot: "sidecarInjectorWebhook"
    SkipReverseTranslate: true
  Policy:
    ResourceType:         "Deployment"
    ResourceName:         "istio-policy"
    ContainerName:        "mixer"
    HelmSubdir:           "istio-policy"
    ToHelmValuesTreeRoot: "mixer.policy"
  Telemetry:
    ResourceType:        "Deployment"
    ResourceName:         "istio-telemetry"
    ContainerName:        "mixer"
    HelmSubdir:           "istio-telemetry/mixer-telemetry"
    ToHelmValuesTreeRoot: "mixer.telemetry"
  Citadel:
    ResourceType:        "Deployment"
    ResourceName:         "istio-citadel"
    ContainerName:        "citadel"
    HelmSubdir:           "security/citadel"
    ToHelmValuesTreeRoot: "security"
  NodeAgent:
    ResourceType:         "DaemonSet"
    ResourceName:         "istio-nodeagent"
    ContainerName:        "nodeagent"
    HelmSubdir:           "security/nodeagent"
    ToHelmValuesTreeRoot: "nodeagent"
    SkipReverseTranslate: true
  IngressGateways:
    ResourceType:         "Deployment"
    ResourceName:         "istio-ingressgateway"
    ContainerName:        "istio-proxy"
    HelmSubdir:           "gateways/istio-ingress"
    ToHelmValuesTreeRoot: "gateways.istio-ingressgateway"
  EgressGateways:
    ResourceType:         "Deployment"
    ResourceName:         "istio-egressgateway"
    ContainerName:        "istio-proxy"
    HelmSubdir:           "gateways/istio-egress"
    ToHelmValuesTreeRoot: "gateways.istio-egressgateway"
  Cni:
    ResourceType:         "DaemonSet"
    ResourceName:         "istio-cni-node"
    ContainerName:        "install-cni"
    HelmSubdir:           "istio-cni"
    ToHelmValuesTreeRoot: "cni"
  IstiodRemote:
    HelmSubdir:           "istiod-remote"
    ToHelmValuesTreeRoot: "global"
    SkipReverseTranslate: true
  Istiocoredns:
    ResourceType:         "Deployment"
    ResourceName:         "istiocoredns"
    ContainerName:        "coredns"
    HelmSubdir:           "istiocoredns"
    ToHelmValuesTreeRoot: "istiocoredns"
  Tracing:
    ResourceType:         "Deployment"
    ResourceName:         "istio-tracing"
    ContainerName:        "jaeger"
    HelmSubdir:           "istio-telemetry/tracing"
    ToHelmValuesTreeRoot: "tracing.jaeger"
  PrometheusOperator:
    ResourceType:         "Deployment"
    ResourceName:         "prometheus"
    ContainerName:        "prometheus"
    HelmSubdir:           "istio-telemetry/prometheusOperator"
    ToHelmValuesTreeRoot: "prometheus"
    SkipReverseTranslate: true
  Kiali:
    ResourceType:         "Deployment"
    ResourceName:         "kiali"
    ContainerName:        "kiali"
    HelmSubdir:           "istio-telemetry/kiali"
    ToHelmValuesTreeRoot: "kiali"
  Grafana:
    ResourceType:        "Deployment"
    ResourceName:         "grafana"
    ContainerName:        "grafana"
    HelmSubdir:           "istio-telemetry/grafana"
    ToHelmValuesTreeRoot: "grafana"
  Prometheus:
    ResourceType:         "Deployment"
    ResourceName:         "prometheus"
    ContainerName:        "prometheus"
    HelmSubdir:           "istio-telemetry/prometheus"
    ToHelmValuesTreeRoot: "prometheus"
`)

func translateconfigTranslateconfig16YamlBytes() ([]byte, error) {
	return _translateconfigTranslateconfig16Yaml, nil
}

func translateconfigTranslateconfig16Yaml() (*asset, error) {
	bytes, err := translateconfigTranslateconfig16YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/translateConfig-1.6.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _versionsYaml = []byte(`- operatorVersion: 1.3.0
  supportedIstioVersions: 1.3.0
  recommendedIstioVersions: 1.3.0
- operatorVersion: 1.3.1
  supportedIstioVersions: ">=1.3.0,<=1.3.1"
  recommendedIstioVersions: 1.3.1
- operatorVersion: 1.3.2
  supportedIstioVersions: ">=1.3.0,<=1.3.2"
  recommendedIstioVersions: 1.3.2
- operatorVersion: 1.3.3
  supportedIstioVersions: ">=1.3.0,<=1.3.3"
  recommendedIstioVersions: 1.3.3
- operatorVersion: 1.3.4
  supportedIstioVersions: ">=1.3.0,<=1.3.4"
  recommendedIstioVersions: 1.3.4
- operatorVersion: 1.3.5
  supportedIstioVersions: ">=1.3.0,<=1.3.5"
  recommendedIstioVersions: 1.3.5
- operatorVersion: 1.3.6
  supportedIstioVersions: ">=1.3.0,<=1.3.6"
  recommendedIstioVersions: 1.3.6
- operatorVersion: 1.3.7
  operatorVersionRange: ">=1.3.7,<1.4.0"
  supportedIstioVersions: ">=1.3.0,<1.4.0"
  recommendedIstioVersions: 1.3.7
- operatorVersion: 1.4.0
  supportedIstioVersions: ">=1.3.3, <1.6"
  recommendedIstioVersions: 1.4.0
- operatorVersion: 1.4.1
  supportedIstioVersions: ">=1.3.3, <1.6"
  recommendedIstioVersions: 1.4.1
- operatorVersion: 1.4.2
  supportedIstioVersions: ">=1.3.3, <1.6"
  recommendedIstioVersions: 1.4.2
- operatorVersion: 1.4.3
  operatorVersionRange: ">=1.4.3,<1.5.0"
  supportedIstioVersions: ">=1.3.3, <1.6"
  recommendedIstioVersions: 1.4.3
- operatorVersion: 1.4.4
  operatorVersionRange: ">=1.4.4,<1.5.0"
  supportedIstioVersions: ">=1.3.3, <1.6"
  recommendedIstioVersions: 1.4.4
- operatorVersion: 1.5.0
  operatorVersionRange: ">=1.5.0,<1.6.0"
  supportedIstioVersions: ">=1.4.0, <1.6"
  recommendedIstioVersions: 1.5.0
  k8sClientVersionRange: ">=1.14"
  k8sServerVersionRange: ">=1.14"
- operatorVersion: 1.6.0
  operatorVersionRange: ">=1.6.0,<1.7.0"
  supportedIstioVersions: ">=1.5.0, <1.7"
  recommendedIstioVersions: 1.6.0
  k8sClientVersionRange: ">=1.14"
  k8sServerVersionRange: ">=1.14"
`)

func versionsYamlBytes() ([]byte, error) {
	return _versionsYaml, nil
}

func versionsYaml() (*asset, error) {
	bytes, err := versionsYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "versions.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

// Asset loads and returns the asset for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func Asset(name string) ([]byte, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("Asset %s can't read by error: %v", name, err)
		}
		return a.bytes, nil
	}
	return nil, fmt.Errorf("Asset %s not found", name)
}

// MustAsset is like Asset but panics when Asset would return an error.
// It simplifies safe initialization of global variables.
func MustAsset(name string) []byte {
	a, err := Asset(name)
	if err != nil {
		panic("asset: Asset(" + name + "): " + err.Error())
	}

	return a
}

// AssetInfo loads and returns the asset info for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func AssetInfo(name string) (os.FileInfo, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("AssetInfo %s can't read by error: %v", name, err)
		}
		return a.info, nil
	}
	return nil, fmt.Errorf("AssetInfo %s not found", name)
}

// AssetNames returns the names of the assets.
func AssetNames() []string {
	names := make([]string, 0, len(_bindata))
	for name := range _bindata {
		names = append(names, name)
	}
	return names
}

// _bindata is a table, holding each asset generator, mapped to its name.
var _bindata = map[string]func() (*asset, error){
	"examples/customresource/istio_v1alpha1_istiooperator_cr.yaml":  examplesCustomresourceIstio_v1alpha1_istiooperator_crYaml,
	"examples/multicluster/values-istio-multicluster-gateways.yaml": examplesMulticlusterValuesIstioMulticlusterGatewaysYaml,
	"examples/multicluster/values-istio-multicluster-primary.yaml":  examplesMulticlusterValuesIstioMulticlusterPrimaryYaml,
	"examples/user-gateway/ingress-gateway-only.yaml":               examplesUserGatewayIngressGatewayOnlyYaml,
	"examples/vm/values-istio-meshexpansion-gateways.yaml":          examplesVmValuesIstioMeshexpansionGatewaysYaml,
	"examples/vm/values-istio-meshexpansion.yaml":                   examplesVmValuesIstioMeshexpansionYaml,
	"profiles/default.yaml":                                         profilesDefaultYaml,
	"profiles/demo.yaml":                                            profilesDemoYaml,
	"profiles/empty.yaml":                                           profilesEmptyYaml,
	"profiles/minimal.yaml":                                         profilesMinimalYaml,
	"profiles/preview.yaml":                                         profilesPreviewYaml,
	"profiles/remote.yaml":                                          profilesRemoteYaml,
	"translateConfig/names-1.5.yaml":                                translateconfigNames15Yaml,
	"translateConfig/names-1.6.yaml":                                translateconfigNames16Yaml,
	"translateConfig/reverseTranslateConfig-1.4.yaml":               translateconfigReversetranslateconfig14Yaml,
	"translateConfig/reverseTranslateConfig-1.5.yaml":               translateconfigReversetranslateconfig15Yaml,
	"translateConfig/reverseTranslateConfig-1.6.yaml":               translateconfigReversetranslateconfig16Yaml,
	"translateConfig/translateConfig-1.3.yaml":                      translateconfigTranslateconfig13Yaml,
	"translateConfig/translateConfig-1.4.yaml":                      translateconfigTranslateconfig14Yaml,
	"translateConfig/translateConfig-1.5.yaml":                      translateconfigTranslateconfig15Yaml,
	"translateConfig/translateConfig-1.6.yaml":                      translateconfigTranslateconfig16Yaml,
	"versions.yaml":                                                 versionsYaml,
}

// AssetDir returns the file names below a certain
// directory embedded in the file by go-bindata.
// For example if you run go-bindata on data/... and data contains the
// following hierarchy:
//     data/
//       foo.txt
//       img/
//         a.png
//         b.png
// then AssetDir("data") would return []string{"foo.txt", "img"}
// AssetDir("data/img") would return []string{"a.png", "b.png"}
// AssetDir("foo.txt") and AssetDir("notexist") would return an error
// AssetDir("") will return []string{"data"}.
func AssetDir(name string) ([]string, error) {
	node := _bintree
	if len(name) != 0 {
		cannonicalName := strings.Replace(name, "\\", "/", -1)
		pathList := strings.Split(cannonicalName, "/")
		for _, p := range pathList {
			node = node.Children[p]
			if node == nil {
				return nil, fmt.Errorf("Asset %s not found", name)
			}
		}
	}
	if node.Func != nil {
		return nil, fmt.Errorf("Asset %s not found", name)
	}
	rv := make([]string, 0, len(node.Children))
	for childName := range node.Children {
		rv = append(rv, childName)
	}
	return rv, nil
}

type bintree struct {
	Func     func() (*asset, error)
	Children map[string]*bintree
}

var _bintree = &bintree{nil, map[string]*bintree{
	"examples": &bintree{nil, map[string]*bintree{
		"customresource": &bintree{nil, map[string]*bintree{
			"istio_v1alpha1_istiooperator_cr.yaml": &bintree{examplesCustomresourceIstio_v1alpha1_istiooperator_crYaml, map[string]*bintree{}},
		}},
		"multicluster": &bintree{nil, map[string]*bintree{
			"values-istio-multicluster-gateways.yaml": &bintree{examplesMulticlusterValuesIstioMulticlusterGatewaysYaml, map[string]*bintree{}},
			"values-istio-multicluster-primary.yaml":  &bintree{examplesMulticlusterValuesIstioMulticlusterPrimaryYaml, map[string]*bintree{}},
		}},
		"user-gateway": &bintree{nil, map[string]*bintree{
			"ingress-gateway-only.yaml": &bintree{examplesUserGatewayIngressGatewayOnlyYaml, map[string]*bintree{}},
		}},
		"vm": &bintree{nil, map[string]*bintree{
			"values-istio-meshexpansion-gateways.yaml": &bintree{examplesVmValuesIstioMeshexpansionGatewaysYaml, map[string]*bintree{}},
			"values-istio-meshexpansion.yaml":          &bintree{examplesVmValuesIstioMeshexpansionYaml, map[string]*bintree{}},
		}},
	}},
	"profiles": &bintree{nil, map[string]*bintree{
		"default.yaml": &bintree{profilesDefaultYaml, map[string]*bintree{}},
		"demo.yaml":    &bintree{profilesDemoYaml, map[string]*bintree{}},
		"empty.yaml":   &bintree{profilesEmptyYaml, map[string]*bintree{}},
		"minimal.yaml": &bintree{profilesMinimalYaml, map[string]*bintree{}},
		"preview.yaml": &bintree{profilesPreviewYaml, map[string]*bintree{}},
		"remote.yaml":  &bintree{profilesRemoteYaml, map[string]*bintree{}},
	}},
	"translateConfig": &bintree{nil, map[string]*bintree{
		"names-1.5.yaml":                  &bintree{translateconfigNames15Yaml, map[string]*bintree{}},
		"names-1.6.yaml":                  &bintree{translateconfigNames16Yaml, map[string]*bintree{}},
		"reverseTranslateConfig-1.4.yaml": &bintree{translateconfigReversetranslateconfig14Yaml, map[string]*bintree{}},
		"reverseTranslateConfig-1.5.yaml": &bintree{translateconfigReversetranslateconfig15Yaml, map[string]*bintree{}},
		"reverseTranslateConfig-1.6.yaml": &bintree{translateconfigReversetranslateconfig16Yaml, map[string]*bintree{}},
		"translateConfig-1.3.yaml":        &bintree{translateconfigTranslateconfig13Yaml, map[string]*bintree{}},
		"translateConfig-1.4.yaml":        &bintree{translateconfigTranslateconfig14Yaml, map[string]*bintree{}},
		"translateConfig-1.5.yaml":        &bintree{translateconfigTranslateconfig15Yaml, map[string]*bintree{}},
		"translateConfig-1.6.yaml":        &bintree{translateconfigTranslateconfig16Yaml, map[string]*bintree{}},
	}},
	"versions.yaml": &bintree{versionsYaml, map[string]*bintree{}},
}}

// RestoreAsset restores an asset under the given directory
func RestoreAsset(dir, name string) error {
	data, err := Asset(name)
	if err != nil {
		return err
	}
	info, err := AssetInfo(name)
	if err != nil {
		return err
	}
	err = os.MkdirAll(_filePath(dir, filepath.Dir(name)), os.FileMode(0755))
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(_filePath(dir, name), data, info.Mode())
	if err != nil {
		return err
	}
	err = os.Chtimes(_filePath(dir, name), info.ModTime(), info.ModTime())
	if err != nil {
		return err
	}
	return nil
}

// RestoreAssets restores an asset under the given directory recursively
func RestoreAssets(dir, name string) error {
	children, err := AssetDir(name)
	// File
	if err != nil {
		return RestoreAsset(dir, name)
	}
	// Dir
	for _, child := range children {
		err = RestoreAssets(dir, filepath.Join(name, child))
		if err != nil {
			return err
		}
	}
	return nil
}

func _filePath(dir, name string) string {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	return filepath.Join(append([]string{dir}, strings.Split(cannonicalName, "/")...)...)
}

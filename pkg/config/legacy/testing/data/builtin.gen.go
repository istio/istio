// Code generated for package data by go-bindata DO NOT EDIT. (@generated)
// sources:
// builtin/deployment.yaml
// builtin/endpoints.yaml
// builtin/ingress.yaml
// builtin/namespace.yaml
// builtin/node.yaml
// builtin/pod.yaml
// builtin/service.yaml
package data

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

var _builtinDeploymentYaml = []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: httpbin
spec:
  replicas: 1
  selector:
    matchLabels:
      app: httpbin
      version: v1
  template:
    metadata:
      labels:
        app: httpbin
        version: v1
    spec:
      containers:
      - image: docker.io/kennethreitz/httpbin
        imagePullPolicy: IfNotPresent
        name: httpbin
        ports:
        - containerPort: 80
`)

func builtinDeploymentYamlBytes() ([]byte, error) {
	return _builtinDeploymentYaml, nil
}

func builtinDeploymentYaml() (*asset, error) {
	bytes, err := builtinDeploymentYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "builtin/deployment.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _builtinEndpointsYaml = []byte(`apiVersion: v1
kind: Endpoints
metadata:
  creationTimestamp: 2018-02-12T15:48:44Z
  labels:
    addonmanager.kubernetes.io/mode: Reconcile
    k8s-app: kube-dns
    kubernetes.io/cluster-service: "true"
    kubernetes.io/name: KubeDNS
  name: kube-dns
  namespace: kube-system
  resourceVersion: "50573380"
  selfLink: /api/v1/namespaces/kube-system/endpoints/kube-dns
  uid: 34991433-100c-11e8-a600-42010a8002c3
subsets:
- addresses:
  - ip: 10.40.0.5
    nodeName: gke-istio-test-default-pool-866a0405-420r
    targetRef:
      kind: Pod
      name: kube-dns-548976df6c-kxnhb
      namespace: kube-system
      resourceVersion: "50573379"
      uid: 66b0ca7d-f71d-11e8-af4f-42010a800072
  - ip: 10.40.1.4
    nodeName: gke-istio-test-default-pool-866a0405-ftch
    targetRef:
      kind: Pod
      name: kube-dns-548976df6c-d9kkv
      namespace: kube-system
      resourceVersion: "50572715"
      uid: dd4bbbd4-f71c-11e8-af4f-42010a800072
  ports:
  - name: dns
    port: 53
    protocol: UDP
  - name: dns-tcp
    port: 53
    protocol: TCP
`)

func builtinEndpointsYamlBytes() ([]byte, error) {
	return _builtinEndpointsYaml, nil
}

func builtinEndpointsYaml() (*asset, error) {
	bytes, err := builtinEndpointsYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "builtin/endpoints.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _builtinIngressYaml = []byte(`apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: secured-ingress
  annotations:
    kubernetes.io/ingress.class: "istio"
spec:
  tls:
    - secretName: istio-ingress-certs
  rules:
    - http:
        paths:
          - path: /http
            backend:
              serviceName: a
              servicePort: http
          - path: /pasta
            backend:
              serviceName: b
              servicePort: 8080
          - path: /.*
            backend:
              serviceName: a
              servicePort: grpc
`)

func builtinIngressYamlBytes() ([]byte, error) {
	return _builtinIngressYaml, nil
}

func builtinIngressYaml() (*asset, error) {
	bytes, err := builtinIngressYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "builtin/ingress.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _builtinNamespaceYaml = []byte(`apiVersion: v1
kind: Namespace
metadata:
  labels:
    istio-injection: disabled
  name: somens
spec:
  finalizers:
    - kubernetes
status:
  phase: Active
`)

func builtinNamespaceYamlBytes() ([]byte, error) {
	return _builtinNamespaceYaml, nil
}

func builtinNamespaceYaml() (*asset, error) {
	bytes, err := builtinNamespaceYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "builtin/namespace.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _builtinNodeYaml = []byte(`apiVersion: v1
kind: Node
metadata:
  annotations:
    container.googleapis.com/instance_id: "2787417306096525587"
    node.alpha.kubernetes.io/ttl: "0"
    volumes.kubernetes.io/controller-managed-attach-detach: "true"
  creationTimestamp: 2018-10-05T19:40:48Z
  labels:
    kubernetes.io/arch: amd64
    beta.kubernetes.io/fluentd-ds-ready: "true"
    beta.kubernetes.io/instance-type: n1-standard-4
    kubernetes.io/os: linux
    cloud.google.com/gke-nodepool: default-pool
    cloud.google.com/gke-os-distribution: cos
    failure-domain.beta.kubernetes.io/region: us-central1
    failure-domain.beta.kubernetes.io/zone: us-central1-a
    kubernetes.io/hostname: gke-istio-test-default-pool-866a0405-420r
  name: gke-istio-test-default-pool-866a0405-420r
  resourceVersion: "60695251"
  selfLink: /api/v1/nodes/gke-istio-test-default-pool-866a0405-420r
  uid: 8f63dfef-c8d6-11e8-8901-42010a800278
spec:
  externalID: "1929748586650271976"
  podCIDR: 10.40.0.0/24
  providerID: gce://nathanmittler-istio-test/us-central1-a/gke-istio-test-default-pool-866a0405-420r
status:
  addresses:
  - address: 10.128.0.4
    type: InternalIP
  - address: 35.238.214.129
    type: ExternalIP
  - address: gke-istio-test-default-pool-866a0405-420r
    type: Hostname
  allocatable:
    cpu: 3920m
    ephemeral-storage: "47093746742"
    hugepages-2Mi: "0"
    memory: 12699980Ki
    pods: "110"
  capacity:
    cpu: "4"
    ephemeral-storage: 98868448Ki
    hugepages-2Mi: "0"
    memory: 15399244Ki
    pods: "110"
  conditions:
  - lastHeartbeatTime: 2019-01-15T17:36:51Z
    lastTransitionTime: 2018-12-03T17:00:58Z
    message: node is functioning properly
    reason: UnregisterNetDevice
    status: "False"
    type: FrequentUnregisterNetDevice
  - lastHeartbeatTime: 2019-01-15T17:36:51Z
    lastTransitionTime: 2018-12-03T16:55:56Z
    message: kernel has no deadlock
    reason: KernelHasNoDeadlock
    status: "False"
    type: KernelDeadlock
  - lastHeartbeatTime: 2018-10-05T19:40:58Z
    lastTransitionTime: 2018-10-05T19:40:58Z
    message: RouteController created a route
    reason: RouteCreated
    status: "False"
    type: NetworkUnavailable
  - lastHeartbeatTime: 2019-01-15T17:37:32Z
    lastTransitionTime: 2018-12-03T16:55:57Z
    message: kubelet has sufficient disk space available
    reason: KubeletHasSufficientDisk
    status: "False"
    type: OutOfDisk
  - lastHeartbeatTime: 2019-01-15T17:37:32Z
    lastTransitionTime: 2018-12-03T16:55:57Z
    message: kubelet has sufficient memory available
    reason: KubeletHasSufficientMemory
    status: "False"
    type: MemoryPressure
  - lastHeartbeatTime: 2019-01-15T17:37:32Z
    lastTransitionTime: 2018-12-03T16:55:57Z
    message: kubelet has no disk pressure
    reason: KubeletHasNoDiskPressure
    status: "False"
    type: DiskPressure
  - lastHeartbeatTime: 2019-01-15T17:37:32Z
    lastTransitionTime: 2018-10-05T19:40:48Z
    message: kubelet has sufficient PID available
    reason: KubeletHasSufficientPID
    status: "False"
    type: PIDPressure
  - lastHeartbeatTime: 2019-01-15T17:37:32Z
    lastTransitionTime: 2018-12-03T16:56:07Z
    message: kubelet is posting ready status. AppArmor enabled
    reason: KubeletReady
    status: "True"
    type: Ready
  daemonEndpoints:
    kubeletEndpoint:
      Port: 10250
  images:
  - names:
    - gcr.io/stackdriver-agents/stackdriver-logging-agent@sha256:a33f69d0034fdce835a1eb7df8a051ea74323f3fc30d911bbd2e3f2aef09fc93
    - gcr.io/stackdriver-agents/stackdriver-logging-agent:0.3-1.5.34-1-k8s-1
    sizeBytes: 554981103
  - names:
    - istio/examples-bookinfo-reviews-v3@sha256:8c0385f0ca799e655d8770b52cb4618ba54e8966a0734ab1aeb6e8b14e171a3b
    - istio/examples-bookinfo-reviews-v3:1.9.0
    sizeBytes: 525074812
  - names:
    - istio/examples-bookinfo-reviews-v2@sha256:d2483dcb235b27309680177726e4e86905d66e47facaf1d57ed590b2bf95c8ad
    - istio/examples-bookinfo-reviews-v2:1.9.0
    sizeBytes: 525074812
  - names:
    - istio/examples-bookinfo-reviews-v1@sha256:920d46b3c526376b28b90d0e895ca7682d36132e6338301fcbcd567ef81bde05
    - istio/examples-bookinfo-reviews-v1:1.9.0
    sizeBytes: 525074812
  - names:
    - gcr.io/nathanmittler-istio-test/proxyv2@sha256:8cea2c055dd3d3ab78f99584256efcc1cff7d8ddbed11cded404e9d164235502
    sizeBytes: 448337138
  - names:
    - gcr.io/nathanmittler-istio-test/proxyv2@sha256:9949bc22667ef88e54ae91700a64bf1459e8c14ed92b870b7ec2f630e14cf3c1
    sizeBytes: 446407220
  - names:
    - gcr.io/nathanmittler-istio-test/proxyv2@sha256:fc1f957cfa26673768be8fa865066f730f22fde98a6e80654d00f755a643b507
    sizeBytes: 446407220
  - names:
    - gcr.io/nathanmittler-istio-test/proxyv2@sha256:23a52850819d5196d66e8e20f4f63f314f779716f830e1d109ad0e24b1f0df43
    sizeBytes: 446407220
  - names:
    - gcr.io/nathanmittler-istio-test/proxyv2@sha256:e338c2c5cbc379db24c5b2d67a4acc9cca9a069c2927217fca0ce7cbc582d312
    - gcr.io/nathanmittler-istio-test/proxyv2:latest
    sizeBytes: 446398900
  - names:
    - gcr.io/istio-release/proxyv2@sha256:dec972eab4f46c974feec1563ea484ad4995edf55ea91d42e148c5db04b3d4d2
    - gcr.io/istio-release/proxyv2:master-latest-daily
    sizeBytes: 353271308
  - names:
    - gcr.io/nathanmittler-istio-test/proxyv2@sha256:cb4a29362ff9014bf1d96e0ce2bb6337bf034908bb4a8d48af0628a4d8d64413
    sizeBytes: 344543156
  - names:
    - gcr.io/nathanmittler-istio-test/proxyv2@sha256:9d502fd29961bc3464f7906ac0e86b07edf01cf4892352ef780e55b3525fb0b8
    sizeBytes: 344257154
  - names:
    - gcr.io/nathanmittler-istio-test/proxyv2@sha256:cdd2f527b4bd392b533d2d0e62c257c19d5a35a6b5fc3512aa327c560866aec1
    sizeBytes: 344257154
  - names:
    - gcr.io/nathanmittler-istio-test/proxyv2@sha256:6ec1dced4cee8569c77817927938fa4341f939e0dddab511bc3ee8724d652ae2
    sizeBytes: 344257154
  - names:
    - gcr.io/nathanmittler-istio-test/proxyv2@sha256:3f4115cd8c26a17f6bf8ea49f1ff5b875382bda5a6d46281c70c886e802666b0
    sizeBytes: 344257154
  - names:
    - gcr.io/nathanmittler-istio-test/proxyv2@sha256:4e75c42518bb46376cfe0b4fbaa3da1d8f1cea99f706736f1b0b04a3ac554db2
    sizeBytes: 344201616
  - names:
    - gcr.io/nathanmittler-istio-test/proxyv2@sha256:58a7511f549448f6f86280559069bc57f5c754877ebec69da5bbc7ad55e42162
    sizeBytes: 344201616
  - names:
    - gcr.io/nathanmittler-istio-test/proxyv2@sha256:7f60a750d15cda9918e9172e529270ce78c670751d4027f6adc6bdc84ec2d884
    sizeBytes: 344201436
  - names:
    - gcr.io/nathanmittler-istio-test/proxyv2@sha256:6fc25c08212652c7539caaf0f6d913d929f84c54767f20066657ce0f4e6a51e0
    sizeBytes: 344193424
  - names:
    - gcr.io/nathanmittler-istio-test/proxyv2@sha256:4e93825950c831ce6d2b65c9a80921c8860035e39a4b384d38d40f7d2cb2a4ee
    sizeBytes: 344185232
  - names:
    - gcr.io/nathanmittler-istio-test/proxyv2@sha256:842216399613774640a4605202d446cf61bd48ff20e12391a0239cbc6a8f2c77
    sizeBytes: 344185052
  - names:
    - gcr.io/nathanmittler-istio-test/proxyv2@sha256:8ee2bb6fc5484373227b17e377fc226d8d19be11d38d6dbc304970bd46bc929b
    sizeBytes: 344159662
  - names:
    - gcr.io/nathanmittler-istio-test/pilot@sha256:b62e9f12609b89892bb38c858936f76d81aa3ccdc91a3961309f900c1c4f574b
    sizeBytes: 307722351
  - names:
    - gcr.io/nathanmittler-istio-test/pilot@sha256:2445d3c2839825be2decbafcd3f2668bdf148ba9acbbb855810006a58899f320
    sizeBytes: 307722351
  - names:
    - gcr.io/nathanmittler-istio-test/pilot@sha256:ea8e501811c06674bb4b4622862e2d12e700f5edadc01a050030a0b33a6435a6
    sizeBytes: 307722351
  nodeInfo:
    architecture: amd64
    bootID: 8f772c7c-09eb-41eb-8bb5-76ef214eaaa1
    containerRuntimeVersion: docker://17.3.2
    kernelVersion: 4.14.65+
    kubeProxyVersion: v1.11.3-gke.18
    kubeletVersion: v1.11.3-gke.18
    machineID: f325f89cd295bdcda652fd40f0049e32
    operatingSystem: linux
    osImage: Container-Optimized OS from Google
    systemUUID: F325F89C-D295-BDCD-A652-FD40F0049E32
`)

func builtinNodeYamlBytes() ([]byte, error) {
	return _builtinNodeYaml, nil
}

func builtinNodeYaml() (*asset, error) {
	bytes, err := builtinNodeYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "builtin/node.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _builtinPodYaml = []byte(`apiVersion: v1
kind: Pod
metadata:
  annotations:
    scheduler.alpha.kubernetes.io/critical-pod: ""
    seccomp.security.alpha.kubernetes.io/pod: docker/default
  creationTimestamp: 2018-12-03T16:59:57Z
  generateName: kube-dns-548976df6c-
  labels:
    k8s-app: kube-dns
    pod-template-hash: "1045328927"
  name: kube-dns-548976df6c-d9kkv
  namespace: kube-system
  ownerReferences:
  - apiVersion: apps/v1
    blockOwnerDeletion: true
    controller: true
    kind: ReplicaSet
    name: kube-dns-548976df6c
    uid: b589a851-f71b-11e8-af4f-42010a800072
  resourceVersion: "50572715"
  selfLink: /api/v1/namespaces/kube-system/pods/kube-dns-548976df6c-d9kkv
  uid: dd4bbbd4-f71c-11e8-af4f-42010a800072
spec:
  containers:
  - args:
    - --domain=cluster.local.
    - --dns-port=10053
    - --config-dir=/kube-dns-config
    - --v=2
    env:
    - name: PROMETHEUS_PORT
      value: "10055"
    image: k8s.gcr.io/k8s-dns-kube-dns-amd64:1.14.13
    imagePullPolicy: IfNotPresent
    livenessProbe:
      failureThreshold: 5
      httpGet:
        path: /healthcheck/kubedns
        port: 10054
        scheme: HTTP
      initialDelaySeconds: 60
      periodSeconds: 10
      successThreshold: 1
      timeoutSeconds: 5
    name: kubedns
    ports:
    - containerPort: 10053
      name: dns-local
      protocol: UDP
    - containerPort: 10053
      name: dns-tcp-local
      protocol: TCP
    - containerPort: 10055
      name: metrics
      protocol: TCP
    readinessProbe:
      failureThreshold: 3
      httpGet:
        path: /readiness
        port: 8081
        scheme: HTTP
      initialDelaySeconds: 3
      periodSeconds: 10
      successThreshold: 1
      timeoutSeconds: 5
    resources:
      limits:
        memory: 170Mi
      requests:
        cpu: 100m
        memory: 70Mi
    terminationMessagePath: /dev/termination-log
    terminationMessagePolicy: File
    volumeMounts:
    - mountPath: /kube-dns-config
      name: kube-dns-config
    - mountPath: /var/run/secrets/kubernetes.io/serviceaccount
      name: kube-dns-token-lwn8l
      readOnly: true
  - args:
    - -v=2
    - -logtostderr
    - -configDir=/etc/k8s/dns/dnsmasq-nanny
    - -restartDnsmasq=true
    - --
    - -k
    - --cache-size=1000
    - --no-negcache
    - --log-facility=-
    - --server=/cluster.local/127.0.0.1#10053
    - --server=/in-addr.arpa/127.0.0.1#10053
    - --server=/ip6.arpa/127.0.0.1#10053
    image: k8s.gcr.io/k8s-dns-dnsmasq-nanny-amd64:1.14.13
    imagePullPolicy: IfNotPresent
    livenessProbe:
      failureThreshold: 5
      httpGet:
        path: /healthcheck/dnsmasq
        port: 10054
        scheme: HTTP
      initialDelaySeconds: 60
      periodSeconds: 10
      successThreshold: 1
      timeoutSeconds: 5
    name: dnsmasq
    ports:
    - containerPort: 53
      name: dns
      protocol: UDP
    - containerPort: 53
      name: dns-tcp
      protocol: TCP
    resources:
      requests:
        cpu: 150m
        memory: 20Mi
    terminationMessagePath: /dev/termination-log
    terminationMessagePolicy: File
    volumeMounts:
    - mountPath: /etc/k8s/dns/dnsmasq-nanny
      name: kube-dns-config
    - mountPath: /var/run/secrets/kubernetes.io/serviceaccount
      name: kube-dns-token-lwn8l
      readOnly: true
  - args:
    - --v=2
    - --logtostderr
    - --probe=kubedns,127.0.0.1:10053,kubernetes.default.svc.cluster.local,5,SRV
    - --probe=dnsmasq,127.0.0.1:53,kubernetes.default.svc.cluster.local,5,SRV
    image: k8s.gcr.io/k8s-dns-sidecar-amd64:1.14.13
    imagePullPolicy: IfNotPresent
    livenessProbe:
      failureThreshold: 5
      httpGet:
        path: /metrics
        port: 10054
        scheme: HTTP
      initialDelaySeconds: 60
      periodSeconds: 10
      successThreshold: 1
      timeoutSeconds: 5
    name: sidecar
    ports:
    - containerPort: 10054
      name: metrics
      protocol: TCP
    resources:
      requests:
        cpu: 10m
        memory: 20Mi
    terminationMessagePath: /dev/termination-log
    terminationMessagePolicy: File
    volumeMounts:
    - mountPath: /var/run/secrets/kubernetes.io/serviceaccount
      name: kube-dns-token-lwn8l
      readOnly: true
  - command:
    - /monitor
    - --component=kubedns
    - --target-port=10054
    - --stackdriver-prefix=container.googleapis.com/internal/addons
    - --api-override=https://monitoring.googleapis.com/
    - --whitelisted-metrics=probe_kubedns_latency_ms,probe_kubedns_errors,dnsmasq_misses,dnsmasq_hits
    - --pod-id=$(POD_NAME)
    - --namespace-id=$(POD_NAMESPACE)
    - --v=2
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
    image: gcr.io/google-containers/prometheus-to-sd:v0.2.3
    imagePullPolicy: IfNotPresent
    name: prometheus-to-sd
    resources: {}
    terminationMessagePath: /dev/termination-log
    terminationMessagePolicy: File
    volumeMounts:
    - mountPath: /var/run/secrets/kubernetes.io/serviceaccount
      name: kube-dns-token-lwn8l
      readOnly: true
  dnsPolicy: Default
  nodeName: gke-istio-test-default-pool-866a0405-ftch
  priority: 2000000000
  priorityClassName: system-cluster-critical
  restartPolicy: Always
  schedulerName: default-scheduler
  securityContext: {}
  serviceAccount: kube-dns
  serviceAccountName: kube-dns
  terminationGracePeriodSeconds: 30
  tolerations:
  - key: CriticalAddonsOnly
    operator: Exists
  - effect: NoExecute
    key: node.kubernetes.io/not-ready
    operator: Exists
    tolerationSeconds: 300
  - effect: NoExecute
    key: node.kubernetes.io/unreachable
    operator: Exists
    tolerationSeconds: 300
  volumes:
  - configMap:
      defaultMode: 420
      name: kube-dns
      optional: true
    name: kube-dns-config
  - name: kube-dns-token-lwn8l
    secret:
      defaultMode: 420
      secretName: kube-dns-token-lwn8l
status:
  conditions:
  - lastProbeTime: null
    lastTransitionTime: 2018-12-03T17:00:00Z
    status: "True"
    type: Initialized
  - lastProbeTime: null
    lastTransitionTime: 2018-12-03T17:00:20Z
    status: "True"
    type: Ready
  - lastProbeTime: null
    lastTransitionTime: null
    status: "True"
    type: ContainersReady
  - lastProbeTime: null
    lastTransitionTime: 2018-12-03T16:59:57Z
    status: "True"
    type: PodScheduled
  containerStatuses:
  - containerID: docker://676f6c98bfa136315c4ccf0fe40e7a56cbf9ac85128e94310eae82f191246b3e
    image: k8s.gcr.io/k8s-dns-dnsmasq-nanny-amd64:1.14.13
    imageID: docker-pullable://k8s.gcr.io/k8s-dns-dnsmasq-nanny-amd64@sha256:45df3e8e0c551bd0c79cdba48ae6677f817971dcbd1eeed7fd1f9a35118410e4
    lastState: {}
    name: dnsmasq
    ready: true
    restartCount: 0
    state:
      running:
        startedAt: 2018-12-03T17:00:14Z
  - containerID: docker://93fd0664e150982dad0481c5260183308a7035a2f938ec50509d586ed586a107
    image: k8s.gcr.io/k8s-dns-kube-dns-amd64:1.14.13
    imageID: docker-pullable://k8s.gcr.io/k8s-dns-kube-dns-amd64@sha256:618a82fa66cf0c75e4753369a6999032372be7308866fc9afb381789b1e5ad52
    lastState: {}
    name: kubedns
    ready: true
    restartCount: 0
    state:
      running:
        startedAt: 2018-12-03T17:00:10Z
  - containerID: docker://e823b79a0a48af75f2eebb1c89ba4c31e8c1ee67ee0d917ac7b4891b67d2cd0f
    image: gcr.io/google-containers/prometheus-to-sd:v0.2.3
    imageID: docker-pullable://gcr.io/google-containers/prometheus-to-sd@sha256:be220ec4a66275442f11d420033c106bb3502a3217a99c806eef3cf9858788a2
    lastState: {}
    name: prometheus-to-sd
    ready: true
    restartCount: 0
    state:
      running:
        startedAt: 2018-12-03T17:00:18Z
  - containerID: docker://74223c401a8dac04b8bd29cdfedcb216881791b4e84bb80a15714991dd18735e
    image: k8s.gcr.io/k8s-dns-sidecar-amd64:1.14.13
    imageID: docker-pullable://k8s.gcr.io/k8s-dns-sidecar-amd64@sha256:cedc8fe2098dffc26d17f64061296b7aa54258a31513b6c52df271a98bb522b3
    lastState: {}
    name: sidecar
    ready: true
    restartCount: 0
    state:
      running:
        startedAt: 2018-12-03T17:00:16Z
  hostIP: 10.128.0.5
  phase: Running
  podIP: 10.40.1.4
  qosClass: Burstable
  startTime: 2018-12-03T17:00:00Z
`)

func builtinPodYamlBytes() ([]byte, error) {
	return _builtinPodYaml, nil
}

func builtinPodYaml() (*asset, error) {
	bytes, err := builtinPodYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "builtin/pod.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _builtinServiceYaml = []byte(`apiVersion: v1
kind: Service
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"v1","kind":"Service","metadata":{"annotations":{},"labels":{"addonmanager.kubernetes.io/mode":"Reconcile","k8s-app":"kube-dns","kubernetes.io/cluster-service":"true","kubernetes.io/name":"KubeDNS"},"name":"kube-dns","namespace":"kube-system"},"spec":{"clusterIP":"10.43.240.10","ports":[{"name":"dns","port":53,"protocol":"UDP"},{"name":"dns-tcp","port":53,"protocol":"TCP"}],"selector":{"k8s-app":"kube-dns"}}}
  creationTimestamp: 2018-02-12T15:48:44Z
  labels:
    addonmanager.kubernetes.io/mode: Reconcile
    k8s-app: kube-dns
    kubernetes.io/cluster-service: "true"
    kubernetes.io/name: KubeDNS
  name: kube-dns
  namespace: kube-system
  resourceVersion: "274"
  selfLink: /api/v1/namespaces/kube-system/services/kube-dns
  uid: 3497d702-100c-11e8-a600-42010a8002c3
spec:
  clusterIP: 10.43.240.10
  ports:
  - name: dns
    port: 53
    protocol: UDP
    targetPort: 53
  - name: dns-tcp
    port: 53
    protocol: TCP
    targetPort: 53
  selector:
    k8s-app: kube-dns
  sessionAffinity: None
  type: ClusterIP
status:
  loadBalancer: {}
`)

func builtinServiceYamlBytes() ([]byte, error) {
	return _builtinServiceYaml, nil
}

func builtinServiceYaml() (*asset, error) {
	bytes, err := builtinServiceYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "builtin/service.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
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
	"builtin/deployment.yaml": builtinDeploymentYaml,
	"builtin/endpoints.yaml":  builtinEndpointsYaml,
	"builtin/ingress.yaml":    builtinIngressYaml,
	"builtin/namespace.yaml":  builtinNamespaceYaml,
	"builtin/node.yaml":       builtinNodeYaml,
	"builtin/pod.yaml":        builtinPodYaml,
	"builtin/service.yaml":    builtinServiceYaml,
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
	"builtin": &bintree{nil, map[string]*bintree{
		"deployment.yaml": &bintree{builtinDeploymentYaml, map[string]*bintree{}},
		"endpoints.yaml":  &bintree{builtinEndpointsYaml, map[string]*bintree{}},
		"ingress.yaml":    &bintree{builtinIngressYaml, map[string]*bintree{}},
		"namespace.yaml":  &bintree{builtinNamespaceYaml, map[string]*bintree{}},
		"node.yaml":       &bintree{builtinNodeYaml, map[string]*bintree{}},
		"pod.yaml":        &bintree{builtinPodYaml, map[string]*bintree{}},
		"service.yaml":    &bintree{builtinServiceYaml, map[string]*bintree{}},
	}},
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

// Code generated for package validation by go-bindata DO NOT EDIT. (@generated)
// sources:
// dataset/networking-v1alpha3-DestinationRule-invalid.yaml
// dataset/networking-v1alpha3-DestinationRule-valid.yaml
// dataset/networking-v1alpha3-EnvoyFilter-invalid.yaml
// dataset/networking-v1alpha3-EnvoyFilter-valid.yaml
// dataset/networking-v1alpha3-Gateway-invalid.yaml
// dataset/networking-v1alpha3-Gateway-valid.yaml
// dataset/networking-v1alpha3-ServiceEntry-invalid.yaml
// dataset/networking-v1alpha3-ServiceEntry-valid.yaml
// dataset/networking-v1alpha3-Sidecar-invalid.yaml
// dataset/networking-v1alpha3-Sidecar-valid.yaml
// dataset/networking-v1alpha3-VirtualService-invalid.yaml
// dataset/networking-v1alpha3-VirtualService-regexp-invalid.yaml
// dataset/networking-v1alpha3-VirtualService-valid.yaml
// dataset/networking-v1alpha3-WorkloadEntry-invalid.yaml
// dataset/networking-v1alpha3-WorkloadEntry-valid.yaml
// dataset/networking-v1alpha3-WorkloadGroup-invalid.yaml
// dataset/networking-v1alpha3-WorkloadGroup-valid.yaml
// dataset/networking-v1beta-DestinationRule-invalid.yaml
// dataset/networking-v1beta-DestinationRule-valid.yaml
// dataset/networking-v1beta-Gateway-invalid.yaml
// dataset/networking-v1beta-Gateway-valid.yaml
// dataset/networking-v1beta-Sidecar-invalid.yaml
// dataset/networking-v1beta-Sidecar-valid.yaml
// dataset/networking-v1beta-VirtualService-invalid.yaml
// dataset/networking-v1beta-VirtualService-valid.yaml
// dataset/networking-v1beta-WorkloadEntry-invalid.yaml
// dataset/networking-v1beta-WorkloadEntry-valid.yaml
// dataset/security-v1beta1-AuthorizationPolicy-invalid.yaml
// dataset/security-v1beta1-AuthorizationPolicy-valid.yaml
// dataset/security-v1beta1-PeerAuthentication-invalid.yaml
// dataset/security-v1beta1-PeerAuthentication-valid.yaml
// dataset/security-v1beta1-RequestAuthentication-invalid.yaml
// dataset/security-v1beta1-RequestAuthentication-valid.yaml
package validation

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

var _datasetNetworkingV1alpha3DestinationruleInvalidYaml = []byte(`apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: invalid-destination-rule
spec:
  subsets:
    - name: v1
      labels:
        version: v1
    - name: v2
      labels:
        version: v2
`)

func datasetNetworkingV1alpha3DestinationruleInvalidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1alpha3DestinationruleInvalidYaml, nil
}

func datasetNetworkingV1alpha3DestinationruleInvalidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1alpha3DestinationruleInvalidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1alpha3-DestinationRule-invalid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1alpha3DestinationruleValidYaml = []byte(`apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: valid-destination-rule
spec:
  host: c
  subsets:
    - name: v1
      labels:
        version: v1
    - name: v2
      labels:
        version: v2
`)

func datasetNetworkingV1alpha3DestinationruleValidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1alpha3DestinationruleValidYaml, nil
}

func datasetNetworkingV1alpha3DestinationruleValidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1alpha3DestinationruleValidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1alpha3-DestinationRule-valid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1alpha3EnvoyfilterInvalidYaml = []byte(`apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: simple-envoy-filter
spec:
  configPatches:
  - applyTo: LISTENER
    patch: {}
`)

func datasetNetworkingV1alpha3EnvoyfilterInvalidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1alpha3EnvoyfilterInvalidYaml, nil
}

func datasetNetworkingV1alpha3EnvoyfilterInvalidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1alpha3EnvoyfilterInvalidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1alpha3-EnvoyFilter-invalid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1alpha3EnvoyfilterValidYaml = []byte(`apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: simple-envoy-filter
spec:
  workloadSelector:
    labels:
      app: c

`)

func datasetNetworkingV1alpha3EnvoyfilterValidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1alpha3EnvoyfilterValidYaml, nil
}

func datasetNetworkingV1alpha3EnvoyfilterValidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1alpha3EnvoyfilterValidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1alpha3-EnvoyFilter-valid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1alpha3GatewayInvalidYaml = []byte(`# Routes TCP traffic through the ingressgateway Gateway to service A.
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: invalid-gateway
spec:
  selector:
    # DO NOT CHANGE THESE LABELS
    # The ingressgateway is defined in install/kubernetes/helm/istio/values.yaml
    # with these labels
    istio: ingressgateway
`)

func datasetNetworkingV1alpha3GatewayInvalidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1alpha3GatewayInvalidYaml, nil
}

func datasetNetworkingV1alpha3GatewayInvalidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1alpha3GatewayInvalidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1alpha3-Gateway-invalid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1alpha3GatewayValidYaml = []byte(`# Routes TCP traffic through the ingressgateway Gateway to service A.
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: valid-gateway
spec:
  selector:
    # DO NOT CHANGE THESE LABELS
    # The ingressgateway is defined in install/kubernetes/helm/istio/values.yaml
    # with these labels
    istio: ingressgateway
  servers:
  - port:
      number: 31400
      protocol: TCP
      name: tcp
    hosts:
    - a.istio-system.svc.cluster.local
`)

func datasetNetworkingV1alpha3GatewayValidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1alpha3GatewayValidYaml, nil
}

func datasetNetworkingV1alpha3GatewayValidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1alpha3GatewayValidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1alpha3-Gateway-valid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1alpha3ServiceentryInvalidYaml = []byte(`apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: invalid-service-entry
spec:
  ports:
  - number: 80
    name: http
    protocol: HTTP
  discovery: DNS
  endpoints:
  # Rather than relying on an external host that might become unreachable (causing test failures)
  # we can mock the external endpoint using service t which has no sidecar.
  - address: t.istio-system.svc.cluster.local # TODO: this is brittle
    ports:
      http: 8080 # TODO test https
`)

func datasetNetworkingV1alpha3ServiceentryInvalidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1alpha3ServiceentryInvalidYaml, nil
}

func datasetNetworkingV1alpha3ServiceentryInvalidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1alpha3ServiceentryInvalidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1alpha3-ServiceEntry-invalid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1alpha3ServiceentryValidYaml = []byte(`apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: valid-service-entry
spec:
  hosts:
  - eu.bookinfo.com
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: DNS
  endpoints:
  # Rather than relying on an external host that might become unreachable (causing test failures)
  # we can mock the external endpoint using service t which has no sidecar.
  - address: t.istio-system.svc.cluster.local # TODO: this is brittle
    ports:
      http: 8080 # TODO test https
`)

func datasetNetworkingV1alpha3ServiceentryValidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1alpha3ServiceentryValidYaml, nil
}

func datasetNetworkingV1alpha3ServiceentryValidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1alpha3ServiceentryValidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1alpha3-ServiceEntry-valid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1alpha3SidecarInvalidYaml = []byte(`apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: invalid-sidecar-config
spec:
  egress:
`)

func datasetNetworkingV1alpha3SidecarInvalidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1alpha3SidecarInvalidYaml, nil
}

func datasetNetworkingV1alpha3SidecarInvalidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1alpha3SidecarInvalidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1alpha3-Sidecar-invalid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1alpha3SidecarValidYaml = []byte(`apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: valid-sidecar-config
spec:
  egress:
  - hosts:
    - "abc/*"
`)

func datasetNetworkingV1alpha3SidecarValidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1alpha3SidecarValidYaml, nil
}

func datasetNetworkingV1alpha3SidecarValidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1alpha3SidecarValidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1alpha3-Sidecar-valid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1alpha3VirtualserviceInvalidYaml = []byte(`apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: invalid-virtual-service
spec:
  http:
    - route:
      - destination:
          host: c
          subset: v1
        weight: 75
      - destination:
          host: c
          subset: v2
        weight: 15
`)

func datasetNetworkingV1alpha3VirtualserviceInvalidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1alpha3VirtualserviceInvalidYaml, nil
}

func datasetNetworkingV1alpha3VirtualserviceInvalidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1alpha3VirtualserviceInvalidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1alpha3-VirtualService-invalid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1alpha3VirtualserviceRegexpInvalidYaml = []byte(`apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: ecma-not-v2
spec:
  hosts:
  - "*"
  gateways:
  - bookinfo-gateway
  http:
  - match:
    - uri:
        regex: "^(?!.<path to match here>.).*"
    route:
    - destination:
        host: productpage
`)

func datasetNetworkingV1alpha3VirtualserviceRegexpInvalidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1alpha3VirtualserviceRegexpInvalidYaml, nil
}

func datasetNetworkingV1alpha3VirtualserviceRegexpInvalidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1alpha3VirtualserviceRegexpInvalidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1alpha3-VirtualService-regexp-invalid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1alpha3VirtualserviceValidYaml = []byte(`apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: valid-virtual-service
spec:
  hosts:
    - c
  http:
    - route:
      - destination:
          host: c
          subset: v1
        weight: 75
      - destination:
          host: c
          subset: v2
        weight: 25
`)

func datasetNetworkingV1alpha3VirtualserviceValidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1alpha3VirtualserviceValidYaml, nil
}

func datasetNetworkingV1alpha3VirtualserviceValidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1alpha3VirtualserviceValidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1alpha3-VirtualService-valid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1alpha3WorkloadentryInvalidYaml = []byte(`apiVersion: networking.istio.io/v1alpha3
kind: WorkloadEntry
metadata:
  name: invalid-workload-entry
spec:
  address: ""
`)

func datasetNetworkingV1alpha3WorkloadentryInvalidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1alpha3WorkloadentryInvalidYaml, nil
}

func datasetNetworkingV1alpha3WorkloadentryInvalidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1alpha3WorkloadentryInvalidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1alpha3-WorkloadEntry-invalid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1alpha3WorkloadentryValidYaml = []byte(`apiVersion: networking.istio.io/v1alpha3
kind: WorkloadEntry
metadata:
  name: valid-workload-entry
spec:
  address: "1.2.3.4"
`)

func datasetNetworkingV1alpha3WorkloadentryValidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1alpha3WorkloadentryValidYaml, nil
}

func datasetNetworkingV1alpha3WorkloadentryValidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1alpha3WorkloadentryValidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1alpha3-WorkloadEntry-valid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1alpha3WorkloadgroupInvalidYaml = []byte(`apiVersion: networking.istio.io/v1alpha3
kind: WorkloadGroup
metadata:
  name: reviews
spec:
  metadata:
    labels:
      app.kubernetes.io/name: reviews
      app.kubernetes.io/version: "1.3.4"
      ".": "~"
`)

func datasetNetworkingV1alpha3WorkloadgroupInvalidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1alpha3WorkloadgroupInvalidYaml, nil
}

func datasetNetworkingV1alpha3WorkloadgroupInvalidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1alpha3WorkloadgroupInvalidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1alpha3-WorkloadGroup-invalid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1alpha3WorkloadgroupValidYaml = []byte(`apiVersion: networking.istio.io/v1alpha3
kind: WorkloadGroup
metadata:
  name: reviews
spec:
  metadata:
    labels:
      app.kubernetes.io/name: reviews
      app.kubernetes.io/version: "1.3.4"
  template:
    ports:
      grpc: 3550
      http: 8080
    serviceAccount: default
`)

func datasetNetworkingV1alpha3WorkloadgroupValidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1alpha3WorkloadgroupValidYaml, nil
}

func datasetNetworkingV1alpha3WorkloadgroupValidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1alpha3WorkloadgroupValidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1alpha3-WorkloadGroup-valid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1betaDestinationruleInvalidYaml = []byte(`apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: invalid-destination-rule
spec:
  subsets:
    - name: v1
      labels:
        version: v1
    - name: v2
      labels:
        version: v2
`)

func datasetNetworkingV1betaDestinationruleInvalidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1betaDestinationruleInvalidYaml, nil
}

func datasetNetworkingV1betaDestinationruleInvalidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1betaDestinationruleInvalidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1beta-DestinationRule-invalid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1betaDestinationruleValidYaml = []byte(`apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: valid-destination-rule
spec:
  host: c
  subsets:
    - name: v1
      labels:
        version: v1
    - name: v2
      labels:
        version: v2
`)

func datasetNetworkingV1betaDestinationruleValidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1betaDestinationruleValidYaml, nil
}

func datasetNetworkingV1betaDestinationruleValidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1betaDestinationruleValidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1beta-DestinationRule-valid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1betaGatewayInvalidYaml = []byte(`# Routes TCP traffic through the ingressgateway Gateway to service A.
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: invalid-gateway
spec:
  selector:
    # DO NOT CHANGE THESE LABELS
    # The ingressgateway is defined in install/kubernetes/helm/istio/values.yaml
    # with these labels
    istio: ingressgateway
`)

func datasetNetworkingV1betaGatewayInvalidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1betaGatewayInvalidYaml, nil
}

func datasetNetworkingV1betaGatewayInvalidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1betaGatewayInvalidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1beta-Gateway-invalid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1betaGatewayValidYaml = []byte(`# Routes TCP traffic through the ingressgateway Gateway to service A.
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: valid-gateway
spec:
  selector:
    # DO NOT CHANGE THESE LABELS
    # The ingressgateway is defined in install/kubernetes/helm/istio/values.yaml
    # with these labels
    istio: ingressgateway
  servers:
  - port:
      number: 31400
      protocol: TCP
      name: tcp
    hosts:
    - a.istio-system.svc.cluster.local
`)

func datasetNetworkingV1betaGatewayValidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1betaGatewayValidYaml, nil
}

func datasetNetworkingV1betaGatewayValidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1betaGatewayValidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1beta-Gateway-valid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1betaSidecarInvalidYaml = []byte(`apiVersion: networking.istio.io/v1beta1
kind: Sidecar
metadata:
  name: invalid-sidecar-config
spec:
  egress:
`)

func datasetNetworkingV1betaSidecarInvalidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1betaSidecarInvalidYaml, nil
}

func datasetNetworkingV1betaSidecarInvalidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1betaSidecarInvalidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1beta-Sidecar-invalid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1betaSidecarValidYaml = []byte(`apiVersion: networking.istio.io/v1beta1
kind: Sidecar
metadata:
  name: valid-sidecar-config
spec:
  egress:
  - hosts:
    - "abc/*"
`)

func datasetNetworkingV1betaSidecarValidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1betaSidecarValidYaml, nil
}

func datasetNetworkingV1betaSidecarValidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1betaSidecarValidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1beta-Sidecar-valid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1betaVirtualserviceInvalidYaml = []byte(`apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: invalid-virtual-service
spec:
  http:
    - route:
      - destination:
          host: c
          subset: v1
        weight: 75
      - destination:
          host: c
          subset: v2
        weight: 15
`)

func datasetNetworkingV1betaVirtualserviceInvalidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1betaVirtualserviceInvalidYaml, nil
}

func datasetNetworkingV1betaVirtualserviceInvalidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1betaVirtualserviceInvalidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1beta-VirtualService-invalid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1betaVirtualserviceValidYaml = []byte(`apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: valid-virtual-service
spec:
  hosts:
    - c
  http:
    - route:
      - destination:
          host: c
          subset: v1
        weight: 75
      - destination:
          host: c
          subset: v2
        weight: 25
`)

func datasetNetworkingV1betaVirtualserviceValidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1betaVirtualserviceValidYaml, nil
}

func datasetNetworkingV1betaVirtualserviceValidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1betaVirtualserviceValidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1beta-VirtualService-valid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1betaWorkloadentryInvalidYaml = []byte(`apiVersion: networking.istio.io/v1beta1
kind: WorkloadEntry
metadata:
  name: valid-workload-entry
spec:
  address: ""
`)

func datasetNetworkingV1betaWorkloadentryInvalidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1betaWorkloadentryInvalidYaml, nil
}

func datasetNetworkingV1betaWorkloadentryInvalidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1betaWorkloadentryInvalidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1beta-WorkloadEntry-invalid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingV1betaWorkloadentryValidYaml = []byte(`apiVersion: networking.istio.io/v1beta1
kind: WorkloadEntry
metadata:
  name: valid-workload-entry
spec:
  address: 1.2.3.4
`)

func datasetNetworkingV1betaWorkloadentryValidYamlBytes() ([]byte, error) {
	return _datasetNetworkingV1betaWorkloadentryValidYaml, nil
}

func datasetNetworkingV1betaWorkloadentryValidYaml() (*asset, error) {
	bytes, err := datasetNetworkingV1betaWorkloadentryValidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking-v1beta-WorkloadEntry-valid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetSecurityV1beta1AuthorizationpolicyInvalidYaml = []byte(`apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: invalid-authorization-policy
spec:
  selector:
    matchLabels:
      app: httpbin
      version: v1
  rules:
  - from:
    - source:
        principals: ["cluster.local/ns/default/sa/sleep"]
    to:
    - operation:
        methods: ["GET"]
    when:
    - values: ["key is missing"]
`)

func datasetSecurityV1beta1AuthorizationpolicyInvalidYamlBytes() ([]byte, error) {
	return _datasetSecurityV1beta1AuthorizationpolicyInvalidYaml, nil
}

func datasetSecurityV1beta1AuthorizationpolicyInvalidYaml() (*asset, error) {
	bytes, err := datasetSecurityV1beta1AuthorizationpolicyInvalidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/security-v1beta1-AuthorizationPolicy-invalid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetSecurityV1beta1AuthorizationpolicyValidYaml = []byte(`apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
 name: authorization-policy
spec:
 selector:
   matchLabels:
     app: httpbin
     version: v1
 rules:
 - from:
   - source:
       principals: ["cluster.local/ns/default/sa/sleep"]
   - source:
       namespaces: ["test"]
   to:
   - operation:
       methods: ["GET"]
       paths: ["/info*"]
   - operation:
       methods: ["POST"]
       paths: ["/data"]
   when:
   - key: request.auth.claims[iss]
     values: ["https://accounts.google.com"]
`)

func datasetSecurityV1beta1AuthorizationpolicyValidYamlBytes() ([]byte, error) {
	return _datasetSecurityV1beta1AuthorizationpolicyValidYaml, nil
}

func datasetSecurityV1beta1AuthorizationpolicyValidYaml() (*asset, error) {
	bytes, err := datasetSecurityV1beta1AuthorizationpolicyValidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/security-v1beta1-AuthorizationPolicy-valid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetSecurityV1beta1PeerauthenticationInvalidYaml = []byte(`apiVersion: "security.istio.io/v1beta1"
kind: "PeerAuthentication"
metadata:
  name: invalid-peer-authentication
spec:
  portLevelMtls: {}
`)

func datasetSecurityV1beta1PeerauthenticationInvalidYamlBytes() ([]byte, error) {
	return _datasetSecurityV1beta1PeerauthenticationInvalidYaml, nil
}

func datasetSecurityV1beta1PeerauthenticationInvalidYaml() (*asset, error) {
	bytes, err := datasetSecurityV1beta1PeerauthenticationInvalidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/security-v1beta1-PeerAuthentication-invalid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetSecurityV1beta1PeerauthenticationValidYaml = []byte(`apiVersion: "security.istio.io/v1beta1"
kind: "PeerAuthentication"
metadata:
  name: valid-peer-authentication
spec:
  selector:
    matchLabels:
      app: httpbin
      version: v1
  mtls:
    mode: PERMISSIVE
  portLevelMtls:
    8080:
      mode: STRICT
`)

func datasetSecurityV1beta1PeerauthenticationValidYamlBytes() ([]byte, error) {
	return _datasetSecurityV1beta1PeerauthenticationValidYaml, nil
}

func datasetSecurityV1beta1PeerauthenticationValidYaml() (*asset, error) {
	bytes, err := datasetSecurityV1beta1PeerauthenticationValidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/security-v1beta1-PeerAuthentication-valid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetSecurityV1beta1RequestauthenticationInvalidYaml = []byte(`apiVersion: "security.istio.io/v1beta1"
kind: "RequestAuthentication"
metadata:
  name: invalid-request-authentication
spec:
  selector:
    matchLabels:
      app: httpbin
      version: v1
  jwtRules:
  - issuer: ""
`)

func datasetSecurityV1beta1RequestauthenticationInvalidYamlBytes() ([]byte, error) {
	return _datasetSecurityV1beta1RequestauthenticationInvalidYaml, nil
}

func datasetSecurityV1beta1RequestauthenticationInvalidYaml() (*asset, error) {
	bytes, err := datasetSecurityV1beta1RequestauthenticationInvalidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/security-v1beta1-RequestAuthentication-invalid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetSecurityV1beta1RequestauthenticationValidYaml = []byte(`apiVersion: "security.istio.io/v1beta1"
kind: "RequestAuthentication"
metadata:
  name: valid-request-authentication
spec:
  selector:
    matchLabels:
      app: httpbin
      version: v1
  jwtRules:
  - issuer: example.com 
`)

func datasetSecurityV1beta1RequestauthenticationValidYamlBytes() ([]byte, error) {
	return _datasetSecurityV1beta1RequestauthenticationValidYaml, nil
}

func datasetSecurityV1beta1RequestauthenticationValidYaml() (*asset, error) {
	bytes, err := datasetSecurityV1beta1RequestauthenticationValidYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/security-v1beta1-RequestAuthentication-valid.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
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
	"dataset/networking-v1alpha3-DestinationRule-invalid.yaml":       datasetNetworkingV1alpha3DestinationruleInvalidYaml,
	"dataset/networking-v1alpha3-DestinationRule-valid.yaml":         datasetNetworkingV1alpha3DestinationruleValidYaml,
	"dataset/networking-v1alpha3-EnvoyFilter-invalid.yaml":           datasetNetworkingV1alpha3EnvoyfilterInvalidYaml,
	"dataset/networking-v1alpha3-EnvoyFilter-valid.yaml":             datasetNetworkingV1alpha3EnvoyfilterValidYaml,
	"dataset/networking-v1alpha3-Gateway-invalid.yaml":               datasetNetworkingV1alpha3GatewayInvalidYaml,
	"dataset/networking-v1alpha3-Gateway-valid.yaml":                 datasetNetworkingV1alpha3GatewayValidYaml,
	"dataset/networking-v1alpha3-ServiceEntry-invalid.yaml":          datasetNetworkingV1alpha3ServiceentryInvalidYaml,
	"dataset/networking-v1alpha3-ServiceEntry-valid.yaml":            datasetNetworkingV1alpha3ServiceentryValidYaml,
	"dataset/networking-v1alpha3-Sidecar-invalid.yaml":               datasetNetworkingV1alpha3SidecarInvalidYaml,
	"dataset/networking-v1alpha3-Sidecar-valid.yaml":                 datasetNetworkingV1alpha3SidecarValidYaml,
	"dataset/networking-v1alpha3-VirtualService-invalid.yaml":        datasetNetworkingV1alpha3VirtualserviceInvalidYaml,
	"dataset/networking-v1alpha3-VirtualService-regexp-invalid.yaml": datasetNetworkingV1alpha3VirtualserviceRegexpInvalidYaml,
	"dataset/networking-v1alpha3-VirtualService-valid.yaml":          datasetNetworkingV1alpha3VirtualserviceValidYaml,
	"dataset/networking-v1alpha3-WorkloadEntry-invalid.yaml":         datasetNetworkingV1alpha3WorkloadentryInvalidYaml,
	"dataset/networking-v1alpha3-WorkloadEntry-valid.yaml":           datasetNetworkingV1alpha3WorkloadentryValidYaml,
	"dataset/networking-v1alpha3-WorkloadGroup-invalid.yaml":         datasetNetworkingV1alpha3WorkloadgroupInvalidYaml,
	"dataset/networking-v1alpha3-WorkloadGroup-valid.yaml":           datasetNetworkingV1alpha3WorkloadgroupValidYaml,
	"dataset/networking-v1beta-DestinationRule-invalid.yaml":         datasetNetworkingV1betaDestinationruleInvalidYaml,
	"dataset/networking-v1beta-DestinationRule-valid.yaml":           datasetNetworkingV1betaDestinationruleValidYaml,
	"dataset/networking-v1beta-Gateway-invalid.yaml":                 datasetNetworkingV1betaGatewayInvalidYaml,
	"dataset/networking-v1beta-Gateway-valid.yaml":                   datasetNetworkingV1betaGatewayValidYaml,
	"dataset/networking-v1beta-Sidecar-invalid.yaml":                 datasetNetworkingV1betaSidecarInvalidYaml,
	"dataset/networking-v1beta-Sidecar-valid.yaml":                   datasetNetworkingV1betaSidecarValidYaml,
	"dataset/networking-v1beta-VirtualService-invalid.yaml":          datasetNetworkingV1betaVirtualserviceInvalidYaml,
	"dataset/networking-v1beta-VirtualService-valid.yaml":            datasetNetworkingV1betaVirtualserviceValidYaml,
	"dataset/networking-v1beta-WorkloadEntry-invalid.yaml":           datasetNetworkingV1betaWorkloadentryInvalidYaml,
	"dataset/networking-v1beta-WorkloadEntry-valid.yaml":             datasetNetworkingV1betaWorkloadentryValidYaml,
	"dataset/security-v1beta1-AuthorizationPolicy-invalid.yaml":      datasetSecurityV1beta1AuthorizationpolicyInvalidYaml,
	"dataset/security-v1beta1-AuthorizationPolicy-valid.yaml":        datasetSecurityV1beta1AuthorizationpolicyValidYaml,
	"dataset/security-v1beta1-PeerAuthentication-invalid.yaml":       datasetSecurityV1beta1PeerauthenticationInvalidYaml,
	"dataset/security-v1beta1-PeerAuthentication-valid.yaml":         datasetSecurityV1beta1PeerauthenticationValidYaml,
	"dataset/security-v1beta1-RequestAuthentication-invalid.yaml":    datasetSecurityV1beta1RequestauthenticationInvalidYaml,
	"dataset/security-v1beta1-RequestAuthentication-valid.yaml":      datasetSecurityV1beta1RequestauthenticationValidYaml,
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
	"dataset": &bintree{nil, map[string]*bintree{
		"networking-v1alpha3-DestinationRule-invalid.yaml":       &bintree{datasetNetworkingV1alpha3DestinationruleInvalidYaml, map[string]*bintree{}},
		"networking-v1alpha3-DestinationRule-valid.yaml":         &bintree{datasetNetworkingV1alpha3DestinationruleValidYaml, map[string]*bintree{}},
		"networking-v1alpha3-EnvoyFilter-invalid.yaml":           &bintree{datasetNetworkingV1alpha3EnvoyfilterInvalidYaml, map[string]*bintree{}},
		"networking-v1alpha3-EnvoyFilter-valid.yaml":             &bintree{datasetNetworkingV1alpha3EnvoyfilterValidYaml, map[string]*bintree{}},
		"networking-v1alpha3-Gateway-invalid.yaml":               &bintree{datasetNetworkingV1alpha3GatewayInvalidYaml, map[string]*bintree{}},
		"networking-v1alpha3-Gateway-valid.yaml":                 &bintree{datasetNetworkingV1alpha3GatewayValidYaml, map[string]*bintree{}},
		"networking-v1alpha3-ServiceEntry-invalid.yaml":          &bintree{datasetNetworkingV1alpha3ServiceentryInvalidYaml, map[string]*bintree{}},
		"networking-v1alpha3-ServiceEntry-valid.yaml":            &bintree{datasetNetworkingV1alpha3ServiceentryValidYaml, map[string]*bintree{}},
		"networking-v1alpha3-Sidecar-invalid.yaml":               &bintree{datasetNetworkingV1alpha3SidecarInvalidYaml, map[string]*bintree{}},
		"networking-v1alpha3-Sidecar-valid.yaml":                 &bintree{datasetNetworkingV1alpha3SidecarValidYaml, map[string]*bintree{}},
		"networking-v1alpha3-VirtualService-invalid.yaml":        &bintree{datasetNetworkingV1alpha3VirtualserviceInvalidYaml, map[string]*bintree{}},
		"networking-v1alpha3-VirtualService-regexp-invalid.yaml": &bintree{datasetNetworkingV1alpha3VirtualserviceRegexpInvalidYaml, map[string]*bintree{}},
		"networking-v1alpha3-VirtualService-valid.yaml":          &bintree{datasetNetworkingV1alpha3VirtualserviceValidYaml, map[string]*bintree{}},
		"networking-v1alpha3-WorkloadEntry-invalid.yaml":         &bintree{datasetNetworkingV1alpha3WorkloadentryInvalidYaml, map[string]*bintree{}},
		"networking-v1alpha3-WorkloadEntry-valid.yaml":           &bintree{datasetNetworkingV1alpha3WorkloadentryValidYaml, map[string]*bintree{}},
		"networking-v1alpha3-WorkloadGroup-invalid.yaml":         &bintree{datasetNetworkingV1alpha3WorkloadgroupInvalidYaml, map[string]*bintree{}},
		"networking-v1alpha3-WorkloadGroup-valid.yaml":           &bintree{datasetNetworkingV1alpha3WorkloadgroupValidYaml, map[string]*bintree{}},
		"networking-v1beta-DestinationRule-invalid.yaml":         &bintree{datasetNetworkingV1betaDestinationruleInvalidYaml, map[string]*bintree{}},
		"networking-v1beta-DestinationRule-valid.yaml":           &bintree{datasetNetworkingV1betaDestinationruleValidYaml, map[string]*bintree{}},
		"networking-v1beta-Gateway-invalid.yaml":                 &bintree{datasetNetworkingV1betaGatewayInvalidYaml, map[string]*bintree{}},
		"networking-v1beta-Gateway-valid.yaml":                   &bintree{datasetNetworkingV1betaGatewayValidYaml, map[string]*bintree{}},
		"networking-v1beta-Sidecar-invalid.yaml":                 &bintree{datasetNetworkingV1betaSidecarInvalidYaml, map[string]*bintree{}},
		"networking-v1beta-Sidecar-valid.yaml":                   &bintree{datasetNetworkingV1betaSidecarValidYaml, map[string]*bintree{}},
		"networking-v1beta-VirtualService-invalid.yaml":          &bintree{datasetNetworkingV1betaVirtualserviceInvalidYaml, map[string]*bintree{}},
		"networking-v1beta-VirtualService-valid.yaml":            &bintree{datasetNetworkingV1betaVirtualserviceValidYaml, map[string]*bintree{}},
		"networking-v1beta-WorkloadEntry-invalid.yaml":           &bintree{datasetNetworkingV1betaWorkloadentryInvalidYaml, map[string]*bintree{}},
		"networking-v1beta-WorkloadEntry-valid.yaml":             &bintree{datasetNetworkingV1betaWorkloadentryValidYaml, map[string]*bintree{}},
		"security-v1beta1-AuthorizationPolicy-invalid.yaml":      &bintree{datasetSecurityV1beta1AuthorizationpolicyInvalidYaml, map[string]*bintree{}},
		"security-v1beta1-AuthorizationPolicy-valid.yaml":        &bintree{datasetSecurityV1beta1AuthorizationpolicyValidYaml, map[string]*bintree{}},
		"security-v1beta1-PeerAuthentication-invalid.yaml":       &bintree{datasetSecurityV1beta1PeerauthenticationInvalidYaml, map[string]*bintree{}},
		"security-v1beta1-PeerAuthentication-valid.yaml":         &bintree{datasetSecurityV1beta1PeerauthenticationValidYaml, map[string]*bintree{}},
		"security-v1beta1-RequestAuthentication-invalid.yaml":    &bintree{datasetSecurityV1beta1RequestauthenticationInvalidYaml, map[string]*bintree{}},
		"security-v1beta1-RequestAuthentication-valid.yaml":      &bintree{datasetSecurityV1beta1RequestauthenticationValidYaml, map[string]*bintree{}},
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

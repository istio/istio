// Code generated for package conversion by go-bindata DO NOT EDIT. (@generated)
// sources:
// dataset/config.istio.io/v1alpha2/rule.yaml
// dataset/config.istio.io/v1alpha2/rule_expected.json
// dataset/core/v1/namespace.yaml
// dataset/core/v1/namespace_expected.json
// dataset/core/v1/service.yaml
// dataset/core/v1/service_expected.json
// dataset/extensions/v1beta1/ingress_basic.yaml
// dataset/extensions/v1beta1/ingress_basic_expected.json
// dataset/extensions/v1beta1/ingress_basic_meshconfig.yaml
// dataset/extensions/v1beta1/ingress_merge_0.yaml
// dataset/extensions/v1beta1/ingress_merge_0_expected.json
// dataset/extensions/v1beta1/ingress_merge_0_meshconfig.yaml
// dataset/extensions/v1beta1/ingress_merge_1.yaml
// dataset/extensions/v1beta1/ingress_merge_1_expected.json
// dataset/extensions/v1beta1/ingress_multihost.yaml
// dataset/extensions/v1beta1/ingress_multihost_expected.json
// dataset/extensions/v1beta1/ingress_multihost_meshconfig.yaml
// dataset/mesh.istio.io/v1alpha1/meshconfig.yaml
// dataset/mesh.istio.io/v1alpha1/meshconfig_expected.json
// dataset/networking.istio.io/v1alpha3/destinationRule.yaml
// dataset/networking.istio.io/v1alpha3/destinationRule_expected.json
// dataset/networking.istio.io/v1alpha3/gateway.yaml
// dataset/networking.istio.io/v1alpha3/gateway_expected.json
// dataset/networking.istio.io/v1alpha3/virtualService.yaml
// dataset/networking.istio.io/v1alpha3/virtualServiceWithUnsupported.yaml
// dataset/networking.istio.io/v1alpha3/virtualServiceWithUnsupported_expected.json
// dataset/networking.istio.io/v1alpha3/virtualService_expected.json
package conversion

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

var _datasetConfigIstioIoV1alpha2RuleYaml = []byte(`apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: valid-rule
spec:
  actions:
  - handler: my-handler
    instances: [ my-instance ]
`)

func datasetConfigIstioIoV1alpha2RuleYamlBytes() ([]byte, error) {
	return _datasetConfigIstioIoV1alpha2RuleYaml, nil
}

func datasetConfigIstioIoV1alpha2RuleYaml() (*asset, error) {
	bytes, err := datasetConfigIstioIoV1alpha2RuleYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/config.istio.io/v1alpha2/rule.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetConfigIstioIoV1alpha2Rule_expectedJson = []byte(`{
  "istio/policy/v1beta1/rules": [
    {
      "Metadata": {
        "name": "{{.Namespace}}/valid-rule"
      },
      "Body": {
        "actions": [
          {
            "handler": "my-handler",
            "instances": [
              "my-instance"
            ]
          }
        ]
      },
      "TypeURL": "type.googleapis.com/istio.policy.v1beta1.Rule"
    }
  ]
}
`)

func datasetConfigIstioIoV1alpha2Rule_expectedJsonBytes() ([]byte, error) {
	return _datasetConfigIstioIoV1alpha2Rule_expectedJson, nil
}

func datasetConfigIstioIoV1alpha2Rule_expectedJson() (*asset, error) {
	bytes, err := datasetConfigIstioIoV1alpha2Rule_expectedJsonBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/config.istio.io/v1alpha2/rule_expected.json", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetCoreV1NamespaceYaml = []byte(`apiVersion: v1
kind: Namespace
metadata:
  creationTimestamp: 2019-05-08T17:06:31Z
  name: default
  resourceVersion: "4"
  selfLink: /api/v1/namespaces/default
  uid: a0641b25-71b3-11e9-9fe1-42010a8a0126
spec:
  finalizers:
  - kubernetes
`)

func datasetCoreV1NamespaceYamlBytes() ([]byte, error) {
	return _datasetCoreV1NamespaceYaml, nil
}

func datasetCoreV1NamespaceYaml() (*asset, error) {
	bytes, err := datasetCoreV1NamespaceYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/core/v1/namespace.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetCoreV1Namespace_expectedJson = []byte(`{
    "k8s/core/v1/namespaces": [
        {
            "TypeURL": "type.googleapis.com/k8s.io.api.core.v1.NamespaceSpec",
            "Metadata": {
                "name": "default"
            },
            "Body": {
                "finalizers": [
                    "kubernetes"
                ]
            }
        }
    ]
}
`)

func datasetCoreV1Namespace_expectedJsonBytes() ([]byte, error) {
	return _datasetCoreV1Namespace_expectedJson, nil
}

func datasetCoreV1Namespace_expectedJson() (*asset, error) {
	bytes, err := datasetCoreV1Namespace_expectedJsonBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/core/v1/namespace_expected.json", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetCoreV1ServiceYaml = []byte(`apiVersion: v1
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
  #namespace: kube-system
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
  loadBalancer: {}`)

func datasetCoreV1ServiceYamlBytes() ([]byte, error) {
	return _datasetCoreV1ServiceYaml, nil
}

func datasetCoreV1ServiceYaml() (*asset, error) {
	bytes, err := datasetCoreV1ServiceYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/core/v1/service.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetCoreV1Service_expectedJson = []byte(`{
  "k8s/core/v1/services": [
    {
      "TypeURL": "type.googleapis.com/k8s.io.api.core.v1.ServiceSpec",
      "Metadata": {
        "name": "{{.Namespace}}/kube-dns",
        "annotations": {
          "kubectl.kubernetes.io/last-applied-configuration": "{\"apiVersion\":\"v1\",\"kind\":\"Service\",\"metadata\":{\"annotations\":{},\"labels\":{\"addonmanager.kubernetes.io/mode\":\"Reconcile\",\"k8s-app\":\"kube-dns\",\"kubernetes.io/cluster-service\":\"true\",\"kubernetes.io/name\":\"KubeDNS\"},\"name\":\"kube-dns\",\"namespace\":\"kube-system\"},\"spec\":{\"clusterIP\":\"10.43.240.10\",\"ports\":[{\"name\":\"dns\",\"port\":53,\"protocol\":\"UDP\"},{\"name\":\"dns-tcp\",\"port\":53,\"protocol\":\"TCP\"}],\"selector\":{\"k8s-app\":\"kube-dns\"}}}\n"
        },
        "labels": {
          "addonmanager.kubernetes.io/mode": "Reconcile",
          "k8s-app": "kube-dns",
          "kubernetes.io/cluster-service": "true",
          "kubernetes.io/name": "KubeDNS"
        }
      },
      "Body": {
        "clusterIP": "10.43.240.10",
        "ports": [
          {
            "name": "dns",
            "port": 53,
            "protocol": "UDP",
            "targetPort": 53
          },
          {
            "name": "dns-tcp",
            "port": 53,
            "protocol": "TCP",
            "targetPort": 53
          }
        ],
        "selector": {
          "k8s-app": "kube-dns"
        },
        "sessionAffinity": "None",
        "type": "ClusterIP"
      }
    }
  ]
}
`)

func datasetCoreV1Service_expectedJsonBytes() ([]byte, error) {
	return _datasetCoreV1Service_expectedJson, nil
}

func datasetCoreV1Service_expectedJson() (*asset, error) {
	bytes, err := datasetCoreV1Service_expectedJsonBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/core/v1/service_expected.json", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetExtensionsV1beta1Ingress_basicYaml = []byte(`apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: foo
  annotations:
    kubernetes.io/ingress.class: "cls"

spec:
  backend:
    serviceName: "testsvc"
    servicePort: "80"
`)

func datasetExtensionsV1beta1Ingress_basicYamlBytes() ([]byte, error) {
	return _datasetExtensionsV1beta1Ingress_basicYaml, nil
}

func datasetExtensionsV1beta1Ingress_basicYaml() (*asset, error) {
	bytes, err := datasetExtensionsV1beta1Ingress_basicYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking.k8s.io/v1beta1/ingress_basic.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetExtensionsV1beta1Ingress_basic_expectedJson = []byte(`{
  "istio/networking/v1alpha3/gateways": [
    {
      "TypeURL": "type.googleapis.com/istio.networking.v1alpha3.Gateway",
      "Metadata": {
        "name": "istio-system/foo-istio-autogenerated-k8s-ingress"
      },
      "Body": {
        "selector": {
          "istio": "ingress"
        },
        "servers": [
          {
            "hosts": [
              "*"
            ],
            "port": {
              "name": "http-80-i-foo-{{.Namespace}}",
              "number": 80,
              "protocol": "HTTP"
            }
          }
        ]
      }
    }
  ],

  "istio/networking/v1alpha3/virtualservices": [
  ]
}
`)

func datasetExtensionsV1beta1Ingress_basic_expectedJsonBytes() ([]byte, error) {
	return _datasetExtensionsV1beta1Ingress_basic_expectedJson, nil
}

func datasetExtensionsV1beta1Ingress_basic_expectedJson() (*asset, error) {
	bytes, err := datasetExtensionsV1beta1Ingress_basic_expectedJsonBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking.k8s.io/v1beta1/ingress_basic_expected.json", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetExtensionsV1beta1Ingress_basic_meshconfigYaml = []byte(`ingressClass: cls
ingressControllerMode: STRICT
`)

func datasetExtensionsV1beta1Ingress_basic_meshconfigYamlBytes() ([]byte, error) {
	return _datasetExtensionsV1beta1Ingress_basic_meshconfigYaml, nil
}

func datasetExtensionsV1beta1Ingress_basic_meshconfigYaml() (*asset, error) {
	bytes, err := datasetExtensionsV1beta1Ingress_basic_meshconfigYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking.k8s.io/v1beta1/ingress_basic_meshconfig.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetExtensionsV1beta1Ingress_merge_0Yaml = []byte(`apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: foo
  namespace: ns
  annotations:
    kubernetes.io/ingress.class: "cls"
spec:
  rules:
  - host: foo.bar.com
    http:
      paths:
      - path: /foo
        backend:
          serviceName: service1
          servicePort: 4200
---
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: bar
  namespace: ns
  annotations:
    kubernetes.io/ingress.class: "cls"
spec:
  rules:
  - host: foo.bar.com
    http:
      paths:
      - path: /bar
        backend:
          serviceName: service2
          servicePort: 2400
---
`)

func datasetExtensionsV1beta1Ingress_merge_0YamlBytes() ([]byte, error) {
	return _datasetExtensionsV1beta1Ingress_merge_0Yaml, nil
}

func datasetExtensionsV1beta1Ingress_merge_0Yaml() (*asset, error) {
	bytes, err := datasetExtensionsV1beta1Ingress_merge_0YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking.k8s.io/v1beta1/ingress_merge_0.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetExtensionsV1beta1Ingress_merge_0_expectedJson = []byte(`{
  "istio/networking/v1alpha3/gateways": [
    {
      "Metadata": {
        "name": "istio-system/bar-istio-autogenerated-k8s-ingress"
      },
      "Body": {
        "selector": {
          "istio": "ingress"
        },
        "servers": [
          {
            "hosts": [
              "*"
            ],
            "port": {
              "name": "http-80-i-bar-{{.Namespace}}",
              "number": 80,
              "protocol": "HTTP"
            }
          }
        ]
      },
      "TypeURL": "type.googleapis.com/istio.networking.v1alpha3.Gateway"
    },
    {
      "Metadata": {
        "name": "istio-system/foo-istio-autogenerated-k8s-ingress"
      },
      "Body": {
        "selector": {
          "istio": "ingress"
        },
        "servers": [
          {
            "hosts": [
              "*"
            ],
            "port": {
              "name": "http-80-i-foo-{{.Namespace}}",
              "number": 80,
              "protocol": "HTTP"
            }
          }
        ]
      },
      "TypeURL": "type.googleapis.com/istio.networking.v1alpha3.Gateway"
    }
  ],

  "istio/networking/v1alpha3/virtualservices": [
    {
      "Metadata": {
        "name": "istio-system/foo-bar-com-bar-istio-autogenerated-k8s-ingress"
      },
      "Body": {
        "gateways": [
          "istio-autogenerated-k8s-ingress"
        ],
        "hosts": [
          "foo.bar.com"
        ],
        "http": [
          {
            "match": [
              {
                "uri": {
                  "exact": "/bar"
                }
              }
            ],
            "route": [
              {
                "destination": {
                  "host": "service2.{{.Namespace}}.svc.cluster.local",
                  "port": {
                    "number": 2400
                  }
                },
                "weight": 100
              }
            ]
          },
          {
            "match": [
              {
                "uri": {
                  "exact": "/foo"
                }
              }
            ],
            "route": [
              {
                "destination": {
                  "host": "service1.{{.Namespace}}.svc.cluster.local",
                  "port": {
                    "number": 4200
                  }
                },
                "weight": 100
              }
            ]
          }
        ]
      },
      "TypeURL": "type.googleapis.com/istio.networking.v1alpha3.VirtualService"
    }
  ]
}
`)

func datasetExtensionsV1beta1Ingress_merge_0_expectedJsonBytes() ([]byte, error) {
	return _datasetExtensionsV1beta1Ingress_merge_0_expectedJson, nil
}

func datasetExtensionsV1beta1Ingress_merge_0_expectedJson() (*asset, error) {
	bytes, err := datasetExtensionsV1beta1Ingress_merge_0_expectedJsonBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking.k8s.io/v1beta1/ingress_merge_0_expected.json", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetExtensionsV1beta1Ingress_merge_0_meshconfigYaml = []byte(`ingressClass: cls
ingressControllerMode: STRICT
`)

func datasetExtensionsV1beta1Ingress_merge_0_meshconfigYamlBytes() ([]byte, error) {
	return _datasetExtensionsV1beta1Ingress_merge_0_meshconfigYaml, nil
}

func datasetExtensionsV1beta1Ingress_merge_0_meshconfigYaml() (*asset, error) {
	bytes, err := datasetExtensionsV1beta1Ingress_merge_0_meshconfigYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking.k8s.io/v1beta1/ingress_merge_0_meshconfig.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetExtensionsV1beta1Ingress_merge_1Yaml = []byte(`apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: foo
  namespace: ns
  annotations:
    kubernetes.io/ingress.class: "cls"
spec:
  rules:
  - host: foo.bar.com
    http:
      paths:
      - path: /foo
        backend:
          serviceName: service1
          servicePort: 4200
---
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: bar
  namespace: ns
  annotations:
    kubernetes.io/ingress.class: "cls"
spec:
  rules:
  - host: foo.bar.com
    http:
      paths:
      - path: /bar
        backend:
          # The service has changed since the initial config.
          serviceName: service5
          servicePort: 5000
---
`)

func datasetExtensionsV1beta1Ingress_merge_1YamlBytes() ([]byte, error) {
	return _datasetExtensionsV1beta1Ingress_merge_1Yaml, nil
}

func datasetExtensionsV1beta1Ingress_merge_1Yaml() (*asset, error) {
	bytes, err := datasetExtensionsV1beta1Ingress_merge_1YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking.k8s.io/v1beta1/ingress_merge_1.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetExtensionsV1beta1Ingress_merge_1_expectedJson = []byte(`{
  "istio/networking/v1alpha3/gateways": [
    {
      "Metadata": {
        "name": "istio-system/bar-istio-autogenerated-k8s-ingress"
      },
      "Body": {
        "selector": {
          "istio": "ingress"
        },
        "servers": [
          {
            "hosts": [
              "*"
            ],
            "port": {
              "name": "http-80-i-bar-{{.Namespace}}",
              "number": 80,
              "protocol": "HTTP"
            }
          }
        ]
      },
      "TypeURL": "type.googleapis.com/istio.networking.v1alpha3.Gateway"
    },
    {
      "Metadata": {
        "name": "istio-system/foo-istio-autogenerated-k8s-ingress"
      },
      "Body": {
        "selector": {
          "istio": "ingress"
        },
        "servers": [
          {
            "hosts": [
              "*"
            ],
            "port": {
              "name": "http-80-i-foo-{{.Namespace}}",
              "number": 80,
              "protocol": "HTTP"
            }
          }
        ]
      },
      "TypeURL": "type.googleapis.com/istio.networking.v1alpha3.Gateway"
    }
  ],

  "istio/networking/v1alpha3/virtualservices": [
    {
      "Metadata": {
        "name": "istio-system/foo-bar-com-bar-istio-autogenerated-k8s-ingress"
      },
      "Body": {
        "gateways": [
          "istio-autogenerated-k8s-ingress"
        ],
        "hosts": [
          "foo.bar.com"
        ],
        "http": [
          {
            "match": [
              {
                "uri": {
                  "exact": "/bar"
                }
              }
            ],
            "route": [
              {
                "destination": {
                  "host": "service5.{{.Namespace}}.svc.cluster.local",
                  "port": {
                    "number": 5000
                  }
                },
                "weight": 100
              }
            ]
          },
          {
            "match": [
              {
                "uri": {
                  "exact": "/foo"
                }
              }
            ],
            "route": [
              {
                "destination": {
                  "host": "service1.{{.Namespace}}.svc.cluster.local",
                  "port": {
                    "number": 4200
                  }
                },
                "weight": 100
              }
            ]
          }
        ]
      },
      "TypeURL": "type.googleapis.com/istio.networking.v1alpha3.VirtualService"
    }
  ]
}
`)

func datasetExtensionsV1beta1Ingress_merge_1_expectedJsonBytes() ([]byte, error) {
	return _datasetExtensionsV1beta1Ingress_merge_1_expectedJson, nil
}

func datasetExtensionsV1beta1Ingress_merge_1_expectedJson() (*asset, error) {
	bytes, err := datasetExtensionsV1beta1Ingress_merge_1_expectedJsonBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking.k8s.io/v1beta1/ingress_merge_1_expected.json", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetExtensionsV1beta1Ingress_multihostYaml = []byte(`apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: echo
  annotations:
    kubernetes.io/ingress.class: cls
spec:
  rules:
  - host: echo1.example.com
    http:
      paths:
      - backend:
          serviceName: echo1
          servicePort: 80
  - host: echo2.example.com
    http:
      paths:
      - backend:
          serviceName: echo2
          servicePort: 80`)

func datasetExtensionsV1beta1Ingress_multihostYamlBytes() ([]byte, error) {
	return _datasetExtensionsV1beta1Ingress_multihostYaml, nil
}

func datasetExtensionsV1beta1Ingress_multihostYaml() (*asset, error) {
	bytes, err := datasetExtensionsV1beta1Ingress_multihostYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking.k8s.io/v1beta1/ingress_multihost.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetExtensionsV1beta1Ingress_multihost_expectedJson = []byte(`{
    "istio/networking/v1alpha3/gateways": [
        {
            "Metadata": {
                "name": "istio-system/echo-istio-autogenerated-k8s-ingress"
            },
            "Body": {
                "selector": {
                    "istio": "ingress"
                },
                "servers": [
                    {
                        "hosts": [
                            "*"
                        ],
                        "port": {
                            "name": "http-80-i-echo-{{.Namespace}}",
                            "number": 80,
                            "protocol": "HTTP"
                        }
                    }
                ]
            },
            "TypeURL": "type.googleapis.com/istio.networking.v1alpha3.Gateway"
        }
    ],
    "istio/networking/v1alpha3/virtualservices": [
        {
            "Metadata": {
                "name": "istio-system/echo1-example-com-echo-istio-autogenerated-k8s-ingress"
            },
            "Body": {
                "gateways": [
                    "istio-autogenerated-k8s-ingress"
                ],
                "hosts": [
                    "echo1.example.com"
                ],
                "http": [
                    {
                        "match": [
                            {}
                        ],
                        "route": [
                            {
                                "destination": {
                                    "host": "echo1.{{.Namespace}}.svc.cluster.local",
                                    "port": {
                                        "number": 80
                                    }
                                },
                                "weight": 100
                            }
                        ]
                    }
                ]
            },
            "TypeURL": "type.googleapis.com/istio.networking.v1alpha3.VirtualService"
        },
        {
            "Metadata": {
                "name": "istio-system/echo2-example-com-echo-istio-autogenerated-k8s-ingress"
            },
            "Body": {
                "gateways": [
                    "istio-autogenerated-k8s-ingress"
                ],
                "hosts": [
                    "echo2.example.com"
                ],
                "http": [
                    {
                        "match": [
                            {}
                        ],
                        "route": [
                            {
                                "destination": {
                                    "host": "echo2.{{.Namespace}}.svc.cluster.local",
                                    "port": {
                                        "number": 80
                                    }
                                },
                                "weight": 100
                            }
                        ]
                    }
                ]
            },
            "TypeURL": "type.googleapis.com/istio.networking.v1alpha3.VirtualService"
        }
    ]
}`)

func datasetExtensionsV1beta1Ingress_multihost_expectedJsonBytes() ([]byte, error) {
	return _datasetExtensionsV1beta1Ingress_multihost_expectedJson, nil
}

func datasetExtensionsV1beta1Ingress_multihost_expectedJson() (*asset, error) {
	bytes, err := datasetExtensionsV1beta1Ingress_multihost_expectedJsonBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking.k8s.io/v1beta1/ingress_multihost_expected.json", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetExtensionsV1beta1Ingress_multihost_meshconfigYaml = []byte(`ingressClass: cls
ingressControllerMode: STRICT
`)

func datasetExtensionsV1beta1Ingress_multihost_meshconfigYamlBytes() ([]byte, error) {
	return _datasetExtensionsV1beta1Ingress_multihost_meshconfigYaml, nil
}

func datasetExtensionsV1beta1Ingress_multihost_meshconfigYaml() (*asset, error) {
	bytes, err := datasetExtensionsV1beta1Ingress_multihost_meshconfigYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking.k8s.io/v1beta1/ingress_multihost_meshconfig.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetMeshIstioIoV1alpha1MeshconfigYaml = []byte(``)

func datasetMeshIstioIoV1alpha1MeshconfigYamlBytes() ([]byte, error) {
	return _datasetMeshIstioIoV1alpha1MeshconfigYaml, nil
}

func datasetMeshIstioIoV1alpha1MeshconfigYaml() (*asset, error) {
	bytes, err := datasetMeshIstioIoV1alpha1MeshconfigYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/mesh.istio.io/v1alpha1/meshconfig.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetMeshIstioIoV1alpha1Meshconfig_expectedJson = []byte(`{
    "istio/mesh/v1alpha1/MeshConfig": [
        {
            "TypeURL": "type.googleapis.com/istio.mesh.v1alpha1.MeshConfig",
            "Metadata": {
                "name": "istio-system/meshconfig"
            },
            "Body": {
                "accessLogFile": "/dev/stdout",
                "connectTimeout": {
                    "seconds": 10
                },
                "defaultConfig": {
                  "binaryPath": "/usr/local/bin/envoy",
                  "configPath": "./etc/istio/proxy",
                  "discoveryAddress": "istio-pilot:15010",
                  "drainDuration": {
                    "seconds": 45
                  },
                  "envoyAccessLogService": {},
                  "envoyMetricsService": {},
                  "parentShutdownDuration": {
                    "seconds": 60
                  },
                  "proxyAdminPort": 15000,
                  "serviceCluster": "istio-proxy",
                  "statNameLength": 189,
                  "statusPort": 15020
                },
                "defaultDestinationRuleExportTo": [
                  "*"
                ],
                "defaultServiceExportTo": [
                  "*"
                ],
                "defaultVirtualServiceExportTo": [
                  "*"
                ],
                "disablePolicyChecks": true,
                "dnsRefreshRate": {
                  "seconds": 5
                },
                "enableAutoMtls": {},
                "enableTracing": true,
                "ingressClass": "istio",
                "ingressControllerMode": 3,
                "ingressService": "istio-ingressgateway",
                "localityLbSetting": {},
                "outboundTrafficPolicy": {
                    "mode": 1
                },
                "protocolDetectionTimeout": {
                  "nanos": 100000000
                },
                "proxyListenPort": 15001,
                "reportBatchMaxEntries": 100,
                "reportBatchMaxTime": {
                  "seconds": 1
                },
                "rootNamespace": "istio-system",
                "thriftConfig":{},
                "trustDomain": "cluster.local"
            }
        }
    ]
}
`)

func datasetMeshIstioIoV1alpha1Meshconfig_expectedJsonBytes() ([]byte, error) {
	return _datasetMeshIstioIoV1alpha1Meshconfig_expectedJson, nil
}

func datasetMeshIstioIoV1alpha1Meshconfig_expectedJson() (*asset, error) {
	bytes, err := datasetMeshIstioIoV1alpha1Meshconfig_expectedJsonBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/mesh.istio.io/v1alpha1/meshconfig_expected.json", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingIstioIoV1alpha3DestinationruleYaml = []byte(`apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: tcp-echo-destination
spec:
  host: tcp-echo
  subsets:
  - name: v1
    labels:
      version: v1
  - name: v2
    labels:
      version: v2
`)

func datasetNetworkingIstioIoV1alpha3DestinationruleYamlBytes() ([]byte, error) {
	return _datasetNetworkingIstioIoV1alpha3DestinationruleYaml, nil
}

func datasetNetworkingIstioIoV1alpha3DestinationruleYaml() (*asset, error) {
	bytes, err := datasetNetworkingIstioIoV1alpha3DestinationruleYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking.istio.io/v1alpha3/destinationRule.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingIstioIoV1alpha3Destinationrule_expectedJson = []byte(`{
  "istio/networking/v1alpha3/destinationrules": [
    {
      "TypeURL": "type.googleapis.com/istio.networking.v1alpha3.DestinationRule",
      "Metadata": {
        "name": "{{.Namespace}}/tcp-echo-destination"
      },
      "Body": {
        "host": "tcp-echo",
        "subsets": [
          {
            "labels": {
              "version": "v1"
            },
            "name": "v1"
          },
          {
            "labels": {
              "version": "v2"
            },
            "name": "v2"
          }
        ]
      }
    }
  ]
}
`)

func datasetNetworkingIstioIoV1alpha3Destinationrule_expectedJsonBytes() ([]byte, error) {
	return _datasetNetworkingIstioIoV1alpha3Destinationrule_expectedJson, nil
}

func datasetNetworkingIstioIoV1alpha3Destinationrule_expectedJson() (*asset, error) {
	bytes, err := datasetNetworkingIstioIoV1alpha3Destinationrule_expectedJsonBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking.istio.io/v1alpha3/destinationRule_expected.json", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingIstioIoV1alpha3GatewayYaml = []byte(`apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: helloworld-gateway
spec:
  selector:
    istio: ingressgateway # use istio default controller
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
`)

func datasetNetworkingIstioIoV1alpha3GatewayYamlBytes() ([]byte, error) {
	return _datasetNetworkingIstioIoV1alpha3GatewayYaml, nil
}

func datasetNetworkingIstioIoV1alpha3GatewayYaml() (*asset, error) {
	bytes, err := datasetNetworkingIstioIoV1alpha3GatewayYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking.istio.io/v1alpha3/gateway.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingIstioIoV1alpha3Gateway_expectedJson = []byte(`{
  "istio/networking/v1alpha3/gateways": [
    {
      "TypeURL": "type.googleapis.com/istio.networking.v1alpha3.Gateway",
      "Metadata": {
        "name": "{{.Namespace}}/helloworld-gateway"
      },
      "Body": {
        "selector": {
          "istio": "ingressgateway"
        },
        "servers": [
          {
            "hosts": [
              "*"
            ],
            "port": {
              "name": "http",
              "number": 80,
              "protocol": "HTTP"
            }
          }
        ]
      }
    }
  ]
}
`)

func datasetNetworkingIstioIoV1alpha3Gateway_expectedJsonBytes() ([]byte, error) {
	return _datasetNetworkingIstioIoV1alpha3Gateway_expectedJson, nil
}

func datasetNetworkingIstioIoV1alpha3Gateway_expectedJson() (*asset, error) {
	bytes, err := datasetNetworkingIstioIoV1alpha3Gateway_expectedJsonBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking.istio.io/v1alpha3/gateway_expected.json", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingIstioIoV1alpha3VirtualserviceYaml = []byte(`apiVersion: networking.istio.io/v1alpha3
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

func datasetNetworkingIstioIoV1alpha3VirtualserviceYamlBytes() ([]byte, error) {
	return _datasetNetworkingIstioIoV1alpha3VirtualserviceYaml, nil
}

func datasetNetworkingIstioIoV1alpha3VirtualserviceYaml() (*asset, error) {
	bytes, err := datasetNetworkingIstioIoV1alpha3VirtualserviceYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking.istio.io/v1alpha3/virtualService.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingIstioIoV1alpha3VirtualservicewithunsupportedYaml = []byte(`apiVersion: networking.istio.io/v1alpha3
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
          unsupportedExtraParam: true
        weight: 75
      - destination:
          host: c
          subset: v2
        weight: 25
        unsupportedExtraParam: true
`)

func datasetNetworkingIstioIoV1alpha3VirtualservicewithunsupportedYamlBytes() ([]byte, error) {
	return _datasetNetworkingIstioIoV1alpha3VirtualservicewithunsupportedYaml, nil
}

func datasetNetworkingIstioIoV1alpha3VirtualservicewithunsupportedYaml() (*asset, error) {
	bytes, err := datasetNetworkingIstioIoV1alpha3VirtualservicewithunsupportedYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking.istio.io/v1alpha3/virtualServiceWithUnsupported.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingIstioIoV1alpha3Virtualservicewithunsupported_expectedJson = []byte(`{
  "istio/networking/v1alpha3/virtualservices": [
    {
      "Metadata": {
        "name": "{{.Namespace}}/valid-virtual-service"
      },
      "Body": {
        "hosts": [
          "c"
        ],
        "http": [
          {
            "route": [
              {
                "destination": {
                  "host": "c",
                  "subset": "v1"
                },
                "weight": 75
              },
              {
                "destination": {
                  "host": "c",
                  "subset": "v2"
                },
                "weight": 25
              }
            ]
          }
        ]
      },
      "TypeURL": "type.googleapis.com/istio.networking.v1alpha3.VirtualService"
    }
  ]
}
`)

func datasetNetworkingIstioIoV1alpha3Virtualservicewithunsupported_expectedJsonBytes() ([]byte, error) {
	return _datasetNetworkingIstioIoV1alpha3Virtualservicewithunsupported_expectedJson, nil
}

func datasetNetworkingIstioIoV1alpha3Virtualservicewithunsupported_expectedJson() (*asset, error) {
	bytes, err := datasetNetworkingIstioIoV1alpha3Virtualservicewithunsupported_expectedJsonBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking.istio.io/v1alpha3/virtualServiceWithUnsupported_expected.json", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _datasetNetworkingIstioIoV1alpha3Virtualservice_expectedJson = []byte(`{
  "istio/networking/v1alpha3/virtualservices": [
    {
      "Metadata": {
        "name": "{{.Namespace}}/valid-virtual-service"
      },
      "Body": {
        "hosts": [
          "c"
        ],
        "http": [
          {
            "route": [
              {
                "destination": {
                  "host": "c",
                  "subset": "v1"
                },
                "weight": 75
              },
              {
                "destination": {
                  "host": "c",
                  "subset": "v2"
                },
                "weight": 25
              }
            ]
          }
        ]
      },
      "TypeURL": "type.googleapis.com/istio.networking.v1alpha3.VirtualService"
    }
  ]
}
`)

func datasetNetworkingIstioIoV1alpha3Virtualservice_expectedJsonBytes() ([]byte, error) {
	return _datasetNetworkingIstioIoV1alpha3Virtualservice_expectedJson, nil
}

func datasetNetworkingIstioIoV1alpha3Virtualservice_expectedJson() (*asset, error) {
	bytes, err := datasetNetworkingIstioIoV1alpha3Virtualservice_expectedJsonBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "dataset/networking.istio.io/v1alpha3/virtualService_expected.json", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
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
	"dataset/config.istio.io/v1alpha2/rule.yaml":                                       datasetConfigIstioIoV1alpha2RuleYaml,
	"dataset/config.istio.io/v1alpha2/rule_expected.json":                              datasetConfigIstioIoV1alpha2Rule_expectedJson,
	"dataset/core/v1/namespace.yaml":                                                   datasetCoreV1NamespaceYaml,
	"dataset/core/v1/namespace_expected.json":                                          datasetCoreV1Namespace_expectedJson,
	"dataset/core/v1/service.yaml":                                                     datasetCoreV1ServiceYaml,
	"dataset/core/v1/service_expected.json":                                            datasetCoreV1Service_expectedJson,
	"dataset/networking.k8s.io/v1beta1/ingress_basic.yaml":                             datasetExtensionsV1beta1Ingress_basicYaml,
	"dataset/networking.k8s.io/v1beta1/ingress_basic_expected.json":                    datasetExtensionsV1beta1Ingress_basic_expectedJson,
	"dataset/networking.k8s.io/v1beta1/ingress_basic_meshconfig.yaml":                  datasetExtensionsV1beta1Ingress_basic_meshconfigYaml,
	"dataset/networking.k8s.io/v1beta1/ingress_merge_0.yaml":                           datasetExtensionsV1beta1Ingress_merge_0Yaml,
	"dataset/networking.k8s.io/v1beta1/ingress_merge_0_expected.json":                  datasetExtensionsV1beta1Ingress_merge_0_expectedJson,
	"dataset/networking.k8s.io/v1beta1/ingress_merge_0_meshconfig.yaml":                datasetExtensionsV1beta1Ingress_merge_0_meshconfigYaml,
	"dataset/networking.k8s.io/v1beta1/ingress_merge_1.yaml":                           datasetExtensionsV1beta1Ingress_merge_1Yaml,
	"dataset/networking.k8s.io/v1beta1/ingress_merge_1_expected.json":                  datasetExtensionsV1beta1Ingress_merge_1_expectedJson,
	"dataset/networking.k8s.io/v1beta1/ingress_multihost.yaml":                         datasetExtensionsV1beta1Ingress_multihostYaml,
	"dataset/networking.k8s.io/v1beta1/ingress_multihost_expected.json":                datasetExtensionsV1beta1Ingress_multihost_expectedJson,
	"dataset/networking.k8s.io/v1beta1/ingress_multihost_meshconfig.yaml":              datasetExtensionsV1beta1Ingress_multihost_meshconfigYaml,
	"dataset/mesh.istio.io/v1alpha1/meshconfig.yaml":                                   datasetMeshIstioIoV1alpha1MeshconfigYaml,
	"dataset/mesh.istio.io/v1alpha1/meshconfig_expected.json":                          datasetMeshIstioIoV1alpha1Meshconfig_expectedJson,
	"dataset/networking.istio.io/v1alpha3/destinationRule.yaml":                        datasetNetworkingIstioIoV1alpha3DestinationruleYaml,
	"dataset/networking.istio.io/v1alpha3/destinationRule_expected.json":               datasetNetworkingIstioIoV1alpha3Destinationrule_expectedJson,
	"dataset/networking.istio.io/v1alpha3/gateway.yaml":                                datasetNetworkingIstioIoV1alpha3GatewayYaml,
	"dataset/networking.istio.io/v1alpha3/gateway_expected.json":                       datasetNetworkingIstioIoV1alpha3Gateway_expectedJson,
	"dataset/networking.istio.io/v1alpha3/virtualService.yaml":                         datasetNetworkingIstioIoV1alpha3VirtualserviceYaml,
	"dataset/networking.istio.io/v1alpha3/virtualServiceWithUnsupported.yaml":          datasetNetworkingIstioIoV1alpha3VirtualservicewithunsupportedYaml,
	"dataset/networking.istio.io/v1alpha3/virtualServiceWithUnsupported_expected.json": datasetNetworkingIstioIoV1alpha3Virtualservicewithunsupported_expectedJson,
	"dataset/networking.istio.io/v1alpha3/virtualService_expected.json":                datasetNetworkingIstioIoV1alpha3Virtualservice_expectedJson,
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
		"config.istio.io": &bintree{nil, map[string]*bintree{
			"v1alpha2": &bintree{nil, map[string]*bintree{
				"rule.yaml":          &bintree{datasetConfigIstioIoV1alpha2RuleYaml, map[string]*bintree{}},
				"rule_expected.json": &bintree{datasetConfigIstioIoV1alpha2Rule_expectedJson, map[string]*bintree{}},
			}},
		}},
		"core": &bintree{nil, map[string]*bintree{
			"v1": &bintree{nil, map[string]*bintree{
				"namespace.yaml":          &bintree{datasetCoreV1NamespaceYaml, map[string]*bintree{}},
				"namespace_expected.json": &bintree{datasetCoreV1Namespace_expectedJson, map[string]*bintree{}},
				"service.yaml":            &bintree{datasetCoreV1ServiceYaml, map[string]*bintree{}},
				"service_expected.json":   &bintree{datasetCoreV1Service_expectedJson, map[string]*bintree{}},
			}},
		}},
		"extensions": &bintree{nil, map[string]*bintree{
			"v1beta1": &bintree{nil, map[string]*bintree{
				"ingress_basic.yaml":                &bintree{datasetExtensionsV1beta1Ingress_basicYaml, map[string]*bintree{}},
				"ingress_basic_expected.json":       &bintree{datasetExtensionsV1beta1Ingress_basic_expectedJson, map[string]*bintree{}},
				"ingress_basic_meshconfig.yaml":     &bintree{datasetExtensionsV1beta1Ingress_basic_meshconfigYaml, map[string]*bintree{}},
				"ingress_merge_0.yaml":              &bintree{datasetExtensionsV1beta1Ingress_merge_0Yaml, map[string]*bintree{}},
				"ingress_merge_0_expected.json":     &bintree{datasetExtensionsV1beta1Ingress_merge_0_expectedJson, map[string]*bintree{}},
				"ingress_merge_0_meshconfig.yaml":   &bintree{datasetExtensionsV1beta1Ingress_merge_0_meshconfigYaml, map[string]*bintree{}},
				"ingress_merge_1.yaml":              &bintree{datasetExtensionsV1beta1Ingress_merge_1Yaml, map[string]*bintree{}},
				"ingress_merge_1_expected.json":     &bintree{datasetExtensionsV1beta1Ingress_merge_1_expectedJson, map[string]*bintree{}},
				"ingress_multihost.yaml":            &bintree{datasetExtensionsV1beta1Ingress_multihostYaml, map[string]*bintree{}},
				"ingress_multihost_expected.json":   &bintree{datasetExtensionsV1beta1Ingress_multihost_expectedJson, map[string]*bintree{}},
				"ingress_multihost_meshconfig.yaml": &bintree{datasetExtensionsV1beta1Ingress_multihost_meshconfigYaml, map[string]*bintree{}},
			}},
		}},
		"mesh.istio.io": &bintree{nil, map[string]*bintree{
			"v1alpha1": &bintree{nil, map[string]*bintree{
				"meshconfig.yaml":          &bintree{datasetMeshIstioIoV1alpha1MeshconfigYaml, map[string]*bintree{}},
				"meshconfig_expected.json": &bintree{datasetMeshIstioIoV1alpha1Meshconfig_expectedJson, map[string]*bintree{}},
			}},
		}},
		"networking.istio.io": &bintree{nil, map[string]*bintree{
			"v1alpha3": &bintree{nil, map[string]*bintree{
				"destinationRule.yaml":                        &bintree{datasetNetworkingIstioIoV1alpha3DestinationruleYaml, map[string]*bintree{}},
				"destinationRule_expected.json":               &bintree{datasetNetworkingIstioIoV1alpha3Destinationrule_expectedJson, map[string]*bintree{}},
				"gateway.yaml":                                &bintree{datasetNetworkingIstioIoV1alpha3GatewayYaml, map[string]*bintree{}},
				"gateway_expected.json":                       &bintree{datasetNetworkingIstioIoV1alpha3Gateway_expectedJson, map[string]*bintree{}},
				"virtualService.yaml":                         &bintree{datasetNetworkingIstioIoV1alpha3VirtualserviceYaml, map[string]*bintree{}},
				"virtualServiceWithUnsupported.yaml":          &bintree{datasetNetworkingIstioIoV1alpha3VirtualservicewithunsupportedYaml, map[string]*bintree{}},
				"virtualServiceWithUnsupported_expected.json": &bintree{datasetNetworkingIstioIoV1alpha3Virtualservicewithunsupported_expectedJson, map[string]*bintree{}},
				"virtualService_expected.json":                &bintree{datasetNetworkingIstioIoV1alpha3Virtualservice_expectedJson, map[string]*bintree{}},
			}},
		}},
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

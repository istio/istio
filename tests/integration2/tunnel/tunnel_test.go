package tunnel

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework/components/apps"
	"istio.io/istio/pkg/test/framework/components/egress"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/ingress"

	v1 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/util/tmpl"
)

const (
	clientSideEgressConfig = `
---
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  labels:
    service: client
  name: istio-egressgateway-client
spec:
  selector:
    istio: egressgateway
  servers:
  - hosts:
    - {{ .sidecarSNI }}
    port:
      name: tcp-port-443
      number: 443
      protocol: TLS
    tls:
      caCertificates: /etc/certs/root-cert.pem
      mode: MUTUAL
      privateKey: /etc/certs/key.pem
      serverCertificate: /etc/certs/cert-chain.pem
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: egress-gateway-client
spec:
  gateways:
  - istio-egressgateway-client
  hosts:
  - {{ .sidecarSNI }}
  tcp:
  - match:
    - gateways:
      - istio-egressgateway-client
      port: 443
    route:
    - destination:
        host: {{ .ingressDNS }}
        port:
          number: 443
        subset: client-2
---
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: egressgateway-client
  namespace: {{ .systemNamespace }}
spec:
  host: {{ .ingressDNS }}
  exportTo: [ "." ]
  subsets:
  - name: client-2
    trafficPolicy:
      portLevelSettings:
      - port:
          number: 443
        tls:
          caCertificates: /etc/istio/tunnel-certs/ca.crt
          clientCertificate: /etc/istio/tunnel-certs/client.crt
          mode: MUTUAL
          privateKey: /etc/istio/tunnel-certs/client.key
          sni: {{ .ingressDNS }}
          subjectAltNames:
          - {{ .ingressDNS }}
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  creationTimestamp: null
  name: ingress-service-entry
spec:
  endpoints:
  - address: {{.ingressAddress}}
  hosts:
  - {{ .ingressDNS }}
  ports:
  - name: index-binding-id-7777
    number: {{.ingressPort}}
    protocol: TCP
  resolution: STATIC
`

	clientSideConfig = `
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: mesh-to-egress-client
spec:
  gateways:
  - mesh
  hosts:
  - {{ .serviceName }}
  tcp:
  - match:
    - destinationSubnets:
      - {{ .vip }}
      gateways:
      - mesh
    route:
    - destination:
        host: istio-egressgateway.{{ .systemNamespace }}.svc.cluster.local
        port:
          number: 443
        subset: client
---
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: sidecar-to-egress-client
spec:
  host: istio-egressgateway.{{ .systemNamespace }}.svc.cluster.local
  subsets:
  - name: client
    trafficPolicy:
      tls:
        mode: ISTIO_MUTUAL
        sni: {{ .sidecarSNI }}
`
	serverSideConfig = `---
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  creationTimestamp: null
  name: index-binding-id-gateway-tls
spec:
  selector:
    istio: ingressgateway # use Istio default gateway implementation
  servers:
  - hosts:
    - "*"
    port:
      name: tls
      number: 443
      protocol: TLS
    tls:
      caCertificates: /etc/istio/tunnel-certs/ca.crt
      mode: MUTUAL
      privateKey: /etc/istio/tunnel-certs/service.key
      serverCertificate: /etc/istio/tunnel-certs/service.crt
      subjectAltNames:
      - {{ .clientSAN }}
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  creationTimestamp: null
  name: index-binding-id-virtual-service-tls
spec:
  gateways:
  - index-binding-id-gateway-tls
  hosts:
  - {{ .ingressDNS }}
  tcp:
  - route:
    - destination:
        host: index-binding-id.service-fabrik
        port:
          number: {{ .port }}
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  creationTimestamp: null
  name: index-binding-id-service-entry
spec:
  endpoints:
  - address: {{ .address }}
  hosts:
  - index-binding-id.service-fabrik
  ports:
  - name: index-binding-id-7777
    number: {{ .port }}
    protocol: TCP
  resolution: STATIC

`
)

//      +--------------+
//  +---|  a-app       |
//  :   +--------------+   mTLS  +------------------+   mTLS  +------------------------+
//  |   |  sidecar     +-------->|  egress gateway  +-------->|  istio-ingressgateway  |
//  |   +------+-------+         +------------------+         +------------+-----------+
//  |                                                                   |
//  |                                                                   |
//  |                                                                   v
//  |                                                       +----------------------------+      +-----------+
//  |                                                       |  istio traffic management  +----->|   t-app   |
//  |                                                       +----------------------------+      +-----------+
//  |                                                                                                 ^
//  |                                                                                                 |
//  +-------------------------------------------------------------------------------------------------+

// This tests a basic scenario of how istio can be used inside kubernetes.
// An app a tries to communicate with the service t-app.
// To do so its sidecar sends the request as mutual tls to the egress gateway.
// But instead of leaving the istio mesh the request is forwarded to the ingress gateway and consecutively to the service.
// The actual service is hidden behind the istio construct of Gateway+VirtualService+ServiceEntry.

func TestTunnel(t *testing.T) {
	ctx := framework.NewContext(t)
	defer ctx.Done(t)

	ctx.RequireOrSkip(t, environment.Kube)

	secret := &v1.Secret{
		Data: map[string][]byte{
			"ca.crt":      readFileOrFail("certs/ca.crt", t),
			"service.crt": readFileOrFail("certs/service.crt", t),
			"service.key": readFileOrFail("certs/service.key", t),
			"client.crt":  readFileOrFail("certs/client.crt", t),
			"client.key":  readFileOrFail("certs/client.key", t),
		}}
	_ = egress.NewOrFail(t, ctx, egress.Config{
		Istio:                      ist,
		Secret:                     secret,
		AdditionalSecretMountPoint: "/etc/istio/tunnel-certs",
	})

	ingress := ingress.NewOrFail(t, ctx, ingress.Config{
		Istio:                      ist,
		Secret:                     secret,
		AdditionalSecretMountPoint: "/etc/istio/tunnel-certs",
	})

	pilot := pilot.NewOrFail(t, ctx, pilot.Config{})
	applications := apps.NewOrFail(ctx, t, apps.Config{Pilot: pilot})
	a := applications.GetAppOrFail("a", t)
	b := applications.GetAppOrFail("t", t).(apps.KubeApp)

	be := b.EndpointsForProtocol(model.ProtocolHTTP)[0].(apps.KubeEndpoint)

	ingressURL, err := ingress.URL(model.ProtocolHTTPS)
	if err != nil {
		t.Fatal(err)
	}

	ingressPort := ingressURL.Port()

	beURL := be.URL()
	virtualPort := 8080
	serviceName := "client"
	env := ctx.Environment().(*kube.Environment)
	cfg, err := istio.DefaultConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}

	clientNamespace := namespace.NewOrFail(t, ctx, "client", true)
	virtualIP := env.AllocateIPAddressOrFail(virtualPort, serviceName, clientNamespace.Name(), t)
	serverNamespace := namespace.NewOrFail(t, ctx, "server", true)

	err = env.ApplyContents(cfg.SystemNamespace,
		dump(tmpl.EvaluateOrFail(t, clientSideEgressConfig, map[string]interface{}{
			"ingressAddress":  ingressURL.Hostname(),
			"ingressPort":     ingressPort,
			"ingressDNS":      "service.istio.test.local", // Must match CN in certs/server.crt
			"sidecarSNI":      "sni.of.destination.rule.in.sidecar",
			"systemNamespace": cfg.SystemNamespace,
		})))

	if err != nil {
		t.Fatal(err)
	}

	err = env.ApplyContents(clientNamespace.Name(),
		dump(tmpl.EvaluateOrFail(t, clientSideConfig, map[string]interface{}{
			"vip":             virtualIP,
			"serviceName":     serviceName,
			"sidecarSNI":      "sni.of.destination.rule.in.sidecar",
			"systemNamespace": cfg.SystemNamespace,
		})),
	)
	if err != nil {
		t.Fatal(err)
	}

	err = env.ApplyContents(serverNamespace.Name(),
		dump(tmpl.EvaluateOrFail(t, serverSideConfig, map[string]interface{}{
			"address":    b.ClusterIP(),
			"port":       beURL.Port(),
			"ingressDNS": "service.istio.test.local", // Must match CN in certs/server.crt
			"clientSAN":  "client.istio.test.local",  // Must match CN and SAN in certs/client.crt
		})),
	)

	if err != nil {
		t.Fatal(err)
	}
	log.Infof("wait for 10 seconds for config distribution.") // see https://github.com/istio/istio/issues/6170
	time.Sleep(10 * time.Second)

	be.SetURL(&url.URL{Host: fmt.Sprintf("%s:%d", virtualIP, virtualPort), Path: beURL.Path, Scheme: beURL.Scheme})
	log.Infof("Trying to call %s", be.URL().String())

	result := a.CallOrFail(be, apps.AppCallOptions{IgnoreWrongPort: true}, t)[0]

	if !result.IsOK() {
		t.Fatalf("HTTP Request unsuccessful: %s", result.Body)
	}
}

func dump(yaml string) string {
	fmt.Println(yaml)
	return yaml
}

func readFileOrFail(filename string, t testing.TB) []byte {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

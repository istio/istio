// Copyright 2019 Istio Authors
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

package ingress

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/istioio"
)

// https://preliminary.istio.io/docs/tasks/traffic-management/ingress/secure-ingress-sds/
func TestSecureIngressSDS(t *testing.T) {
	framework.
		NewTest(t).
		Run(istioio.NewBuilder("traffic_management__ingress__secure_gateways_sds").
			// Configure a TLS ingress gateway for a single host.
			// https://preliminary.istio.io/docs/tasks/traffic-management/ingress/secure-ingress-sds/#configure-a-tls-ingress-gateway-for-a-single-host
			Add(istioio.Command{
				Input: istioio.Inline{
					FileName: "httpbin_deployment.sh",
					Value: `
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Service
metadata:
  name: httpbin
  labels:
    app: httpbin
spec:
  ports:
  - name: http
    port: 8000
  selector:
    app: httpbin
---
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: httpbin
spec:
  replicas: 1
  template:
    metadata:
      labels:
        app: httpbin
        version: v1
    spec:
      containers:
      - image: docker.io/citizenstig/httpbin
        imagePullPolicy: IfNotPresent
        name: httpbin
        ports:
        - containerPort: 8000
EOF`,
				},
			}, istioio.Command{
				Input: istioio.Inline{
					FileName: "httpbin_gen_keys.sh",
					Value:    `./scripts/mtls-go-example.sh "httpbin.example.com" "httpbin.example.com"`,
				},
			}, istioio.Command{
				Input: istioio.Inline{
					FileName: "create_httpbin_tls_secret.sh",
					Value: `
kubectl create -n istio-system secret generic httpbin-credential \
--from-file=key=httpbin.example.com/3_application/private/httpbin.example.com.key.pem \
--from-file=cert=httpbin.example.com/3_application/certs/httpbin.example.com.cert.pem

kubectl get -n istio-system secret httpbin-credential`,
				},
			}, istioio.Command{
				Input: istioio.Inline{
					FileName: "create_httpbin_tls_gateway.sh",
					Value: `
cat <<EOF | kubectl apply -f -
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: mygateway
spec:
  selector:
    istio: ingressgateway # use istio default ingress gateway
  servers:
  - port:
      number: 443
      name: https
      protocol: HTTPS
    tls:
      mode: SIMPLE
      credentialName: "httpbin-credential" # must be the same as secret
    hosts:
    - "httpbin.example.com"
EOF`,
				},
			}, istioio.Command{
				Input: istioio.Inline{
					FileName: "create_httpbin_ingress_routes.sh",
					Value: `
cat <<EOF | kubectl apply -f -
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: httpbin
spec:
  hosts:
  - "httpbin.example.com"
  gateways:
  - mygateway
  http:
  - match:
    - uri:
        prefix: /status
    - uri:
        prefix: /delay
    route:
    - destination:
        port:
          number: 8000
        host: httpbin
EOF`,
				},
			}, istioio.Command{
				Input: istioio.Inline{
					FileName: "check_envoy_sds_update_1.sh",
					Value: `
for it in {1..15}
do
  stats=$(kubectl -n istio-system exec "$(kubectl get pod -l istio=ingressgateway -n istio-system \
  -o jsonpath={.items..metadata.name})" -c istio-proxy -- \
  curl 127.0.0.1:15000/stats?filter=listener.0.0.0.0_443.server_ssl_socket_factory.ssl_context_update_by_sds)
  if [[ "$stats" == "listener.0.0.0.0_443.server_ssl_socket_factory.ssl_context_update_by_sds: 1" ]]; then
    echo "Envoy stats listener.0.0.0.0_443.server_ssl_socket_factory.ssl_context_update_by_sds meets expectation with ${it} retries"
    exit
  else
    sleep 2s
  fi
done

echo "ingress gateway SDS stats does not meet in 30 seconds. Expected 1 but got ${stats}" >&2`,
				},
			}, istioio.Command{
				Input: istioio.IfMinikube{
					Then: istioio.Inline{
						FileName: "curl_httpbin_tls_gateway_minikube.sh",
						Value: `
set -x

INGRESS_HOST=$(kubectl -n istio-system get pod -l istio=ingressgateway -o jsonpath='{.items[0].status.hostIP}')
export INGRESS_HOST

SECURE_INGRESS_PORT=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.port==443)].nodePort}')
export SECURE_INGRESS_PORT

curl -v -HHost:httpbin.example.com \
--resolve httpbin.example.com:"$SECURE_INGRESS_PORT":"$INGRESS_HOST" \
--cacert httpbin.example.com/2_intermediate/certs/ca-chain.cert.pem \
https://httpbin.example.com:"$SECURE_INGRESS_PORT"/status/418`,
					},
					Else: istioio.Inline{
						FileName: "curl_httpbin_tls_gateway_gke.sh",
						Value: `
set -x

INGRESS_HOST=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
export INGRESS_HOST

SECURE_INGRESS_PORT=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.name=="https")].port}')
export SECURE_INGRESS_PORT

curl -v -HHost:httpbin.example.com \
--resolve httpbin.example.com:"$SECURE_INGRESS_PORT":"$INGRESS_HOST" \
--cacert httpbin.example.com/2_intermediate/certs/ca-chain.cert.pem \
https://httpbin.example.com:"$SECURE_INGRESS_PORT"/status/418`,
					},
				},
			}, istioio.Command{
				Input: istioio.Inline{
					FileName: "rotate_httpbin_tls_secret.sh",
					Value: `
echo "Delete the gateway’s secret and create a new one to change the ingress gateway’s credentials."

kubectl -n istio-system delete secret httpbin-credential

sh ./scripts/mtls-go-example.sh "httpbin.new.example.com"

kubectl create -n istio-system secret generic httpbin-credential \
--from-file=key=httpbin.new.example.com/3_application/private/httpbin.example.com.key.pem \
--from-file=cert=httpbin.new.example.com/3_application/certs/httpbin.example.com.cert.pem

kubectl get -n istio-system secret httpbin-credential`,
				},
			}, istioio.Command{
				Input: istioio.Inline{
					FileName: "check_envoy_sds_update_2.sh",
					Value: `
for it in {1..15}
do
  stats=$(kubectl -n istio-system exec "$(kubectl get pod -l istio=ingressgateway -n istio-system \
  -o jsonpath={.items..metadata.name})" -c istio-proxy -- \
  curl 127.0.0.1:15000/stats?filter=listener.0.0.0.0_443.server_ssl_socket_factory.ssl_context_update_by_sds)
  if [[ "$stats" == "listener.0.0.0.0_443.server_ssl_socket_factory.ssl_context_update_by_sds: 2" ]]; then
    echo "Envoy stats listener.0.0.0.0_443.server_ssl_socket_factory.ssl_context_update_by_sds meets expectation with ${it} retries"
    exit
  else
    sleep 2s
  fi
done

echo "ingress gateway SDS stats does not meet in 30 seconds. Expected 2 but got ${stats}" >&2`,
				},
			}).

			// Rotate secret and send HTTPS request with new credentials.
			Add(istioio.Command{
				Input: istioio.IfMinikube{
					Then: istioio.Inline{
						FileName: "curl_httpbin_tls_gateway_minikube_new_tls_secret.sh",
						Value: `
set -x

INGRESS_HOST=$(kubectl -n istio-system get pod -l istio=ingressgateway -o jsonpath='{.items[0].status.hostIP}')
export INGRESS_HOST

SECURE_INGRESS_PORT=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.port==443)].nodePort}')
export SECURE_INGRESS_PORT

curl -v -HHost:httpbin.example.com \
--resolve httpbin.example.com:"$SECURE_INGRESS_PORT":"$INGRESS_HOST" \
--cacert httpbin.new.example.com/2_intermediate/certs/ca-chain.cert.pem \
https://httpbin.example.com:"$SECURE_INGRESS_PORT"/status/418`,
					},
					Else: istioio.Inline{
						FileName: "curl_httpbin_tls_gateway_gke_new_tls_secret.sh",
						Value: `
set -x

INGRESS_HOST=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
export INGRESS_HOST

SECURE_INGRESS_PORT=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.name=="https")].port}')
export SECURE_INGRESS_PORT

curl -v -HHost:httpbin.example.com \
--resolve httpbin.example.com:"$SECURE_INGRESS_PORT":"$INGRESS_HOST" \
--cacert httpbin.example.com/2_intermediate/certs/ca-chain.cert.pem \
https://httpbin.example.com:"$SECURE_INGRESS_PORT"/status/418`,
					},
				},
			}).

			// Configure a TLS ingress gateway for multiple hosts.
			// https://preliminary.istio.io/docs/tasks/traffic-management/ingress/secure-ingress-sds/#configure-a-tls-ingress-gateway-for-multiple-hosts
			Add(istioio.Command{
				Input: istioio.Inline{
					FileName: "restore_httpbin_tls_secret.sh",
					Value: `
echo "restore the credentials for httpbin"

kubectl -n istio-system delete secret httpbin-credential

kubectl create -n istio-system secret generic httpbin-credential \
--from-file=key=httpbin.example.com/3_application/private/httpbin.example.com.key.pem \
--from-file=cert=httpbin.example.com/3_application/certs/httpbin.example.com.cert.pem`,
				},
			}, istioio.Command{
				Input: istioio.Inline{
					FileName: "helloworld_deployment.sh",
					Value: `
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Service
metadata:
  name: helloworld-v1
  labels:
    app: helloworld-v1
spec:
  ports:
  - name: http
    port: 5000
  selector:
    app: helloworld-v1
---
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: helloworld-v1
spec:
  replicas: 1
  template:
    metadata:
      labels:
        app: helloworld-v1
    spec:
      containers:
      - name: helloworld
        image: istio/examples-helloworld-v1
        resources:
          requests:
            cpu: "100m"
        imagePullPolicy: IfNotPresent #Always
        ports:
        - containerPort: 5000
EOF`,
				},
			}, istioio.Command{
				Input: istioio.Inline{
					FileName: "create_helloworld_tls_secret.sh",
					Value: `
sh ./scripts/mtls-go-example.sh "helloworld-v1.example.com" "helloworld-v1.example.com"

kubectl create -n istio-system secret generic helloworld-credential \
--from-file=key=helloworld-v1.example.com/3_application/private/helloworld-v1.example.com.key.pem \
--from-file=cert=helloworld-v1.example.com/3_application/certs/helloworld-v1.example.com.cert.pem

kubectl get -n istio-system secret helloworld-credential`,
				},
			}, istioio.Command{
				Input: istioio.Inline{
					FileName: "create_helloworld_tls_gateway.sh",
					Value: `
cat <<EOF | kubectl apply -f -
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: mygateway
spec:
  selector:
    istio: ingressgateway # use istio default ingress gateway
  servers:
  - port:
      number: 443
      name: https-httpbin
      protocol: HTTPS
    tls:
      mode: SIMPLE
      credentialName: "httpbin-credential"
    hosts:
    - "httpbin.example.com"
  - port:
      number: 443
      name: https-helloworld
      protocol: HTTPS
    tls:
      mode: SIMPLE
      credentialName: "helloworld-credential"
    hosts:
    - "helloworld-v1.example.com"
EOF

cat <<EOF | kubectl apply -f -
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: helloworld-v1
spec:
  hosts:
  - "helloworld-v1.example.com"
  gateways:
  - mygateway
  http:
  - match:
    - uri:
        exact: /hello
    route:
    - destination:
        host: helloworld-v1
        port:
          number: 5000
EOF`,
				},
			}).

			// Send an HTTPS request to access the helloworld service TLS gateway.
			Add(istioio.Command{
				Input: istioio.Inline{
					FileName: "check_envoy_sds_update_4.sh",
					Value: `
for it in {1..15}
do
  stats=$(kubectl -n istio-system exec "$(kubectl get pod -l istio=ingressgateway -n istio-system \
  -o jsonpath={.items..metadata.name})" -c istio-proxy -- \
  curl 127.0.0.1:15000/stats?filter=listener.0.0.0.0_443.server_ssl_socket_factory.ssl_context_update_by_sds)
  if [[ "$stats" == "listener.0.0.0.0_443.server_ssl_socket_factory.ssl_context_update_by_sds: 4" ]]; then
    echo "Envoy stats listener.0.0.0.0_443.server_ssl_socket_factory.ssl_context_update_by_sds meets expectation with ${it} retries"
    exit
  else
    sleep 2s
  fi
done

echo "ingress gateway SDS stats does not meet in 30 seconds. Expected 4 but got ${stats}" >&2`,
				},
			}, istioio.Command{
				Input: istioio.IfMinikube{
					Then: istioio.Inline{
						FileName: "curl_helloworld_tls_gateway_minikube.sh",
						Value: `
set -x

INGRESS_HOST=$(kubectl -n istio-system get pod -l istio=ingressgateway -o jsonpath='{.items[0].status.hostIP}')
export INGRESS_HOST

SECURE_INGRESS_PORT=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.port==443)].nodePort}')
export SECURE_INGRESS_PORT

curl -v -HHost:helloworld-v1.example.com \
--resolve helloworld-v1.example.com:"$SECURE_INGRESS_PORT":"$INGRESS_HOST" \
--cacert helloworld-v1.example.com/2_intermediate/certs/ca-chain.cert.pem \
https://helloworld-v1.example.com:"$SECURE_INGRESS_PORT"/hello`,
					},
					Else: istioio.Inline{
						FileName: "curl_helloworld_tls_gateway_gke.sh",
						Value: `
set -x

INGRESS_HOST=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
export INGRESS_HOST

SECURE_INGRESS_PORT=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.name=="https")].port}')
export SECURE_INGRESS_PORT

curl -v -HHost:helloworld-v1.example.com \
--resolve helloworld-v1.example.com:"$SECURE_INGRESS_PORT":"$INGRESS_HOST" \
--cacert helloworld-v1.example.com/2_intermediate/certs/ca-chain.cert.pem \
https://helloworld-v1.example.com:"$SECURE_INGRESS_PORT"/hello`,
					},
				},
			}).

			// Configure a mutual TLS ingress gateway.
			// https://preliminary.istio.io/docs/tasks/traffic-management/ingress/secure-ingress-sds/#configure-a-mutual-tls-ingress-gateway
			Add(istioio.Command{
				Input: istioio.Inline{
					FileName: "rotate_httpbin_mtls_secret.sh",
					Value: `
echo "Delete the gateway’s secret and create a new one with client CA certificate"

kubectl -n istio-system delete secret httpbin-credential

kubectl create -n istio-system secret generic httpbin-credential  \
--from-file=key=httpbin.example.com/3_application/private/httpbin.example.com.key.pem \
--from-file=cert=httpbin.example.com/3_application/certs/httpbin.example.com.cert.pem \
--from-file=cacert=httpbin.example.com/2_intermediate/certs/ca-chain.cert.pem

kubectl get -n istio-system secret httpbin-credential`,
				},
			}, istioio.Command{
				Input: istioio.Inline{
					FileName: "create_httpbin_mtls_gateway.sh",
					Value: `
cat <<EOF | kubectl apply -f -
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
 name: mygateway
spec:
 selector:
   istio: ingressgateway # use istio default ingress gateway
 servers:
 - port:
     number: 443
     name: https
     protocol: HTTPS
   tls:
     mode: MUTUAL
     credentialName: "httpbin-credential" # must be the same as secret
   hosts:
   - "httpbin.example.com"
EOF`,
				},
			}, istioio.Command{
				Input: istioio.Inline{
					FileName: "check_envoy_sds_update_5.sh",
					Value: `
for it in {1..15}
do
  stats=$(kubectl -n istio-system exec "$(kubectl get pod -l istio=ingressgateway -n istio-system \
  -o jsonpath={.items..metadata.name})" -c istio-proxy -- \
  curl 127.0.0.1:15000/stats?filter=listener.0.0.0.0_443.server_ssl_socket_factory.ssl_context_update_by_sds)
  if [[ "$stats" == "listener.0.0.0.0_443.server_ssl_socket_factory.ssl_context_update_by_sds: 5" ]]; then
    echo "Envoy stats listener.0.0.0.0_443.server_ssl_socket_factory.ssl_context_update_by_sds meets expectation with ${it} retries"
    exit
  else
    sleep 2s
  fi
done

echo "ingress gateway SDS stats does not meet in 30 seconds. Expected 5 but got ${stats}" >&2`,
				},
			}).

			// Send an HTTPS request to access the httpbin service mTLS gateway.
			Add(istioio.Command{
				Input: istioio.IfMinikube{
					Then: istioio.Inline{
						FileName: "curl_httpbin_mtls_gateway_minikube.sh",
						Value: `
set -x

INGRESS_HOST=$(kubectl -n istio-system get pod -l istio=ingressgateway -o jsonpath='{.items[0].status.hostIP}')
export INGRESS_HOST

SECURE_INGRESS_PORT=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.port==443)].nodePort}')
export SECURE_INGRESS_PORT

curl -v -HHost:httpbin.example.com \
--resolve httpbin.example.com:"$SECURE_INGRESS_PORT":"$INGRESS_HOST" \
--cacert httpbin.example.com/2_intermediate/certs/ca-chain.cert.pem \
--cert httpbin.example.com/4_client/certs/httpbin.example.com.cert.pem \
--key httpbin.example.com/4_client/private/httpbin.example.com.key.pem \
https://httpbin.example.com:"$SECURE_INGRESS_PORT"/status/418`,
					},
					Else: istioio.Inline{
						FileName: "curl_httpbin_mtls_gateway_gke.sh",
						Value: `
set -x

INGRESS_HOST=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
export INGRESS_HOST

SECURE_INGRESS_PORT=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.name=="https")].port}')
export SECURE_INGRESS_PORT

curl -v -HHost:httpbin.example.com \
--resolve httpbin.example.com:"$SECURE_INGRESS_PORT":"$INGRESS_HOST" \
--cacert httpbin.example.com/2_intermediate/certs/ca-chain.cert.pem \
--cert httpbin.example.com/4_client/certs/httpbin.example.com.cert.pem \
--key httpbin.example.com/4_client/private/httpbin.example.com.key.pem \
https://httpbin.example.com:"$SECURE_INGRESS_PORT"/status/418`,
					},
				},
			}).

			// Cleanup
			Defer(istioio.Command{
				Input: istioio.Inline{
					FileName: "check_envoy_sds_update_5.sh",
					Value: `
rm -rf mtls-go-example
rm -rf httpbin.example.com
rm -rf httpbin.new.example.com
rm -rf helloworld-v1.example.com`,
				},
			}).Build())
}

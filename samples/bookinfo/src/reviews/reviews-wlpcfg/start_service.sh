
/opt/istio/prepare_proxy.sh -p 15001 -u 1337
/opt/ibm/wlp/bin/server run defaultServer &
su istio-proxy -c "/opt/istio/pilot-agent proxy -v 2 --serviceregistry Consul --serviceCluster reviews-${SERVICE_VERSION} --zipkinAddress zipkin:9411 > /tmp/envoy.log"

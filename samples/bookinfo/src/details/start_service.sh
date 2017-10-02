
/opt/istio/prepare_proxy.sh -p 15001 -u 1337
ruby /opt/microservices/details.rb 9080 &
su istio-proxy -c "/opt/istio/pilot-agent proxy -v 2 --serviceregistry Consul --serviceCluster details-v1 --zipkinAddress zipkin:9411  > /tmp/envoy.log"


/opt/istio/prepare_proxy.sh -p 15001 -u 1337
ruby /opt/microservices/details.rb 9080 &
su istio-proxy -c "/opt/istio/pilot-agent proxy -v 2 --serviceregistry Consul > /tmp/envoy.log"

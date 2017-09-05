
/opt/istio/prepare_proxy.sh -p 15001 -u 1337
/opt/ibm/wlp/bin/server run defaultServer &
su istio -c "/opt/istio/pilot-agent proxy -v 2 --serviceregistry Consul > /tmp/envoy.log"

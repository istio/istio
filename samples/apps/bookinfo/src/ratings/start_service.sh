
/opt/istio/prepare_proxy.sh -p 15001 -u 1337
node /opt/microservices/ratings.js 9080 &
su istio -c "/opt/istio/pilot-agent proxy -v 2 --serviceregistry Consul > /tmp/envoy.log"

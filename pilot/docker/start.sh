#!/bin/sh
echo "starting proxy_debug"
chmod +777 /dev/stdout

if [ -z $INBOUND_PORTS ]; then
    /usr/local/bin/istio-iptables.sh -p 15001 -u 1337
else
    /usr/local/bin/istio-iptables.sh -p 15001 -u 1337 -m REDIRECT -i '*' -b "$INBOUND_PORTS"
fi

iptables -t nat -I ISTIO_OUTPUT -d $HOST/32 -p tcp -m tcp --dport 5051 -j RETURN
if [ -z $DISCOVERY_ADDRESSS ]; then
    echo "DISCOVERY_ADDRESSS cannot be empty"
    exit 1
fi

su istio-proxy -c "/usr/local/bin/pilot-agent proxy --discoveryAddress $DISCOVERY_ADDRESSS  --domain $SERVICES_DOMAIN --serviceregistry Mesos --serviceCluster $SERVICE_NAME --zipkinAddress $ZIPKIN_ADDRESS --configPath /var/lib/istio"
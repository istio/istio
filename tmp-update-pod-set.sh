
#!/bin/bash

set -x

function add_ip_set() {
    local node_name="$1"
    PODS="$(kubectl get pods --field-selector spec.nodeName==$node_name -lapp=sleep -o custom-columns=:.status.podIP --no-headers) $(kubectl get pods --field-selector spec.nodeName==$node_name -lapp=helloworld -o custom-columns=:.status.podIP --no-headers)"
    docker exec -i "$node_name" sh <<EOF
    ipset create tmp-uproxy-pods-ips hash:ip
    ipset flush tmp-uproxy-pods-ips
EOF

    for pip in $PODS; do
        docker exec -i "$node_name" ipset add tmp-uproxy-pods-ips $pip
    done

    docker exec -i "$node_name" sh <<EOF
    ipset swap tmp-uproxy-pods-ips uproxy-pods-ips
    ipset destroy tmp-uproxy-pods-ips
EOF

}


while true; do
    for nodeName in $(kubectl get nodes -o custom-columns=:.metadata.name --no-headers | grep worker); do
        add_ip_set $nodeName
    done
    sleep 1
done
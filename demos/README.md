# Istio Demos

This directory contains demo applications highlighting Istio features 
such as ACLs, rate limiting, routing, etc.

The milestone 1 demo, although still a work in progress, does work and can be found [here](bookinfo/doc/bookinfo.md).

**Optional - Create a k8s namespace and set your current context to use that namespace**

    kubectl create ns <ns>
    
    kubectl config set-context `kubectl config view | grep current-context | awk '{print $2}'` --namespace <ns>

**Deploy istio infra**

    kubectl apply -f ./istio

This will deploy istio discovery service and istio mixer.

**Deploy a simple echo app with manually injected proxy**

    kubectl apply -f ./apps/simple_echo_app

This will deploy two pods, each running a simple echo server and client, and will create two kubernetes services called "echo" and "logic".

**Send some traffic**

Note the pod corresponding to the apps "echo" and "logic":
    
    kubectl get pods


Send HTTP request from "echo" pod to "logic" service:

    kubectl exec <echo-pod> -c app /bin/client http://logic/<some-text> -- --count 10
    
Send HTTP request from "logic" pod to "echo" service:

    kubectl exec -it <logic-pod> -c app /bin/client http://echo/<some-text> -- --count 10

This will echo the URL and print HTTP headers, including "X-Envoy-Expected-Rq-Timeout-Ms".

**Enable rate limiting in mixer**

    kubectl replace -f ./mixer-config-quota.yaml
    
**Optional - Monitoring with Prometheus and Grafana**

    kubectl apply -f ./prometheus.yaml  

    kubectl apply -f ./grafana.yaml   

Grafana custom image contains a build-in Istio-dashboard that you can access from:
    
    http://<grafana-svc-external-IP>:3000/dashboard/db/istio-dashboard

The example templates contain services configured as type LoadBalancer. If services are deployed with type NodePort,
kubectl proxy must be started, and the istio-dashboard in grafana must be edited to use the proxy. Grafana can be 
accessed via the proxy from:

    http://127.0.0.1:8001/api/v1/proxy/namespaces/<ns>/services/grafana:3000/dashboard/db/istio-dashboard
    
**Optional - Service Graph**

    kubectl apply -f ./servicegraph.yaml

View the graph json data and image at:

    http://<servicegraph-svc-external-IP>:8088/graph?time_horizon=10m

    http://<servicegraph-svc-external-IP>:8088/dotgraph

    http://<servicegraph-svc-external-IP>:8088/dotviz
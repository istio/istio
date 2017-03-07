# Istio Kubernetes Demo


**Optional - Create a k8s namespace**

    kubectl create ns <ns>

**Deploy istio infra**

    kubectl apply -f ./istio -n <ns>

This will deploy istio discovery manager and istio mixer.

**Deploy a simple echo app with manually injected proxy**

    kubectl apply -f ./apps/simple_echo_app -n <ns>

This will deploy two pods, each running a simple echo server and client, and will create two kubernetes services called "echo" and "logic".

**Send some traffic**

Note the pod corresponding to the apps "echo" and "logic"
    
    kubectl get pods -n <ns>


Send HTTP request from "echo" pod to "logic" service

    kubectl exec <echo-pod> -c app -n <ns> /bin/client http://logic/<some-text> -- --count 10
    
Send HTTP request from "logic" pod to "echo" service

    kubectl exec -it <logic-pod> -c app -n demo /bin/client http://echo/<some-text> -- --count 10

This will echo the URL and print HTTP headers, including "X-Envoy-Expected-Rq-Timeout-Ms".

**Optional - Monitoring with Prometheus and Grafana**

    kubectl apply -f ./prometheus.yaml -n <ns>    

    kubectl apply -f ./grafana.yaml -n <ns>   

Grafana custom image contains a build-in Istio-dashboard that you can access it from:
    
    http://<grafana-svc-external-IP>:3000/dashboard/db/istio-dashboard

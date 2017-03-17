# Install Istio on an existing Kubernetes cluster


**Optional - Create a Kubernetes namespace and set the current context to use that namespace**

    kubectl create ns <ns>
    
    kubectl config set-context `kubectl config view | grep current-context | awk '{print $2}'` --namespace <ns>

**Install Istio core components**

    kubectl apply -f ./kubernetes/istio-install

This will install istio-manager and istio-mixer.

    
**Optional addons - Monitoring with Prometheus, Grafana and Service Graph**

    kubectl apply -f ./kubernetes/addons/


Grafana custom image contains a build-in Istio-dashboard that you can access from:
    
    http://<grafana-svc-external-IP>:3000/dashboard/db/istio-dashboard

    
View the microservices graph image with service graph at:

    http://<servicegraph-svc-external-IP>:8088/dotviz

*The addons yaml files contain services configured as type LoadBalancer. If services are deployed with type NodePort,
start kubectl proxy, and edit Grafana's Istio-dashboard to use the proxy. Access Grafana via kubectl proxy:*

    http://127.0.0.1:8001/api/v1/proxy/namespaces/<ns>/services/grafana:3000/dashboard/db/istio-dashboard

        
**Deploy your apps**

Deploy your apps, or try one of the example apps from demos directory. Each app directory contains an associated README.md providing more details.
# Install Istio on an existing Kubernetes cluster


**Optional - Create a Kubernetes namespace and set the current context to use that namespace**

    kubectl create ns <ns>
    
    kubectl config set-context `kubectl config view | grep current-context | awk '{print $2}'` --namespace <ns>

**Install Istio infra**

    kubectl apply -f ./kubernetes/istio-install

This will install istio-manager and istio mixer.

    
**Optional addons - Monitoring with Prometheus, Grafana and Service Graph**

    kubectl apply -f ./kubernetes/addons/


Grafana custom image contains a build-in Istio-dashboard that you can access from:
    
    http://<grafana-svc-external-IP>:3000/dashboard/db/istio-dashboard

    
View the microservices graph image with service graph at:

    http://<servicegraph-svc-external-IP>:8088/dotviz

*The example templates contain services configured as type LoadBalancer. If services are deployed with type NodePort,
kubectl proxy must be started, and the istio-dashboard in grafana must be edited to use the proxy. Grafana can be 
accessed via the proxy from:*

    http://127.0.0.1:8001/api/v1/proxy/namespaces/<ns>/services/grafana:3000/dashboard/db/istio-dashboard

        
**Deploy your apps**

You can start deploy your apps, or try one of the examples apps from demos directory. Please read the associated README.md in each demo directory.


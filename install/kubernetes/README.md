# Install Istio on an existing Kubernetes cluster

<!---
**Optional - Create a Kubernetes namespace and set the current context to use that namespace**

    kubectl create ns <ns>

    kubectl config set-context `kubectl config view | grep current-context | awk '{print $2}'` --namespace <ns>
--->

## Install Istio core services

    kubectl apply -f ./kubernetes/istio-install

This will install istio-manager, istio-mixer, and the istio ingress controller.


## Optional addons - Monitoring with Prometheus, Grafana and Service Graph

    kubectl apply -f ./kubernetes/addons/


Grafana custom image contains a build-in Istio-dashboard that you can access from:

    http://<grafana-svc-external-IP>:3000/dashboard/db/istio-dashboard


View the microservices graph image with service graph at:

    http://<servicegraph-svc-external-IP>:8088/dotviz

*The addons yaml files contain services configured as type LoadBalancer. If services are deployed with type NodePort,
start kubectl proxy, and edit Grafana's Istio-dashboard to use the proxy. Access Grafana via kubectl proxy:*

    http://127.0.0.1:8001/api/v1/proxy/namespaces/<ns>/services/grafana:3000/dashboard/db/istio-dashboard


## Deploy your apps

NOTE: Kubernetes admission controller for transparent proxy is not
implemented yet . Use `istioctl kube-inject` to modify kubernetes
resources files client-side *before* submitting them to the kubernetes
API server. `istioctl kube-inject` is documented
[here](https://github.com/istio/istio.github.io/blob/master/docs/reference/istioctl.md#kube-inject).

    kubectl apply -f <(istioctl kube-inject -f <resource.yaml>)

Deploy your apps, or try one of the example apps from demos directory. Each app directory contains an associated README.md providing more details.


## Uninstalling Istio core services

To uninstall the Istio control plane, run the following command:

    kubectl delete -f ./kubernetes/istio-install

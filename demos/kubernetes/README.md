# Istio Kubernetes Demos

Create a k8s namespace

kubectl create ns demo

Deploy istio infra

kubectl apply -f istio -n demo

Deploy simple app

kubectl apply -f apps/simple_app/ -n demo

Send a HTTP request from a to b

kubectl get pods -n demo

Note the pod corresponding to app "a"

kubectl exec <a-pod> -c app -n demo /bin/client http://b/someurl

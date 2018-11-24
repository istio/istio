# Operator

Operator is the top-level installation framework for creating and managing a
Istio deployment. It is responsible for converting any labeled configmaps into
CRDs in a Kubernetes environment. It contains a Kubernetes configmap listener
for creating CRDs for managed by Galley and Kubernetes.

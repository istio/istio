KUBECONFIG=${KUBECONFIG:/config}

# This runs inside KIND node

kubectl apply -f /project/crds.yaml

# Helm is not available in the KIND node

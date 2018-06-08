# !/bin/bash

# File path to local registry for e2e tests
LOCALREG_FILE=${ISTIO}/istio/tests/e2e/local/localregistry/localregistry.yaml

echo "Deploying local registry onto cluster..."
kubectl apply -f ${LOCALREG_FILE}

count=0
echo "Waiting for registry proxy to be active..."
until $(kubectl -n kube-system get pods | grep -q '^kube-registry-proxy.*1/1.*$'); do
    printf '.'
    sleep 1
    let count+=1
    if [[ "$count" -gt 10 ]]; then
        echo "Time limit reached. Fail to bring local registry proxy up."
        exit 1
    fi
done
echo "Local registry proxy is up"

count=0
echo "Waiting for registry to be active..."
until $(kubectl -n kube-system get pods | grep -q '^kube-registry-v0.*1/1.*$'); do
    printf '.'
    sleep 1
    let count+=1
    if [[ "$count" -gt 10 ]]; then
    	echo "Time limit reached. Fail to bring local registry up."
    	exit 1
    fi
done
echo "Local registry is up"

# Set up docker daemon for linux
case "$OSTYPE" in
  darwin*)  
	;;
  linux*)   echo "sudo sed -i 's/ExecStart=\/usr\/bin\/dockerd -H fd:\/\//ExecStart=\/usr\/bin\/dockerd -H fd:\/\/ --insecure-registry localhost:5000/' /lib/systemd/system/docker.service"
			sudo sed -i 's/ExecStart=\/usr\/bin\/dockerd -H fd:\/\//ExecStart=\/usr\/bin\/dockerd -H fd:\/\/ --insecure-registry localhost:5000/' /lib/systemd/system/docker.service
			sudo systemctl daemon-reload
			sudo systemctl restart docker
	;;
  *)        echo "unsupported: $OSTYPE" 
	;;
esac

# Set up port forwarding
POD=`kubectl get po -n kube-system | grep kube-registry-v0 | awk '{print $1;}'`
kubectl port-forward --namespace kube-system $POD 5000:5000 &
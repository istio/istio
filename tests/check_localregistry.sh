# !/bin/bash

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

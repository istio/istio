# Using an in-cluster Private Registry

`localregistry.yaml` is a copy of Kubernete's local registry addon, and is included to make it easier to test
Istio by allowing a developer to push docker images locally rather than to some remote registry.

### Run the registry
To run the local registry in your kubernetes cluster:

```shell
$ kubectl apply -f ./tests/util/localregistry/localregistry.yaml
```

### Expose the Registry

After the registry pod is running, expose it locally by executing:

```shell
$ POD=$(kubectl get pods --namespace kube-system -l k8s-app=kube-registry \
  -o template --template '{{range .items}}{{.metadata.name}} {{.status.phase}}{{"\n"}}{{end}}' \
  | grep Running | head -1 | cut -f1 -d' ')
$ kubectl port-forward --namespace kube-system $POD 5000:5000 &
```

### Enable insecure registry on host
**On macOS**
Add `docker.for.mac.localhost:5000` to insecure registries in docker daemon, then build and push images to the registry

**On Linux**
Run the following commands to finish the setup:
```shell
echo "sudo sed -i 's/ExecStart=\/usr\/bin\/dockerd -H fd:\/\//ExecStart=\/usr\/bin\/dockerd -H fd:\/\/ --insecure-registry localhost:5000/' /lib/systemd/system/docker.service"
sudo sed -i 's/ExecStart=\/usr\/bin\/dockerd -H fd:\/\//ExecStart=\/usr\/bin\/dockerd -H fd:\/\/ --insecure-registry localhost:5000/' /lib/systemd/system/docker.service
sudo systemctl daemon-reload
sudo systemctl restart docker
```

### Enable insecure registry in your cluster
GKE and minikube enable this by default.
For Bluemix Kubernetes clusters, please follow the [instructions](setup_bluemix.md) to modify the docker setting. 

### Push Local Images

Build and push the Istio docker images to the local registry, by running:
For value of <local hub>, use `localhost` for linux and `docker.for.mac.localhost` for macOS.

```shell
$ GOOS=linux make build docker.push HUB=<local hub>:5000 TAG=latest
```

You should be able to see all images in the registry with the following command:
```shell
$ curl localhost:5000/v2/_catalog
```

### Run with Local Images

#### Get service ip for registry
```shell
$ REG_SVC_IP=$(kubectl get service kube-registry -n kube-system -o jsonpath='{.spec.clusterIP}')
```

#### Running E2E Tests
Now you can run e2e tests. For example, to run the e2e simple test:
```shell
$ make e2e_simple HUB=$REG_SVC_IP:5000 TAG=latest
```

For more details on how to run e2e test, please refer to [link](../../README.md)

#### Hard-coding the Image URL

You can also modify the image URLs in your deployment yaml files directly:

```yaml
image: <REGISTRY_SVC_IP>:5000/<APP_NAME>:latest
```


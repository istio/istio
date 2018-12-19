# Using an in-cluster Docker Registry

`localregistry.yaml` is a copy of Kubernete's local registry addon, and is included to make it easier to test
Istio by allowing a developer to push docker images locally rather than to some remote registry.

### Run the registry
To run the local registry in your kubernetes cluster:

```shell
$ kubectl apply -f ./tests/util/localregistry/localregistry.yaml
```

### Expose the Registry

After the registry server is running, expose it locally by executing:

```shell
$ kubectl port-forward --namespace docker-registry $POD 5000:5000
```

If you're testing locally with minikube, `$POD` can be set with:

```shell
$ POD=$(kubectl get pods --namespace docker-registry -l k8s-app=kube-registry \
  -o template --template '{{range .items}}{{.metadata.name}} {{.status.phase}}{{"\n"}}{{end}}' \
  | grep Running | head -1 | cut -f1 -d' ')
```

### Push Local Images

Note that on GKE nodes, localhost does not seem to resolve to 127.0.0.1, so you
should use 127.0.0.1 instead of localhost in all the following shell calls.
Build and push the Istio docker images to the local registry, by running:

```shell
$ make docker push HUB=localhost:5000 TAG=latest
```

### Run with Local Images

#### Running E2E Tests
If you're running e2e tests, you can set the test flags:

```shell
$ go test <TEST_PATH> -hub=localhost:5000 -tag=latest
```

#### Hard-coding the Image URL

You can also modify the image URLs in your deployment yaml files directly:

```yaml
image: localhost:5000/<APP_NAME>:latest
```


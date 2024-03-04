The calico.yaml file is from [https://raw.githubusercontent.com/projectcalico/calico/v3.27.0/manifests/calico.yaml](https://raw.githubusercontent.com/projectcalico/calico/v3.27.0/manifests/calico.yaml)

Once downloaded, run the following sed command to replace the default docker.io images with istio-testing's copies of them:

```shell
sed -ie "s?docker.io?gcr.io/istio-testing?g" calico.yaml
```

In order to upgrade versions of calico we'll need to update the version below and then have someone with the ability to push run the following:

```shell
export VERSION=v3.27.0

crane cp {docker.io,gcr.io/istio-testing}/calico/cni:"${VERSION}"
crane cp {docker.io,gcr.io/istio-testing}/calico/node:"${VERSION}"
crane cp {docker.io,gcr.io/istio-testing}/calico/kube-controllers:"${VERSION}"
```

# Run E2E tests on Bluemix

This setup helps you run e2e tests with your local code against a k8s cluster on BLuemix.

* [Step 0: Install and configure IBM Cloud Kubernetes Service CLI](#step-0-install-and-configure-ibm-cloud-kubernetes-service-cli)
* [Step 1: Create and configure your kubernetes cluster](#step-1-create-and-configure-your-kubernetes-cluster)
* [Step 2: Set up Registry](#step-2-set-up-registry)
* [Step 3: Run the tests](#step-3-run-the-tests)

## Step 0: Install and configure IBM Cloud Kubernetes Service CLI

### 1. Download and Install IBM Cloud CLI:

*macOS*:
```shell
curl -fsSL https://clis.ng.bluemix.net/install/osx | sh
```

*Linux*:
```shell
curl -fsSL https://clis.ng.bluemix.net/install/linux | sh
```

See [full instructions](https://console.bluemix.net/docs/cli/reference/bluemix_cli/download_cli.html#download_install) for more details.

### 2. Install Kubernetes Container Service Plugin:

Login to your IBM CLoud Account:

```shell
bx login
```

```shell
bx plugin install container-service -r Bluemix
```

And verify it using:
```shell
bx plugin list
``` 

You should see container-service in the result.

## Step 1: Create and configure your kubernetes cluster

### Option 1: Create a cluster using the CLI

```shell
bx cs cluster-create \
	--location dal10 \
	--machine-type b2c.4x16 \
	--hardware <shared_or_dedicated> \
	--public-vlan <public_VLAN_ID> \
	--private-vlan <private_VLAN_ID> \
	--workers 3 \
	 --name <cluster_name> \
	--kube-version <major.minor.patch> \
	[--disable-disk-encrypt] \
	[--trusted]
```

For details of all components, please refer to the [doc](https://console.bluemix.net/docs/containers/cs_clusters.html#clusters_cli)

To create a free one-node cluster(with 2 CPUs and 4GB memory, deleted after 30 days) for trial, you can simply run
```shell
bx cs cluster-create --name <cluster-name>
```

### Option 2: Create a cluster using the GUI
You can go to [link](https://console.bluemix.net/containers-kubernetes/catalog/cluster/create) to follow steps to create a cluster.


## Step 2: Set up the registry

### Use remote registry
Set the HUB and TAG variables to point to your prebuilt images on a remote registry, e.g.
```shell
export HUB="kimiko"
export TAG="latest"
```

## Step 3: Run the tests

Go to `$ISTIO/istio`,

Run all e2e tests skipping cleanup process(for debug):
```shell
make e2e_all E2E_ARGS="--skip_cleanup"
```

Run one e2e test only, e.g. e2e_simple:
```shell
make e2e_simple
```

Please refer to Step 5 [here](UsingGKE.md#step-5-run) for more details about test arguments.

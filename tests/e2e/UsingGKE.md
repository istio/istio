# Running E2E tests on your own kubernetes cluster

* [Step 1: Create a kubernetes cluster](#step-1-create-and-setup-a-kubernetes-cluster)
* [Step 2: Get cluster credentials](#step-2-get-cluster-credentials)
* [Step 3: Create Clusterrolebinding](#step-3-create-clusterrolebinding)
* [Step 4: Export test script variables](#step-4-export-test-script-variables)
* [Step 5: Run](#step-5-run)
* [Examples](#examples)

## Step 1: Create and setup a kubernetes cluster
E2E tests require a Kubernetes cluster. You can create one using the Google Container Engine using
the following command:

```bash
gcloud container clusters \
  create ${CLUSTER_NAME} \
  --zone ${ZONE} \
  --project ${PROJECT_NAME} \
  --cluster-version ${CLUSTER_VERSION} \
  --machine-type ${MACHINE_TYPE} \
  --num-nodes ${NUM_NODES} \
  --enable-kubernetes-alpha \
  --no-enable-legacy-authorization
 ```

 - `CLUSTER_NAME`: Whatever suits your fancy, 'istio-e2e' is a good choice.
 - `ZONE`: 'us-central1-f' is a good value to use.
 - `PROJECT_NAME`: is the name of the GCP project that will house the cluster. You get a project by visiting [GCP](https://console.cloud.google.com).
 - `CLUSTER_VERSION`: 1.7.3 or later.
 - `MACHINE_TYPE`: Use 'n1-standard-4'
 - `NUM_NODES`: Use 3.
 - `no-enable-legacy-authorization`: Optional, needed if you want to test RBAC.

You must set your default compute service account to include:
roles/container.admin (Kubernetes Engine Admin)
Editor (on by default)

To set this, navigate to the IAM section of the Cloud Console and find your default GCE/GKE service account in the
following form: projectNumber-compute@developer.gserviceaccount.com: by default it should just have the Editor role.
Then in the Roles drop-down list for that account, find the Kubernetes Engine group and select the role Kubernetes
Engine Admin. The Roles listing for your account will change to Multiple.

## Step 2: Get cluster credentials
```
gcloud container clusters get-credentials ${CLUSTER_NAME} --zone ${ZONE} --project ${PROJECT_NAME}
```

## Step 3: Create Clusterrolebinding
```
kubectl create clusterrolebinding myname-cluster-admin-binding  --clusterrole=cluster-admin  --user=$(gcloud config get-value core/account)
```
* user_email should be the one you use to log in gcloud command. You can do `gcloud info` to find out current user info.


## Step 4: Export test script variables

**Option 1:** Build your own images.


```
# Customize .istiorc.mk (at the top of the istio.io/istio source tree) with your HUB and optional TAG
# it allows you to customize Makefile rules. For example:
cat .istiorc.mk
HUB=costinm
TAG=mybranch
GS_BUCKET=mybucket

# Build images on the local docker.
make docker

# Push images to docker registry
make push

# the hub/tag set in your .istiorc.mk will be used by the test.

```

On MacOS, you need to set the target operating system before building the images

```
GOOS=linux make docker push
```

**Option 2:** Already committed changes to istio/istio master branch
NOTE: SHA used as TAG is one that is already committed on istio/istio. You can pick any SHA you want.
```
export HUB="gcr.io/istio-testing"
export TAG="d0142e1afe41c18917018e2fa85ab37254f7e0ca"
```

**Option 3:** Testing local changes

If you want to test on uncommitted changes to master istio:
* Create a PR with your change.
* This will trigger istio-presubmit.sh. At the end of this script, it creates docker images for mixer, pilot, ca, with
your changes and upload them to container registry. See the logs of this istio-presubmit.sh and at the end there must
be a SHA which you need to copy and set it as a GIT_SHA. Example from a log: the SHA to copy is marked in **bold**

	I1207 04:42:40.881] **0077bb73e0b9d2841f8c299f15305193e42dae0d**: digest: sha256:6f72528d475be56e8392bc3b833b94a815a1fbab8a70cd058b92982e61364021 size: 528

* Then set the export variables again
```
export HUB="gcr.io/istio-testing"
export TAG="<sha copied from the logs of istio-presubmit.sh>"
```

## Step 5: Run

From the repo checkout root directory

```bash
make e2e_all E2E_ARGS="--skip_cleanup"
```

Each test has its own directory and can be run independently as a
go_test target. The script has a number of options available [here](README.md#options-for-e2e-tests)

## Examples

* Running on Mac:

  Although istioctl-osx currently is not built during presubmit/postsubmit, only in release process. You can build your own istioctl from source or download a release version from [release page](https://github.com/istio/istio/releases) (although it's not the latest one), and then use `--istioctl` instead of `--istioctl_url` to specify the local path.

  ```bash
  make e2e_all E2E_ARGS="--skip_cleanup"
  ```

* Running single test file (bookinfo, mixer, simple) and also skip cleanup.

  ```bash
  make e2e_bookinfo E2E_ARGS="--skip_cleanup"
  ```

Please see golang testing options for more information.

  ```bash
  go test --help
  ```

* For **simple test** specific, you can run test multiple time against the same environment setup by `skip_setup`:
  ```bash
  # First time you want to run: deploy in namespace e2e and leave it running:
  make e2e_simple E2E_ARGS="--skip_cleanup --namespace=e2e -istioctl ~/istioctl-osx --auth_enable"
  # Subsequent runs if only the TestSimpleIngress (for instance) changes:
  make e2e_simple E2E_ARGS="--skip_setup --skip_cleanup --namespace=e2e -istioctl ~/istioctl-osx --auth_enable --test.run TestSimpleIngress"
  ```


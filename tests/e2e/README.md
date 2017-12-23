# End-to-End Testing

This directory contains Istio end-to-end tests and associated test framework.

# Running E2E tests on your own kubernets cluster

NOTE: the e2e tests might not run on a Mac because istioctl-osx, needed for test execution, is only built for release
builds and not for normal presubmits jobs, see [examples](#examples) for solutions.

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
 - `ZONE`: 'us-central-f' is a good value to use.
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
kubectl create clusterrolebinding myname-cluster-admin-binding    --clusterrole=cluster-admin    --user="<user_email>"
```
* user_email should be the one you use to log in gcloud command. You can do `gcloud info` to find out current user info.


## Step 4: Export test script variables

**Option 1:** Build your own images.

```
# Customize .istiorc with your HUB and optional TAG (example: HUB=costinm TAG=mybranch)

# Build images on the local docker. 
make docker

# Push images to docker registry
# If you use minikube and its docker environment, images will be  available in minikube for use, 
# you can skip this step.
make push

# the hub/tag set in your .istiorc will be used by the test.

```

**Option 2:** Already committed changes to istio/istio master branch
NOTE: SHA used for GIT_SHA is one that is already committed on istio/istio. You can pick any SHA you want.
```
export HUB="gcr.io/istio-testing"
export GIT_SHA="d0142e1afe41c18917018e2fa85ab37254f7e0ca"
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
export GIT_SHA="<sha copied from the logs of istio-presubmit.sh>"
```

## Step 5: Run

From the repo checkout root directory

```bash
make e2e E2E_ARGS="--skip_cleanup"
```

Tests are driven by the [e2e.sh](../e2e.sh) script. Each test has its own directory and can be run independently as a
go_test target. The script has a number of options:

* `--skip_cleanup` - to skip cleanup steps
* `--namespace <namespace>` : If you don't specify `namespace`, a random namespace is generated for each test.
* `--verbose <debug level noise from proxies>`
* `--istioctl <local istioctl path>`: Use local istioctl binary. 
* `--istioctl_url <remote istioctl url>`: If local path is not defined, download istioctl from a remote location. 
* `--use_local_cluster`
* `--auth_enable` - if you want to include auth
* `--cluster_wide` - if you want to run the cluster wide installation and tests
* `--use_initializer` - if you want to do transparent sidecar injection
* `--mixer_hub <mixer image hub>`
* `--mixer_tag <mixer image tag>`
* `--pilot_hub <pilot image hub>`
* `--pilot_tag <pilot image tag>`
* `--ca_hub <CA image hub>`
* `--ca_tag <CA image tag>`

## Examples

* Running on Mac:

  Although istioctl-osx currently is not built during presubmit/postsubmit, only in release process. You can build your own istioctl from source or download a release version from [release page](https://github.com/istio/istio/releases) (although it's not the latest one), and then use `--istioctl` instead of `--istioctl_url` to specify the local path.

  ```bash
  ./tests/e2e.sh \
  --mixer_tag "${GIT_SHA}"  --mixer_hub "${HUB}" \
  --pilot_tag "${GIT_SHA}"  --pilot_hub "${HUB}" \
  --ca_tag "${GIT_SHA}"  --ca_hub "${HUB}" \
  --istioctl <path to local istioctl> \
  --skip_cleanup
  ```

* Running single test file (bookinfo, mixer, simple) and also skip cleanup. Add `-s` flag (parsed by e2e.sh).
  ```bash
  ./tests/e2e.sh -s bookinfo \
  --mixer_tag "${GIT_SHA}" --mixer_hub "${HUB}" \
  --pilot_tag "${GIT_SHA}" --pilot_hub "${HUB}" \
  --ca_tag "${GIT_SHA}" --ca_hub "${HUB}" \
  --istioctl_url "https://storage.googleapis.com/istio-artifacts/pilot/${GIT_SHA}/artifacts/istioctl" \
  --skip_cleanup
  ```

* Running one specific test case (TestSimpleIngress, TestGlobalCheckAndReport) in a test file:

  First build that go test target:
  ```bash
  bazel build //tests/e2e/tests/simple:go_default_test
  ```

  Then you can run the go_test binary and specific a test case use flag `--test.run`:
  ```bash
  ./bazel-bin/tests/e2e/tests/simple/go_default_test -alsologtostderr -test.v -v 2 \
  --test.run TestSimpleIngress \
  --mixer_tag "${GIT_SHA}" --mixer_hub "${HUB}" \
  --pilot_tag "${GIT_SHA}" --pilot_hub "${HUB}" \
  --ca_tag "${GIT_SHA}"  --ca_hub "${HUB}"  \
  --istioctl_url "https://storage.googleapis.com/istio-artifacts/pilot/${GIT_SHA}/artifacts/istioctl" \
  --auth_enable --skip_cleanup 
  ```

* For **simple test** specific, you can run test multiple time against the same environement setup by `skip_setup`:
  ```bash
  # First time you want to run: deploy in namespace e2e and leave it running:
  ./bazel-bin/tests/e2e/tests/simple/go_default_test -alsologtostderr -test.v -v 2  --skip_cleanup --namespace=e2e -istioctl ~/istioctl-osx --auth_enable
  # Subsequent runs if only the TestSimpleIngress (for instance) changes:
  ./bazel-bin/tests/e2e/tests/simple/go_default_test -alsologtostderr -test.v -v 2  --skip_setup --skip_cleanup --namespace=e2e -istioctl ~/istioctl-osx --auth_enable --test.run TestSimpleIngress
  ```
  

# demo_test.go

[demo_test.go](tests/bookinfo/demo_test.go) is a sample test.
It's based on the shell script version of demo test. It has four test cases: default routing, version routing, fault
delay and version migration. Each test case applies specific rules for itself and clean them up after finishing.

You can build and run this or any single test manually with the same options as e2e.sh when testing specific version of master, mixer or istioctl

# Writing tests

Follow the sample of demo_test.go
1. Create a new commonConfig for framework and add app used for this test in setTestConfig().
   Each test file has a `testConfig` handling framework and test configuration.
   `testConfig` is a cleanable structure which has  `Setup` and `Teardown`. `Setup` will run before all tests and `Teardown`
   is going to clean up after all tests.
2. Framework would handle all setting up: install and setup istio, deploy app.
3. Setup test-specific environment, like generate rule files from templates and apply routing rules.
   These could be done in `testConfig.Setup()` and would be executed by cleanup register right after framework setup.
4. Write a test. Test case name should start with 'Test' and using 't *testing.T' to log test failures.
   There is no guarantee for running order
4. Each test file is supposed to have a `BUILD` and is built as a go_test target in its own director. Target must have
   `tags =  ["manual"]`, since tests cannot ran by `bazel test` who runs tests in sandbox where you can't connect to cluster.

# Cluster in same local network

In order to talk to istio ingress, we use the ingress IP by default. If your
cluster is on the same local network and cannot provide external IP (for example, minikube), use the `--use-local-cluster` flag.
In that case, the framework will not create a LoadBalancer and talk directly to the Pod running istio-ingress.

# Debugging

The requests from the containers deployed in tests are performed by `client` program.
For example, to perform a call from a deployed test container to https://console.bluemix.net/, run:

```bash
kubectl exec -it <test pod> -n <test namespace> -c app -- client -url https://console.bluemix.net/
```

To see its usage run:

```
kubectl exec -it <test pod> -n <test namespace> -c app -- client -h
```

## Test framework notes

The E2E test framework defines and creates structures and processes for creating cleanable test environments:
install and setup istio modules and clean up afterward.

Writing new tests doesn't require knowledge of the framework.

- framework.go: `Cleanable` is a interface defined with setup() and teardown(). While initialization, framework calls setup() from all registered cleanable
structures and calls teardown() while framework cleanup. The cleanable register works like a stack, first setup, last teardown.

- kubernetes.go: `KubeInfo` handles interactions between tests and kubectl, installs istioctl and apply istio module. Module yaml files are in store at
[install/kubernetes/templates](../../install/kubernetes/templates) and will finally use all-in-one yaml [istio.yaml](../../install/kubernetes/istio.yaml)

- appManager.go: gather apps required for test into a array and deploy them while setup()

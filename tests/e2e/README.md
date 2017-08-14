# e2e Testing

This directory contains Istio end-to-end tests and test framework.


## e2e.sh

Each test has its own directory and would be built as a go_test target.
Tests could be built and drove manually as a single test or automatically detected and ran by [e2e.sh](../e2e.sh)

### Options
* `--namespace` specify a namespace for test
* `--mixer_hub` mixer iamge hub
* `--mixer_tag` mixer iamge tag
* `--pilot_hub` pilot iamge hub
* `--pilot_tag` pilot iamge tag
* `--ca_hub` app image hub
* `--ca_tag` app image tag
* `--verbose` debug level noise from proxies
* `--istioctl_url` the location of an `istioctl` binary
* `--skip_cleanup` if skip cleanup steps
* `--log_provider` where cluster logs are hosted, only support `stackdriver` for now
* `--project_id` project id used to filter logs from provider
* `--use_local_cluster` whether the cluster is local or not

Default values for the `mixer_hub/tag`, `pilot_hub/tag`, and `istioctl_url` are as specified in
[istio.VERSION](../../istio.VERSION), which are latest tested stable version pairs.

istio.VERSION can be updated by [updateVersion.sh](../../updateVersion.sh).
Look at [Integration Test](https://github.com/istio/istio/tree/master/tests#updateversionsh) for more information.

If not specify `namespace`, a randomly namespace would be generated for each test.

`log_provider` and `project_id` must both be specified if one wishes to collect cluster logs.

### Example
From the repo checkout root directory

* `tests/e2e.sh`: run tests with the latest stable version of istio according to istio.VERSION.

* `tests/e2e.sh --pilot_hub=gcr.io/istio-testing --pilot_tag=dc738396fd21ab9779853635dd22693d9dd3f78a --istioctl_url=https://storage.googleapis.com/istio-artifacts/dc738396fd21ab9779853635dd22693d9dd3f78a/artifacts/istioctl`: test commit in pilot repo, SHA:"dc738396fd21ab9779853635dd22693d9dd3f78a".


## Access to logs and temp files from Jenkins

If tests ran in presubmit on Jenkins, you can easily access to logs and temp files. Go to your pr page on Jenkins console "https://testing.istio.io/job/istio/job/presubmit/<pr_number>", click "artifacts.html", and it will lead you to tests records.

## demo_test.go

[demo_test.go](tests/bookinfo/demo_test.go) is a sample test.
It's based on the shell script version of demo test [kubeTest.sh](../kubeTest.sh). It has four test cases: default routing, version routing, fault delay and version migration. Each test case applies specific rules for itself and clean them up after finishing.

You can build and run this or any single test manually with the same options as e2e.sh when testing specific version of master, mixer or istioctl


## Developer process

### Testing code change
1. Run `e2e.sh --pilot_hub <pilot hub> --pilot_tag <pilot tag> --istioctl_url <istioctl url>` or
   `e2e.sh --mixer_hub <mixer hub> --mixer_tag <mixer tag>` to test your changes to pilot, mixer, respectively.
2. Submit a PR with your changes to `istio/pilot` or `istio/mixer`.
3. Run `updateVersion.sh` to update the default Istio install configuration and then
   submit a PR  to `istio/istio` for the version change. (Only admin)

### Writing tests
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


## Framework

e2e framework defines and creates structure and processes for creating cleanable test environment: install and setup istio modules and clean up afterward.

Testing code or writing tests don't require knowledge of framework, it should be transparent for test writers

### framework.go
`Cleanable` is a interface defined with setup() and teardown(). While initialization, framework calls setup() from all registered cleanable structures and calls teardown() while framework cleanup. The cleanable register works like a stack, first setup, last teardown.

### kubernetes.go
`KubeInfo` handles interactions between tests and kubectl, installs istioctl and apply istio module. Module yaml files are in store at [install/kubernetes/templates](../../install/kubernetes/templates) and will finally use all-in-one yaml [istio.yaml](../../install/kubernetes/istio.yaml)

### appManager.go
`appManager` gather apps required for test into a array and deploy them while setup()




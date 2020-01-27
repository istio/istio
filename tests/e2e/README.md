# End-to-End Testing

This directory contains Istio end-to-end tests and associated test framework.

NOTE: These tests are considered deprecated. All new tests should try to use the new testing framework.
This allows tests to run locally as well as in Kubernetes and is more flexible. See `./tests/integration` or the [docs](https://github.com/istio/istio/wiki/Istio-Test-Framework).

E2E tests are meant to ensure functional correctness in an E2E environment to make sure Istio works with one or more
deployments. For now, these tests run with kind in Prow in both pre-submit and post-submit stages. Their results can be found in
<https://prow.istio.io/> and <https://k8s-testgrid.appspot.com/istio_istio_postsubmit>.

Developers, on the other hand, are recommended to run the tests locally before sending out any PR.

## Running E2E Tests

In CI, tests run using [kind](https://kind.sigs.k8s.io/). The same commands run in CI can be run locally, as the test scripts set up a
hermetic environment.

For example: `prow/e2e-kind-suite.sh --auth_enable --single_test e2e_simple`.

The full list of jobs can be found in [test-infra](https://github.com/istio/test-infra/blob/release-1.5/prow/config/jobs/istio.yaml).

## Debugging

The requests from the containers deployed in tests are performed by `client` program.
For example, to perform a call from a deployed test container to <https://console.bluemix.net/>, run:

```bash
kubectl exec -it <test pod> -n <test namespace> -c app -- client -url https://console.bluemix.net/
```

To see its usage run:

```bash
kubectl exec -it <test pod> -n <test namespace> -c app -- client -h
```

or check [the source code](https://github.com/istio/istio/blob/release-1.5/pkg/test/echo/cmd/client/main.go).

## Adding New E2E Tests

[demo_test.go](tests/bookinfo/demo_test.go) is a sample test that covers four cases: default routing, version routing, fault delay, and version migration.
Each case applies traffic rules and then clean up after the test. It can serve as a reference for building new test cases.

See below for guidelines for creating a new E2E test.

1. Create a new commonConfig for framework and add app used for this test in setTestConfig().
   Each test file has a `testConfig` handling framework and test configuration.
   `testConfig` is a cleanable structure which has  `Setup` and `Teardown`. `Setup` will run before all tests and `Teardown`
   is going to clean up after all tests.

1. Framework would handle all setting up: install and setup istio, deploy app.

1. Setup test-specific environment, like generate rule files from templates and apply routing rules.
   These could be done in `testConfig.Setup()` and would be executed by cleanup register right after framework setup.

1. Write a test. Test case name should start with 'Test' and using 't *testing.T' to log test failures.
   There is no guarantee for running order

## Test Framework Notes

The E2E test framework defines and creates structures and processes for creating cleanable test environments:
install and setup istio modules and clean up afterward.

Writing new tests doesn't require knowledge of the framework.

- framework.go: `Cleanable` is a interface defined with setup() and teardown(). While initialization, framework calls setup() from all registered cleanable
structures and calls teardown() while framework cleanup. The cleanable register works like a stack, first setup, last teardown.

- kubernetes.go: `KubeInfo` handles interactions between tests and kubectl, installs istioctl and apply istio module. Module yaml files are in Helm charts
[install/kubernetes/helm](../../install/kubernetes/helm) and will finally use all-in-one yaml [istio.yaml](../../install/kubernetes/istio.yaml)

- appManager.go: gather apps required for test into a array and deploy them while setup()

## Options For E2E Tests

E2E tests have multiple options available while running them as follows:

- `--skip_setup` - Skip namespace creation and istio cluster setup (default: false)
- `--skip_cleanup` - Skip the cleanup steps (default: false)
- `--namespace <namespace>` - If you don't specify `namespace`, a random namespace is generated for each test.
- `--use_local_cluster` - If true any LoadBalancer type services will be converted to a NodePort service during testing. If running on minikube, this should be set to true. (default: false)
- `--auth_enable` - If you want to include auth (default: false)
- `--rbac_enabled` - Enable RBAC (default: true)
- `--cluster_wide` - If true Pilot/Mixer will observe all namespaces rather than just the testing namespace (default: false)
- `--use_automatic_injection` - if you want to do transparent sidecar injection  (default: false)
- `--use_galley_config_validator` - if you want to enable automatic configuration validation (default: false)
- `--mixer_hub <hub>` - Image hub for the Mixer (default: environment $HUB)
- `--mixer_tag <tag>` - Image tag for the Mixer (default: environment $TAG)
- `--pilot_hub <hub>` - Image hub for the Pilot (default: environment $HUB)
- `--pilot_tag <tag>` - Image tag for the Pilot (default: environment $TAG)
- `--proxy_hub <hub>` - Image hub for the Proxy (default: environment $HUB)
- `--proxy_tag <tag>` - Image tag for the Proxy (default: environment $TAG)
- `--ca_hub <hub>` - Image hub for the Citadel (default: environment $HUB)
- `--ca_tag <tag>` - Image tag for the Citadel (default: environment $TAG)
- `--galley_hub <hub>` - Image hub for the Sidecar Injector (default: environment $HUB)
- `--galley_tag <tag>` - Image tag for the Sidecar Injector (default: environment $TAG)
- `--sidecar_injector_hub <hub>` - Image hub for the Sidecar Injector (default: environment $HUB)
- `--sidecar_injector_tag <tag>` - Image tag for the Sidecar Injector (default: environment $TAG)
- `--sidecar_injector_file <file>` - Sidecar injector YAML file name (default: istio-sidecar-injector.yaml)
- `--image_pull_policy <policy>` - Specifies an override for the Docker image pull policy to be used
- `--cluster_registry_dir <dir>` - Directory name for the cluster registry config. When provided this will trigger a multicluster test to be run across two clusters.
- `--installer <cmd>` - Use `helm` or `kubectl` to install Istio (default: kubectl)
- `--kube_inject_configmap <configmap>` - Istioctl will use the specified configmap when running kube-inject (default: ""). This will normally be used with the CNI option to override the embedded initContainers insertion.
- `--split_horizon` - Set up a split horizon EDS multi-cluster test environment (default: false)
-
- `--use_mcp` - If true will use MCP for configuring Istio components (default: true)
- `--use_cni` - If true install the Istio CNI which will add the IP table rules for Envoy instead of the init container (default: false)
- `--cniHelmRepo` - Location/name of the Istio-CNI helm (default: istio.io/istio-cni)

## Tips

1. Running a single test
If you are debugging a single test, you can run it with `test.run` option in E2E_ARGS as follows:

    ```bash
    make e2e_mixer E2E_ARGS="--use_local_cluster --cluster_wide --test.run TestRedisQuota" HUB=localhost:5000 TAG=latest
    ```

1. Using `--skip_setup` and `--skip_cleanup` options in E2E tests
If you are debugging a test, you can choose to skip the cleanup process, so that the test setup still remains and you
can debug test easily. Also, if you are developing a new test and setup of istio system will remain same, you can use
the option to skip setup too so that you can save time for setting up istio system in e2e test.

    Ex: Using --skip_cleanup option to debug test:

    ```bash
    make e2e_mixer E2E_ARGS="--use_local_cluster --cluster_wide --skip_cleanup --test.run TestRedisQuota" HUB=localhost:5000 TAG=latest
    ```

    Ex: Using --skip_cleanup and --skip_setup option for debugging/development of test:

    ```bash
    make e2e_mixer E2E_ARGS="--use_local_cluster --cluster_wide --skip_cleanup --skip_setup --test.run TestRedisQuota" HUB=localhost:5000 TAG=latest
    ```

1. Update just one istio component you are debugging.
More often than not, you would be debugging one istio component say mixer, pilot, citadel etc. You can update a single
component when running the test using component specific HUB and TAG

    Ex: Consider a scenario where you are debugging a mixer test and just want to update mixer code when running the test again.
    1. First time, run test with option of --skip_cleanup

        ```bash
        make e2e_mixer E2E_ARGS="--use_local_cluster --cluster_wide --skip_cleanup --test.run TestRedisQuota" HUB=localhost:5000 TAG=latest
        ```

    1. Next update mixer code and build and push it to localregistry at localhost:5000 with a new TAG say latest1
    (TAG is changed, to make sure we would be using the new mixer binary)

        ```bash
        make push.docker.mixer TAG=latest
        ```

    1. Use the newly uploaded mixer image in the test now using E2E test option --mixer_tag

        ```bash
        make e2e_mixer E2E_ARGS="--use_local_cluster --cluster_wide --skip_cleanup --mixer_tag latest1 --test.run TestRedisQuota" TAG=latest
        ```

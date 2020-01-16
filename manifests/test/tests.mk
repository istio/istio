# Makefile containing the basic presubmit and integration tests, running inside a kind node or docker image.


# Run the istio install and tests. Assumes KUBECONFIG is pointing to a valid cluster.
# This should run inside a container and using KIND, to reduce dependency on host or k8s environment.
#
# It can also be used directly on the host against a real k8s cluster.
#
# This is set as default value for TEST_TARGET for runs of "make test" (full test) and "make docker-run-test" (re-running
# the tests without cleanup)
#
# If a test fails, you can re-run it by setting it as TEST_TARGET.
#
# The order of the tests is important if running all tests locally:
# - noauth/knative tests run without citadel, minimal installation - need to verify lack of certificates is not a problem
# - demo will install the demo profile, including citadel, and will call run-bookinfo test
# - run-simple, run-simple-strict, integration will use the existing 'demo' installation.
#
# Pre-submit tests shards the execution of the tests.
run-all-tests: run-build \
    run-test-noauth-micro \
    run-test-knative \
    run-test-demo \
	run-simple \
	run-simple-strict \
	test-canary \
    run-test.integration.kube.presubmit \
    run-reachability-test \
	run-prometheus-operator-config-test

# Tests using multiple namespaces. Out of scope for 1.3
run-multinamespace: run-build install-full

# Tests running against 'micro' environment - just citadel + pilot + ingress
# TODO: also add 'nano' - pilot + ingress without citadel, some users are using this a-la-carte option
run-micro-tests: install-base-chart install-base install-ingress run-simple run-simple-strict



E2E_ARGS=--skip_setup --use_local_cluster=true --istio_namespace=${ISTIO_CONTROL_NS}

SIMPLE_AUTH ?= false

# The simple test from istio/istio integration, in permissive mode
# Will kube-inject and test the ingress and service-to-service
run-simple-base: ${TMPDIR}
	mkdir -p  ${GOPATH}/out/logs
	kubectl create ns ${NS} || true
	# Global default may be strict or permissive - make it explicit for this ns
	kubectl -n ${NS} apply -f test/k8s/mtls_${MODE}.yaml
	kubectl -n ${NS} apply -f test/k8s/sidecar-local.yaml
	kubectl label ns ${NS} istio-injection=disabled --overwrite
	(set -o pipefail; cd ${GOPATH}/src/istio.io/istio; \
	 go test -v -timeout 25m ./tests/e2e/tests/simple -args \
	     --auth_enable=${SIMPLE_AUTH} \
         --egress=false \
         --ingress=false \
         --rbac_enable=false \
         --cluster_wide \
         --skip_setup \
         --use_local_cluster=true \
         --istio_namespace=${ISTIO_CONTROL_NS} \
         --namespace=${NS} \
         ${SIMPLE_EXTRA} \
         --istioctl=${ISTIOCTL_BIN} \
           2>&1 | tee ${GOPATH}/out/logs/$@.log)

run-simple:
	$(MAKE) run-simple-base ISTIO_CONTROL_NS=istio-system MODE=permissive NS=simple

# Simple test, strict mode
run-simple-strict:
	$(MAKE) run-simple-base MODE=strict ISTIO_CONTROL_NS=istio-system NS=simple-strict SIMPLE_AUTH=true

run-bookinfo-demo:
	kubectl create ns bookinfo-demo || true
	kubectl -n bookinfo-demo apply -f test/k8s/mtls_permissive.yaml
	kubectl -n bookinfo-demo apply -f test/k8s/sidecar-local.yaml
	(cd ${GOPATH}/src/istio.io/istio; make e2e_bookinfo_run ${TEST_FLAGS} \
		E2E_ARGS="${E2E_ARGS} --namespace=bookinfo-demo")

# Simple bookinfo install and curl command
# In 1.3 we'll use istio-system
run-bookinfo:
	kubectl create ns bookinfo || true
	echo ${BASE} ${GOPATH}
	# Bookinfo test
	#kubectl label namespace bookinfo istio-env=${ISTIO_CONTROL_NS} --overwrite
	kubectl -n bookinfo apply -f test/k8s/mtls_permissive.yaml
	kubectl -n bookinfo apply -f test/k8s/sidecar-local.yaml
	ONE_NAMESPACE=1 SKIP_CLEANUP=${SKIP_CLEANUP} ISTIO_CONTROL=${ISTIO_SYSTEM_NS} INGRESS_NS=${ISTIO_SYSTEM_NS} SKIP_DELETE=1 SKIP_LABEL=1 \
	  bin/test.sh ${GOPATH}/src/istio.io/istio

# Simple fortio install and curl command
#run-fortio:
#	kubectl apply -f

run-mixer:
	kubectl create ns mixertest || true
	kubectl -n mixertest apply -f test/k8s/mtls_permissive.yaml
	kubectl -n mixertest apply -f test/k8s/sidecar-local.yaml
	(cd ${GOPATH}/src/istio.io/istio; make e2e_mixer_run ${TEST_FLAGS} \
		E2E_ARGS="${E2E_ARGS} --namespace=mixertest")

# Test targets to run. Exclude tests that are broken for now
# Reachability tests are run in the 'run-minimal-test', no need to duplicate it.
# (also helps to shard the tests)
INT_TARGETS = $(shell env GO111MODULE=off GOPATH=${GOPATH} ${GO} list ../istio/tests/integration/... | grep -v "/mixer\|security/reachability\|telemetry/tracing\|/istioctl")

INT_FLAGS ?= \
	--istio.test.hub ${HUB} \
	--istio.test.tag ${TAG} \
	--istio.test.pullpolicy IfNotPresent \
	--istio.test.env kube \
	--istio.test.kube.config ${KUBECONFIG} \
	--istio.test.ci \
	--istio.test.nocleanup \
	--istio.test.kube.deploy=false \
	--istio.test.kube.systemNamespace ${ISTIO_SYSTEM_NS} \
	--istio.test.kube.istioNamespace ${ISTIO_SYSTEM_NS} \
	--istio.test.kube.configNamespace ${ISTIO_CONTROL_NS} \
	--istio.test.kube.telemetryNamespace ${ISTIO_TELEMETRY_NS} \
	--istio.test.kube.policyNamespace ${ISTIO_POLICY_NS} \
	--istio.test.kube.ingressNamespace ${ISTIO_INGRESS_NS} \
	--istio.test.kube.egressNamespace ${ISTIO_EGRESS_NS} \
	--istio.test.kube.customSidecarInjectorNamespace=${CUSTOM_SIDECAR_INJECTOR_NAMESPACE} \
	--istio.test.kube.minikube \
	--istio.test.ci -timeout 30m

# Integration tests create and delete istio-system
# Need to be fixed to use new installer
# Requires an environment with telemetry installed
run-test.integration.kube:
	export TMPDIR=${GOPATH}/out/tmp
	mkdir -p ${GOPATH}/out/tmp

	kubectl -n default apply -f test/k8s/mtls_permissive.yaml
	kubectl -n default apply -f test/k8s/sidecar-local.yaml

	set -o pipefail; \
	cd ${GOPATH}/src/istio.io/istio; \
	${GO} test -v ${INT_TARGETS} --istio.test.select -customsetup ${INT_FLAGS} 2>&1 | tee ${GOPATH}/out/logs/$@.log

run-test.integration.kube.presubmit:
	export TMPDIR=${GOPATH}/out/tmp
	mkdir -p ${GOPATH}/out/tmp ${GOPATH}/out/linux_amd64/release/ ${GOPATH}/out/logs/

	set -o pipefail; \
	cd ${GOPATH}/src/istio.io/istio; \
	${GO} test -v ${INT_TARGETS} --istio.test.select -customsetup,-flaky ${INT_FLAGS} 2>&1 | tee ${GOPATH}/out/logs/$@.log

run-stability:
	 ISTIO_ENV=${ISTIO_CONTROL_NS} bin/iop test stability ${GOPATH}/src/istio.io/tools/perf/stability/allconfig ${IOP_OPTS}

run-mysql:
	 ISTIO_ENV=${ISTIO_CONTROL_NS} bin/iop mysql mysql ${BASE}/test/mysql ${IOP_OPTS}
	 ISTIO_ENV=${ISTIO_CONTROL_NS} bin/iop mysqlplain mysqlplain ${BASE}/test/mysql ${IOP_OPTS} --set mtls=false --set Name=plain

# This test currently only validates the correct config generation and install in API server.
# When prom operator config moves out of alpha, this should be incorporated in the other tests
# and removed.
run-prometheus-operator-config-test: PROM_OPTS="--set prometheus.createPrometheusResource=true"
run-prometheus-operator-config-test: install-prometheus-operator install-prometheus-operator-config
	if [ "$$(kubectl -n ${ISTIO_CONTROL_NS} get servicemonitors -o name | wc -l)" -ne "8" ]; then echo "Failure to find ServiceMonitor resouces!"; exit 1; fi
	# kubectl wait is problematic, as the pod may not exist before the command is issued.
	until timeout ${WAIT_TIMEOUT} kubectl -n ${ISTIO_CONTROL_NS} get pod/prometheus-prometheus-0; do echo "Waiting for pods to be created..."; done
	kubectl -n ${ISTIO_CONTROL_NS} wait pod/prometheus-prometheus-0 --for=condition=Ready --timeout=${WAIT_TIMEOUT}

run-base-reachability: ENABLE_NAMESPACES_BY-DEFAULT=false
run-base-reachability: ${GOBIN}/istioctl install-base run-reachability-test

run-reachability-test:
	mkdir -p ${GOPATH}/out/logs ${GOPATH}/out/tmp
	(set -o pipefail; cd ${GOPATH}/src/istio.io/istio; \
		go test -v -run TestReachability ./tests/integration/security \
			-istio.test.env kube \
			-istio.test.kube.config=${KUBECONFIG} \
			-istio.test.nocleanup \
			-istio.test.kube.deploy=0 \
			-istio.test.kube.configNamespace=${ISTIO_CONTROL_NS} \
			-istio.test.kube.customSidecarInjectorNamespace=${CUSTOM_SIDECAR_INJECTOR_NAMESPACE})

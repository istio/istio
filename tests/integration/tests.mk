#-----------------------------------------------------------------------------
# Target: test.integration.*
#-----------------------------------------------------------------------------

# The following flags (in addition to ${V}) can be specified on the command-line, or the environment. This
# is primarily used by the CI systems.
_INTEGRATION_TEST_FLAGS ?= $(INTEGRATION_TEST_FLAGS)

# $(CI) specifies that the test is running in a CI system. This enables CI specific logging.
ifneq ($(CI),)
	_INTEGRATION_TEST_FLAGS += --istio.test.ci
	_INTEGRATION_TEST_FLAGS += --istio.test.pullpolicy=IfNotPresent
endif

ifeq ($(TEST_ENV),kind)
    _INTEGRATION_TEST_FLAGS += --istio.test.kube.loadbalancer=false
endif

ifeq ($(shell uname -m),aarch64)
    _INTEGRATION_TEST_FLAGS += --istio.test.kube.architecture=arm64
endif

ifneq ($(ARTIFACTS),)
    _INTEGRATION_TEST_FLAGS += --istio.test.work_dir=$(ARTIFACTS)
endif

ifneq ($(HUB),)
    _INTEGRATION_TEST_FLAGS += --istio.test.hub=$(HUB)
endif

ifneq ($(TAG),)
    _INTEGRATION_TEST_FLAGS += --istio.test.tag=$(TAG)
endif

_INTEGRATION_TEST_SELECT_FLAGS ?= --istio.test.select=$(TEST_SELECT)
ifneq ($(JOB_TYPE),postsubmit)
	_INTEGRATION_TEST_SELECT_FLAGS:="$(_INTEGRATION_TEST_SELECT_FLAGS),-postsubmit"
endif
ifeq ($(IP_FAMILY),ipv6)
	_INTEGRATION_TEST_SELECT_FLAGS:="$(_INTEGRATION_TEST_SELECT_FLAGS),-ipv4"
	# Fundamentally, VMs should support IPv6. However, our test framework uses a contrived setup to test VMs
	# such that they run in the cluster. In particular, they configure DNS to a public DNS server.
	# For CI, our nodes do not have IPv6 external connectivity. This means the cluster *cannot* reach these external
	# DNS servers.
	# Extensive work was done to try to hack around this, but ultimately nothing was able to cover all
	# of the edge cases. This work was captured in https://github.com/howardjohn/istio/tree/tf/vm-ipv6.
	_INTEGRATION_TEST_FLAGS += --istio.test.skipVM
endif

# $(INTEGRATION_TEST_KUBECONFIG) overrides all kube config settings.
_INTEGRATION_TEST_KUBECONFIG ?= $(INTEGRATION_TEST_KUBECONFIG)

# If $(INTEGRATION_TEST_KUBECONFIG) not specified, use $(KUBECONFIG).
ifeq ($(_INTEGRATION_TEST_KUBECONFIG),)
    _INTEGRATION_TEST_KUBECONFIG = $(KUBECONFIG)
endif

# If neither $(INTEGRATION_TEST_KUBECONFIG) nor $(KUBECONFIG) specified, use default.
ifeq ($(_INTEGRATION_TEST_KUBECONFIG),)
    _INTEGRATION_TEST_KUBECONFIG = ~/.kube/config
endif

_INTEGRATION_TEST_TOPOLOGY_FILE ?= $(INTEGRATION_TEST_TOPOLOGY_FILE)
ifneq ($(_INTEGRATION_TEST_TOPOLOGY_FILE),)
    _INTEGRATION_TEST_FLAGS += --istio.test.kube.topology=$(_INTEGRATION_TEST_TOPOLOGY_FILE)
else
	# integ-suite-kind.sh should populate the topology file with kubeconfigs
	_INTEGRATION_TEST_FLAGS += --istio.test.kube.config=$(_INTEGRATION_TEST_KUBECONFIG)
endif

RUN_TEST=$(GO) test -p 1 ${T} -tags=integ -vet=off

test.integration.analyze: test.integration...analyze

test.integration.%.analyze: | $(JUNIT_REPORT) check-go-tag
	$(RUN_TEST) ./tests/integration/$(subst .,/,$*)/... -timeout 30m \
	${_INTEGRATION_TEST_FLAGS} \
	--istio.test.analyze \
	2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_OUT))

# Ensure that all test files are tagged properly. This ensures that we don't accidentally skip tests
# and that integration tests are not run as part of the unit test suite.
check-go-tag:
	@go list ./tests/integration/... 2>/dev/null | xargs -r -I{} sh -c 'echo "Detected a file in tests/integration/ without a build tag set. Add // +build integ to the files: {}"; exit 2'

# Generate integration test targets for kubernetes environment.
test.integration.%.kube: | $(JUNIT_REPORT) check-go-tag
	$(RUN_TEST) ./tests/integration/$(subst .,/,$*)/... -timeout 30m \
	${_INTEGRATION_TEST_FLAGS} ${_INTEGRATION_TEST_SELECT_FLAGS} \
	2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_OUT))

# Generate integration fuzz test targets for kubernetes environment.
test.integration-fuzz.%.kube: | $(JUNIT_REPORT) check-go-tag
	$(GO) test -p 1 -vet=off ${T} -tags="integfuzz integ" ./tests/integration/$(subst .,/,$*)/... -timeout 30m \
	${_INTEGRATION_TEST_FLAGS} ${_INTEGRATION_TEST_SELECT_FLAGS} \
	2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_OUT))

# Generate presubmit integration test targets for each component in kubernetes environment
test.integration.%.kube.presubmit:
	@make test.integration.$*.kube

# Run all tests
.PHONY: test.integration.kube
test.integration.kube: test.integration.kube.presubmit
	@:

# Presubmit integration tests targeting Kubernetes environment. Really used for postsubmit on different k8s versions.
.PHONY: test.integration.kube.presubmit
test.integration.kube.presubmit: | $(JUNIT_REPORT) check-go-tag
	$(RUN_TEST) ./tests/integration/... -timeout 30m \
	${_INTEGRATION_TEST_FLAGS} ${_INTEGRATION_TEST_SELECT_FLAGS} \
	2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_OUT))

# Defines a target to run a standard set of tests in various different environments (IPv6, distroless, ARM, etc)
# In presubmit, this target runs a minimal set. In postsubmit, all tests are run
.PHONY: test.integration.kube.environment
test.integration.kube.environment: | $(JUNIT_REPORT) check-go-tag
ifeq (${JOB_TYPE},postsubmit)
	$(RUN_TEST) ./tests/integration/... -timeout 30m \
	${_INTEGRATION_TEST_FLAGS} ${_INTEGRATION_TEST_SELECT_FLAGS} \
	2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_OUT))
else
	$(RUN_TEST) ./tests/integration/security/ ./tests/integration/pilot -timeout 30m \
	${_INTEGRATION_TEST_FLAGS} ${_INTEGRATION_TEST_SELECT_FLAGS} \
	--test.run="TestReachability|TestTraffic" \
	2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_OUT))
endif

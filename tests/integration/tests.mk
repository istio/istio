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

ifeq ($(TEST_ENV),minikube)
    _INTEGRATION_TEST_FLAGS += --istio.test.kube.minikube
else ifeq ($(TEST_ENV),minikube-none)
    _INTEGRATION_TEST_FLAGS += --istio.test.kube.minikube
else ifeq ($(TEST_ENV),kind)
    _INTEGRATION_TEST_FLAGS += --istio.test.kube.minikube
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
ifeq ($(TEST_SELECT),)
    _INTEGRATION_TEST_SELECT_FLAGS = --istio.test.select=-postsubmit,-flaky,-multicluster
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

_INTEGRATION_TEST_FLAGS += --istio.test.kube.config=$(_INTEGRATION_TEST_KUBECONFIG)

# Generate integration test targets for kubernetes environment.
test.integration.%.kube: | $(JUNIT_REPORT)
	$(GO) test -p 1 ${T} ./tests/integration/$(subst .,/,$*)/... -timeout 30m \
	--istio.test.env kube \
	${_INTEGRATION_TEST_FLAGS} \
	2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_OUT))

# filter out non-standard test directories
TEST_PACKAGES = $(shell go list ./tests/integration/... | grep -v /qualification | grep -v /examples)

# Generate integration test targets for local environment.
test.integration.%.local: | $(JUNIT_REPORT)
	$(GO) test -p 1 ${T} -race ./tests/integration/$(subst .,/,$*)/... \
	--istio.test.env native \
	${_INTEGRATION_TEST_FLAGS} ${_INTEGRATION_TEST_SELECT_FLAGS} \
	2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_OUT))

# Generate presubmit integration test targets for each component in kubernetes environment
test.integration.%.kube.presubmit: | $(JUNIT_REPORT)
	PATH=${PATH}:${ISTIO_OUT} $(GO) test -p 1 ${T} ./tests/integration/$(subst .,/,$*)/... -timeout 30m \
	--istio.test.env kube \
	${_INTEGRATION_TEST_FLAGS} ${_INTEGRATION_TEST_SELECT_FLAGS} \
	2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_OUT))

# Presubmit integration tests targeting Kubernetes environment.
.PHONY: test.integration.kube.presubmit
test.integration.kube.presubmit: | $(JUNIT_REPORT)
	PATH=${PATH}:${ISTIO_OUT} $(GO) test -p 1 ${T} ${TEST_PACKAGES} -timeout 30m \
 	--istio.test.env kube \
	${_INTEGRATION_TEST_FLAGS} ${_INTEGRATION_TEST_SELECT_FLAGS} \
	2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_OUT))

# Defines a target to run a minimal reachability testing basic traffic
.PHONY: test.integration.kube.reachability
test.integration.kube.reachability: | $(JUNIT_REPORT)
	PATH=${PATH}:${ISTIO_OUT} $(GO) test -p 1 ${T} ./tests/integration/security/ -timeout 30m \
	--istio.test.env kube \
	${_INTEGRATION_TEST_FLAGS} \
	--test.run=TestReachability \
	2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_OUT))

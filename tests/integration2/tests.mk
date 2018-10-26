#-----------------------------------------------------------------------------
# Target: test.integration.*
#-----------------------------------------------------------------------------

# The following flags (in addition to ${V}) can be specified on the command-line, or the environment. This
# is primarily used by the CI systems.

# $(CI) specifies that the test is running in a CI system. This enables CI specific logging.
_INTEGRATION_TEST_LOGGING_FLAG =
_INTEGRATION_TEST_INGRESS_FLAG =
ifneq ($(CI),)
    _INTEGRATION_TEST_LOGGING_FLAG = --log_output_level CI:info
    #_INTEGRATION_TEST_INGRESS_FLAG = --istio.test.kube.minikubeingress
endif


# $(INTEGRATION_TEST_WORKDIR) specifies the working directory for the tests. If not specified, then a
# temporary folder is used.
_INTEGRATION_TEST_WORKDIR_FLAG =
ifneq ($(INTEGRATION_TEST_WORKDIR),)
    _INTEGRATION_TEST_WORKDIR_FLAG = --istio.test.work_dir $(INTEGRATION_TEST_WORKDIR)
endif

# $(INTEGRATION_TEST_KUBECONFIG) specifies the kube config file to be used. If not specified, then
# ~/.kube/config is used.
# TODO: This probably needs to be more intelligent and take environment variables into account.
INTEGRATION_TEST_KUBECONFIG = ~/.kube/config
ifneq ($(KUBECONFIG),)
    INTEGRATION_TEST_KUBECONFIG = $(KUBECONFIG)
endif

# This is a useful debugging target for testing everything.
.PHONY: test.integration.all
test.integration.all: test.integration test.integration.kube

JUNIT_UNIT_TEST_XML ?= $(ISTIO_OUT)/junit_unit-tests.xml
JUNIT_REPORT := $(shell which go-junit-report 2> /dev/null || echo "${ISTIO_BIN}/go-junit-report")

# All integration tests targeting local environment.
test.integration: | $(JUNIT_REPORT)
	mkdir -p $(dir $(JUNIT_UNIT_TEST_XML))
	set -o pipefail; \
	$(GO) test -p 1 ${T} ./tests/integration2/... --istio.test.env local \
	2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_UNIT_TEST_XML))

# All integration tests targeting Kubernetes environment.
test.integration.kube: | $(JUNIT_REPORT)
	mkdir -p $(dir $(JUNIT_UNIT_TEST_XML))
	set -o pipefail; \
	$(GO) test -p 1 ${T} ./tests/integration2/... ${_INTEGRATION_TEST_WORKDIR_FLAG} ${_INTEGRATION_TEST_LOGGING_FLAG} \
	--istio.test.env kubernetes \
	--istio.test.kube.config ${INTEGRATION_TEST_KUBECONFIG} \
	--istio.test.kube.deploy \
	--istio.test.kube.helm.values global.hub=${HUB},global.tag=${TAG} \
	${_INTEGRATION_TEST_INGRESS_FLAG} \
	2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_UNIT_TEST_XML))


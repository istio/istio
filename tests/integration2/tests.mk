#-----------------------------------------------------------------------------
# Target: test.integration.*
#-----------------------------------------------------------------------------

# The following flags (in addition to ${V}) can be specified on the command-line, or the environment. This
# is primarily used by the CI systems.

# $(CI) specifies that the test is running in a CI system. This enables CI specific logging.
_INTEGRATION_TEST_LOGGING_FLAG =
ifneq ($(CI),)
    _INTEGRATION_TEST_LOGGING_FLAG = --log_output_level lab:info
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

# The names of the integration test folders at ROOT/tests/integration2/*.
_INTEGRATION_TEST_NAMES = galley mixer

# Generate the names of the integration test targets that use local environment (i.e. test.integration.galley)
_INTEGRATION_TESTS_LOCAL = $(addprefix test.integration., $(_INTEGRATION_TEST_NAMES))

# Generate the names of the integration test targets that use kubernetes environment (i.e. test.integration.galley.kube)
_INTEGRATION_TESTS_KUBE  = $(addsuffix .kube, $(addprefix test.integration., $(_INTEGRATION_TEST_NAMES)))

# This is a useful debugging target for testing everything.
.PHONY: test.integration.all
test.integration.all: test.integration test.integration.kube

# All integration tests targeting local environment.
.PHONY: test.integration
test.integration: $(_INTEGRATION_TESTS_LOCAL)

# All integration tests targeting Kubernetes environment.
.PHONY: test.integration.kube
test.integration.kube: $(_INTEGRATION_TESTS_KUBE)

# Generate integration test targets for local environment.
$(_INTEGRATION_TESTS_LOCAL): test.integration.%:
	$(GO) test -p 1 ${T} ./tests/integration2/$*/... --istio.test.env local

# Generate integration test targets for kubernetes environment.
$(_INTEGRATION_TESTS_KUBE): test.integration.%.kube:
	$(GO) test -p 1 ${T} ./tests/integration2/$*/... ${_INTEGRATION_TEST_WORKDIR_FLAG} ${_INTEGRATION_TEST_LOGGING_FLAG} \
	--istio.test.env kubernetes \
	--istio.test.kube.config ${INTEGRATION_TEST_KUBECONFIG} \
	--istio.test.kube.deploy \
	--istio.test.kube.tag ${TAG} \
	--istio.test.kube.hub ${HUB}




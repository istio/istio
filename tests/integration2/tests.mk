#-----------------------------------------------------------------------------
# Target: test.integration.*
#-----------------------------------------------------------------------------

# The names of the integration test folders at ROOT/tests/integration2/*.
INTEGRATION_TEST_NAMES = galley mixer

# Generate the names of the integration test targets that use local environment (i.e. test.integration.galley)
INTEGRATION_TESTS_LOCAL = $(addprefix test.integration., $(INTEGRATION_TEST_NAMES))

# Generate the names of the integration test targets that use kubernetes environment (i.e. test.integration.galley.kube)
INTEGRATION_TESTS_KUBE  = $(addsuffix .kube, $(addprefix test.integration., $(INTEGRATION_TEST_NAMES)))

# Calculate the Kubernetes config file to use. Default to the one in user's home.
# TODO: This probably needs to be more intelligent and take environment variables into account.
INTEGRATION_TEST_KUBECONFIG = ~/.kube/config
ifneq ($(KUBECONFIG),)
    INTEGRATION_TEST_KUBECONFIG = $(KUBECONFIG)
endif

# This is a useful debugging target for testing everything.
.PHONY: test.integration.all
test.integration.all: test.integration test.integration.kube

# All integration tests targeting local environment.
.PHONY: test.integration
test.integration: $(INTEGRATION_TESTS_LOCAL)

# All integration tests targeting Kubernetes environment.
.PHONY: test.integration.kube
test.integration.kube: $(INTEGRATION_TESTS_KUBE)

# Generate integration test targets for local environment.
$(INTEGRATION_TESTS_LOCAL): test.integration.%:
	$(GO) test -p 1 ${T} ./tests/integration2/$*/... --istio.test.env local

# Generate integration test targets for kubernetes environment.
$(INTEGRATION_TESTS_KUBE): test.integration.%.kube:
	$(GO) test -p 1 ${T} ./tests/integration2/$*/... --istio.test.env kubernetes --istio.test.kube.config ${INTEGRATION_TEST_KUBECONFIG}


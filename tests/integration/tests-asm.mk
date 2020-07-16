#-----------------------------------------------------------------------------
# Target: test.integration.asm.*
#-----------------------------------------------------------------------------

ASM_INTEGRATION_TEST_TARGETS ?= $(shell go list ./tests/integration/... | grep -v /qualification | grep -v /examples)

# Presubmit integration tests targeting Kubernetes environment.
.PHONY: test.integration.asm
test.integration.asm: | $(JUNIT_REPORT)
	PATH=${PATH}:${ISTIO_OUT} $(GO) test -p 1 ${T} ${ASM_INTEGRATION_TEST_TARGETS} -timeout 30m \
	${_INTEGRATION_TEST_FLAGS} ${_INTEGRATION_TEST_SELECT_FLAGS} \
	2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_OUT))

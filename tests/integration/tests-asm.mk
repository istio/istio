#-----------------------------------------------------------------------------
# Target: test.integration.asm.*
#-----------------------------------------------------------------------------

# Presubmit integration tests targeting Kubernetes environment.
.PHONY: test.integration.asm
test.integration.asm: | $(JUNIT_REPORT)
	PATH=${PATH}:${ISTIO_OUT} $(GO) test -p 1 ${T} $(shell go list -tags=integ ./tests/integration/... | grep -v /qualification | grep -v /examples) -timeout 30m \
	${_INTEGRATION_TEST_FLAGS} ${_INTEGRATION_TEST_SELECT_FLAGS} \
	2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_OUT))

# Custom test target for ASM networking.
.PHONY: test.integration.asm.networking
test.integration.asm.networking: | $(JUNIT_REPORT)
	PATH=${PATH}:${ISTIO_OUT} $(GO) test -p 1 ${T} -tags=integ ./tests/integration/pilot/... -timeout 30m \
	${_INTEGRATION_TEST_FLAGS} ${_INTEGRATION_TEST_SELECT_FLAGS} --log_output_level=tf:debug,mcp:debug \
	2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_OUT))

# Custom test target for ASM telemetry.
# TODO: Add select tests under tests/integration/telemetry
.PHONY: test.integration.asm.telemetry
test.integration.asm.telemetry: | $(JUNIT_REPORT)
	PATH=${PATH}:${ISTIO_OUT} $(GO) test -p 1 ${T} -tags=integ ./tests/integration/multiclusterasm/... \
	 ./tests/integration/telemetry/stats/prometheus/... ./tests/integration/telemetry/stackdriver/vm/... -timeout 30m \
	${_INTEGRATION_TEST_FLAGS} ${_INTEGRATION_TEST_SELECT_FLAGS} --log_output_level=tf:debug,mcp:debug \
	2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_OUT))

# Custom test target for ASM security.
.PHONY: test.integration.asm.security
test.integration.asm.security: | $(JUNIT_REPORT)
	PATH=${PATH}:${ISTIO_OUT} $(GO) test -p 1 ${T} -tags=integ ./tests/integration/security/... -timeout 30m \
	${_INTEGRATION_TEST_FLAGS} ${_INTEGRATION_TEST_SELECT_FLAGS} --log_output_level=tf:debug,mcp:debug \
	2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_OUT))

# Test canary, assume istio-system is already installed.
test-canary: run-build-canary
	# Install Pilot canary
	kubectl apply -k kustomize/istio-canary --prune -l release=istio-canary

	# Wait for it to startup
	kubectl wait deployments istio-pilotcanary -n ${ISTIO_SYSTEM_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}

	$(MAKE) test-canary-tests

test-canary-tests:
	# Manual injection test for canary. Settings in values.yaml and mesh.yaml
	# values and mesh are set to use TLS to pilot
	kubectl create ns fortio-canary || true
	kubectl label ns fortio-canary istio-injection=disabled --overwrite

	kubectl apply -n fortio-canary -f test/canary/sidecar.yaml
	istioctl kube-inject -f test/canary/fortio.yaml \
		-n fortio-canary \
		--meshConfigFile test/canary/mesh.yaml \
		--valuesFile test/canary/values.yaml \
		--injectConfigFile istio-control/istio-autoinject/files/injection-template.yaml \
	 | kubectl apply -n fortio-canary -f -

	 kubectl wait deployments fortio -n fortio-canary --for=condition=available --timeout=${WAIT_TIMEOUT}

	# Namespace with auto-injection
	kubectl apply -k test/canary
	kubectl wait deployments cli-fortio -n fortio-canary-inject --for=condition=available --timeout=${WAIT_TIMEOUT}


# Test the demo install - in istio-system and the 'side by side'/upgrade mode.
# This requires a fresh kind cluster.

INSTALL_OPTS="--set global.istioNamespace=${ISTIO_CONTROL_NS} --set global.configNamespace=${ISTIO_CONTROL_NS} --set global.telemetryNamespace=${ISTIO_TELEMETRY_NS} --set global.policyNamespace=${ISTIO_POLICY_NS}"

test-demo-simple:
	$(MAKE) KIND_CLUSTER=${KIND_CLUSTER}-demo maybe-clean maybe-prepare sync
	$(MAKE) KIND_CLUSTER=${KIND_CLUSTER}-demo kind-run TARGET="run-test-demo"



# Run the 'install demo' test. Should run with a valid kube config and cluster - KIND or real.
run-test-demo: ${TMPDIR}
	kubectl apply -k github.com/istio/installer/crds
	kubectl apply -k test/demo	
	kubectl wait deployments istio-citadel11 -n ${ISTIO_SYSTEM_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}
	kubectl wait deployments istio-galley istio-pilot -n ${ISTIO_CONTROL_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}
	kubectl wait deployments istio-sidecar-injector -n ${ISTIO_CONTROL_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}
	kubectl wait deployments istio-ingressgateway -n ${ISTIO_INGRESS_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}
	kubectl wait deployments istio-telemetry prometheus grafana -n ${ISTIO_TELEMETRY_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}

	# Verify that we can kube-inject using files ( there is no injector in this config )
	kubectl create ns demo || true
	istioctl kube-inject -f test/simple/servicesToBeInjected.yaml \
		-n demo \
		--meshConfigFile test/demo/mesh.yaml \
		--valuesFile test/simple/values.yaml \
		--injectConfigFile istio-control/istio-autoinject/files/injection-template.yaml \
	 | kubectl apply -n demo -f -


test-demo-multi:
	$(MAKE) KIND_CLUSTER=${KIND_CLUSTER}-upgrade maybe-clean maybe-prepare sync
	$(MAKE) KIND_CLUSTER=${KIND_CLUSTER}-upgrade kind-run TARGET="run-test-demoupgrade"


# Galley, Pilot, Ingress, Telemetry (separate ns)
run-test-demoupgrade: run-build
	kubectl apply -f test/istio-system-1.0.6.yaml
	kubectl apply -k installer/crds
	kubectl apply -k test/demo/istio-testing

# Test the demo install - in istio-system and the 'side by side'/upgrade mode.
# This requires a fresh kind cluster.

INSTALL_OPTS="--set global.istioNamespace=${ISTIO_CONTROL_NS} --set global.configNamespace=${ISTIO_CONTROL_NS} --set global.telemetryNamespace=${ISTIO_TELEMETRY_NS} --set global.policyNamespace=${ISTIO_POLICY_NS}"


# Run the 'install demo' test. Should run with a valid kube config and cluster - KIND or real.
# The demo environment should be compatible and we should be able to upgrade from 1.2
#
# If you repeat the test without deleting the cluster the config will be fast, and will just run the curl calls.
run-test-demo: ${GOBIN}/istioctl run-build-cluster run-build-demo ${TMPDIR}
	kubectl apply -k kustomize/cluster

	kubectl apply -k test/demo --prune -l release=istio-system-istio
	$(MAKE) wait-all-system

	# Verify that we can kube-inject using files
	kubectl create ns demo || true
	istioctl kube-inject -f test/simple/servicesToBeInjected.yaml \
		-n demo \
		--meshConfigFile test/demo/mesh.yaml \
		--valuesFile test/simple/values.yaml \
		--injectConfigFile istio-control/istio-autoinject/files/injection-template.yaml \
	 | kubectl apply -n demo -f -

	kubectl wait deployments echosrv-deployment-1 -n demo --for=condition=available --timeout=${WAIT_TIMEOUT}

	# Do a simple test for bookinfo
	$(MAKE) run-bookinfo



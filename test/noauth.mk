# Targets for testing the installer in 'minimal' mode. Both configs install only pilot and optional ingress or
# manual-injected sidecars.

# Security is not enabled - this can be used for users who have ipsec or other secure VPC, or don't need the
# security features. It is also intended to verify that Istio can work without citadel for a-la-carte modes.

run-test-noauth: ${GOBIN}/istioctl run-build run-test-noauth-micro run-test-noauth-full run-test-knative

# Run a test with the smallest/simplest install possible
run-test-noauth-micro:
	kubectl apply -k kustomize/cluster --prune -l istio=cluster

	# Verify that we can kube-inject using files ( there is no injector in this config )
	kubectl create ns simple-micro || true

	# Use a kustomization to lower the alloc (to fit in circle)
	kubectl apply -k test/minimal --prune -l release=istio-system-istio-discovery
	kubectl apply -k test/minimal --prune -l release=istio-system-istio-ingress

	kubectl wait deployments istio-pilot istio-ingressgateway -n istio-system --for=condition=available --timeout=${WAIT_TIMEOUT}

	# Add a node port service, so the tests can also run from container - port is 30080
	# If running with 'local mount' - it also sets a port in the main docker contaier, so port is accessible from dev
	# machine. Otherwise the test should in inside the kind container node.
	kubectl apply -f test/kind/ingress-service.yaml

	# Apply an ingress, to verify ingress is configured properly
	kubectl apply -f test/simple/ingress.yaml

	istioctl kube-inject -f test/simple/servicesToBeInjected.yaml \
		-n simple-micro \
		--meshConfigFile test/simple/mesh.yaml \
		--valuesFile test/simple/values.yaml \
		--injectConfigFile istio-control/istio-autoinject/files/injection-template.yaml \
	 | kubectl apply -n simple-micro -f -

	kubectl wait deployments echosrv-deployment-1 -n simple-micro --for=condition=available --timeout=${WAIT_TIMEOUT}
	kubectl wait deployments echosrv-deployment-2 -n simple-micro --for=condition=available --timeout=${WAIT_TIMEOUT}

	# Verify ingress and pilot are happy
	# The 'simple' fortio has a rewrite rule - so /fortio/fortio/ is the real UI
	timeout 10s sh -c 'until curl -s localhost:30080/fortio/fortio/ | grep fortio_chart.js; do echo "retrying..."; sleep .1; done'

	# This is the ingress gateway, no rewrite. Without host it hits the redirect
	timeout 3s sh -c 'until curl -s localhost:30080/fortio/ -HHost:fortio-ingress.example.com | grep fortio_chart.js; do echo "retrying..."; sleep .1; done'

# Installs minimal istio (pilot + ingressgateway) to support knative serving.
# Then installs a simple service and waits for the route to be ready.
#
# This test can be run in several environments:
# - using a 'minimal' pilot+ingress in istio-system
# - using a full istio in istio-system
# - using only a 'minimal' istio+ingress in a separate namespace - nothing in istio-system
# The last config seems to be broken in CircleCI but passes locally, still investigating.
run-test-knative: run-build-cluster run-build-minimal run-build-ingress
	kubectl apply -k kustomize/cluster --prune -l istio=cluster

	# Install Knative CRDs (istio-crds applied via install-crds)
	# Using serving seems to be flaky - no matches for kind "Image" in version "caching.internal.knative.dev/v1alpha1"
	kubectl apply --selector=knative.dev/crd-install=true --filename test/knative/crds.yaml
	kubectl wait --for=condition=Established -f test/knative/crds.yaml

	# Install pilot, ingress - using a kustomization that installs them in istio-micro instead of istio-system
	# The kustomization installs a modified istio-ingress+istio-pilot, using separate namespace
	kubectl apply -k test/knative
	kubectl wait deployments istio-ingressgateway istio-pilot -n istio-micro --for=condition=available --timeout=360s

	# Set host port 30090, for the ingressateway in istio-micro
	kubectl apply -f test/kind/ingress-service-micro.yaml

	kubectl apply --filename test/knative/serving.yaml

	kubectl wait deployments webhook controller activator autoscaler \
	  -n knative-serving --for=condition=available --timeout=${WAIT_TIMEOUT}

	kubectl apply --filename test/knative/service.yaml

	# The route may take some small period of time to be create (WAIT_TIMEOUT default is 240s)
	# kubectl wait is problematic, as the pod may not exist before the command is issued.
	until timeout 120s kubectl get routes helloworld-go; do echo "Waiting for routes to be created..."; done
	kubectl wait routes helloworld-go --for=condition=ready --timeout=120s

	# Verify that ingress, pilot and knative are all happy
	#curl localhost:30090/hello -v -H Host:helloworld-go.default.example.com


run-test-noauth-full:
	echo "Skipping - only micro profile in scope, will use telemetry-lite"

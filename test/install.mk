# Included file for generating the install files and installing istio for testing or use.
# This runs inside a build container where all the tools are available.

# The main makefile is responsible for starting the docker image that runs the steps in this makefile.


# Verify each component can be generated. Create pre-processed yaml files with the defaults.
# TODO: minimize 'ifs' in templates, and generate alternative files for cases we can't remove. The output could be
# used directly with kubectl apply -f https://....
# TODO: Add a local test - to check various things are in the right place (jsonpath or equivalent)
# TODO: run a local etcd/apiserver and verify apiserver accepts the files
run-build: dep
	mkdir -p ${OUT}/release
	cp -aR crds/ ${OUT}/release
	bin/iop istio-system istio-system-security ${BASE}/security/citadel -t > ${OUT}/release/citadel.yaml
	bin/iop ${ISTIO_NS} istio-config ${BASE}/istio-control/istio-config -t > ${OUT}/release/istio-config.yaml
	bin/iop ${ISTIO_NS} istio-discovery ${BASE}/istio-control/istio-discovery -t > ${OUT}/release/istio-discovery.yaml
	bin/iop ${ISTIO_NS} istio-autoinject ${BASE}/istio-control/istio-autoinject -t > ${OUT}/release/istio-autoinject.yaml
	bin/iop ${ISTIO_NS} istio-ingress ${BASE}/gateways/istio-ingress -t > ${OUT}/release/istio-ingress.yaml
	bin/iop ${ISTIO_NS} istio-egress ${BASE}/gateways/istio-egress -t > ${OUT}/release/istio-egress.yaml
	bin/iop ${ISTIO_NS} istio-telemetry ${BASE}/istio-telemetry/mixer-telemetry -t > ${OUT}/release/istio-telemetry.yaml
	bin/iop ${ISTIO_NS} istio-telemetry ${BASE}/istio-telemetry/prometheus -t > ${OUT}/release/istio-prometheus.yaml
	bin/iop ${ISTIO_NS} istio-telemetry ${BASE}/istio-telemetry/grafana -t > ${OUT}/release/istio-grafana.yaml
	#bin/iop ${ISTIO_NS} istio-policy ${BASE}/istio-policy -t > ${OUT}/release/istio-policy.yaml
	#bin/iop ${ISTIO_NS} istio-cni ${BASE}/istio-cni -t > ${OUT}/release/istio-cni.yaml
	# TODO: generate single config (merge all yaml)
	# TODO: different common user-values combinations
	# TODO: apply to a local kube apiserver to validate against k8s
	# Short term: will be checked in - for testing apply -k
	cat crds/*.yaml ${OUT}/release/*.yaml > test/demo/k8s.yaml

run-lint:
	helm lint istio-control/istio-discovery -f global.yaml
	helm lint istio-control/istio-config -f global.yaml
	helm lint istio-control/istio-autoinject -f global.yaml
	helm lint istio-policy -f global.yaml
	helm lint istio-telemetry/grafana -f global.yaml
	helm lint istio-telemetry/mixer-telemetry -f global.yaml
	helm lint istio-telemetry/prometheus -f global.yaml
	helm lint security/citadel -f global.yaml
	helm lint gateways/istio-egress -f global.yaml
	helm lint gateways/istio-ingress -f global.yaml


install-full: ${TMPDIR} install-crds install-base install-ingress install-telemetry install-policy

.PHONY: ${GOPATH}/out/yaml/crds
# Install CRDS
install-crds: crds
	kubectl apply -f crds/
	kubectl wait --for=condition=Established -f crds/

# Individual step to install or update base istio.
# This setup is optimized for migration from 1.1 and testing - note that autoinject is enabled by default,
# since new integration tests seem to fail to inject
install-base: install-crds
	kubectl create ns ${ISTIO_NS} || true
	# Autoinject global enabled - we won't be able to install injector
	kubectl label ns ${ISTIO_NS} istio-injection=disabled --overwrite
	bin/iop istio-system istio-system-security ${BASE}/security/citadel ${IOP_OPTS}
	kubectl wait deployments istio-citadel11 -n istio-system --for=condition=available --timeout=${WAIT_TIMEOUT}
	bin/iop ${ISTIO_NS} istio-config ${BASE}/istio-control/istio-config ${IOP_OPTS}
	bin/iop ${ISTIO_NS} istio-discovery ${BASE}/istio-control/istio-discovery ${IOP_OPTS}
	kubectl wait deployments istio-galley istio-pilot -n ${ISTIO_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}
	bin/iop ${ISTIO_NS} istio-autoinject ${BASE}/istio-control/istio-autoinject --set sidecarInjectorWebhook.enableNamespacesByDefault=true \
		--set global.istioNamespace=${ISTIO_NS} ${IOP_OPTS}
	kubectl wait deployments istio-sidecar-injector -n ${ISTIO_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}

# Some tests assumes ingress is in same namespace with pilot.
# TODO: fix test (or replace), break it down in multiple namespaces for isolation/hermecity
install-ingress:
	bin/iop ${ISTIO_NS} istio-ingress ${BASE}/gateways/istio-ingress --set global.istioNamespace=${ISTIO_NS} ${IOP_OPTS}
	kubectl wait deployments ingressgateway -n ${ISTIO_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}

install-egress:
	bin/iop istio-egress istio-egress ${BASE}/gateways/istio-egress --set global.istioNamespace=${ISTIO_NS} ${IOP_OPTS}
	kubectl wait deployments egressgateway -n istio-egress --for=condition=available --timeout=${WAIT_TIMEOUT}

# Telemetry will be installed in istio-control for the tests, until integration tests are changed
# to expect telemetry in separate namespace
install-telemetry:
	#bin/iop istio-telemetry istio-grafana $IBASE/istio-telemetry/grafana/ --set global.istioNamespace=${ISTIO_NS}
	bin/iop ${ISTIO_NS} istio-prometheus ${BASE}/istio-telemetry/prometheus/ --set global.istioNamespace=${ISTIO_NS} ${IOP_OPTS}
	bin/iop ${ISTIO_NS} istio-mixer ${BASE}/istio-telemetry/mixer-telemetry/ --set global.istioNamespace=${ISTIO_NS} ${IOP_OPTS}
	kubectl wait deployments istio-telemetry prometheus -n ${ISTIO_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}

install-policy:
	bin/iop ${ISTIO_NS} istio-policy ${BASE}/istio-policy --set global.istioNamespace=${ISTIO_NS} ${IOP_OPTS}
	kubectl wait deployments istio-policy -n ${ISTIO_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}


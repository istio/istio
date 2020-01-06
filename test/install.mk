# Included file for generating the install files and installing istio for testing or use.
# This runs inside a build container where all the tools are available.

# The main makefile is responsible for starting the docker image that runs the steps in this makefile.

INSTALL_OPTS="--set global.istioNamespace=${ISTIO_CONTROL_NS} --set global.configNamespace=${ISTIO_CONTROL_NS} --set global.telemetryNamespace=${ISTIO_TELEMETRY_NS} --set global.policyNamespace=${ISTIO_POLICY_NS}"

# Verify each component can be generated. If "ONE_NAMESPACE" is set to 1, then create pre-processed yaml files with the defaults
# all in "istio-control" namespace, otherwise, create pre-processed yaml in isolated namespaces for different components.
# TODO: minimize 'ifs' in templates, and generate alternative files for cases we can't remove. The output could be
# used directly with kubectl apply -f https://....
# TODO: Add a local test - to check various things are in the right place (jsonpath or equivalent)
# TODO: run a local etcd/apiserver and verify apiserver accepts the files
run-build:  dep run-build-cluster run-build-demo run-build-micro run-build-minimal run-build-citadel run-build-default run-build-canary run-build-ingress

# Kustomization for cluster-wide resources. Must be used as first step ( if old installer was used - this might not be
# required).
run-build-cluster:
	bin/iop istio-system cluster ${BASE}/base -t > kustomize/cluster/crds-namespace.gen.yaml

# Micro profile - just pilot and ingress, in a separate namespace.
# This can be used side-by-side with istio-system. For example knative uses a similar config as ingress while allowing
# full istio to run at the same time.
run-build-micro: run-build-cluster
	# No longer used, minimal matching original istio.

# Minimal profile. Similar with minimal profile in old installer
run-build-minimal:
	bin/iop istio-system istio-discovery ${BASE}/istio-control/istio-discovery  -t \
	  --set global.controlPlaneSecurityEnabled=false \
	  --set pilot.useMCP=false \
	  --set pilot.ingress.ingressControllerMode=STRICT \
	  --set global.mtls.auto=false \
	  --set pilot.plugins="health" > kustomize/minimal/discovery.gen.yaml

# Generate config for ingress matching minimal profie. Runs in istio-system.
run-build-ingress:
	bin/iop istio-system istio-ingress ${BASE}/gateways/istio-ingress  -t \
	  --set global.istioNamespace=istio-system \
	  --set global.k8sIngress.enabled=true \
	  --set global.controlPlaneSecurityEnabled=false \
      > kustomize/istio-ingress/istio-ingress.gen.yaml

	# Required since we can't yet kustomize the CLI ( need to switch to viper and env first )
	bin/iop istio-micro istio-ingress ${BASE}/gateways/istio-ingress  -t \
	  --set global.istioNamespace=istio-micro \
	  --set global.configNamespace=istio-micro \
	  --set global.k8sIngress.enabled=true \
      --set global.controlPlaneSecurityEnabled=false \
	  --set global.mtls.auto=false \
      > test/knative/istio-ingress.gen.yaml

run-build-citadel:
	bin/iop ${ISTIO_SYSTEM_NS} istio-system-security ${BASE}/security/citadel -t --set kustomize=true > kustomize/citadel/citadel.gen.yaml

# A canary pilot, to be used for testing config changes in pilot.
run-build-canary: run-build-cluster
	# useMCP is false because Galley with old installer uses the DNS cert, which is hard to manage.
	# In operator/new installer galley MCP side has a sidecar and uses normal spiffe: cert.
	bin/iop ${ISTIO_SYSTEM_NS} pilot-canary istio-control/istio-discovery -t \
    		--set pilot.useMCP=false \
    	  	--set clusterResources=false \
    		--set version=canary > kustomize/istio-canary/discovery.gen.yaml

run-build-default: dep
	bin/iop ${ISTIO_SYSTEM_NS} istio ${BASE}/security/citadel -t > kustomize/default/istio-citadel.gen.yaml
	bin/iop ${ISTIO_SYSTEM_NS} istio ${BASE}/istio-control/istio-config  -t > kustomize/default/istio-config.gen.yaml
	bin/iop ${ISTIO_SYSTEM_NS} istio ${BASE}/istio-control/istio-discovery -t > kustomize/default/istio-discovery.gen.yaml
	bin/iop ${ISTIO_SYSTEM_NS} istio ${BASE}/istio-control/istio-autoinject  -t > kustomize/default/istio-autoinject.gen.yaml
	bin/iop ${ISTIO_SYSTEM_NS} istio ${BASE}/gateways/istio-ingress -t > kustomize/default/istio-ingress.gen.yaml
	bin/iop ${ISTIO_SYSTEM_NS} istio ${BASE}/istio-telemetry/mixer-telemetry -t > kustomize/default/istio-telemetry.gen.yaml
	bin/iop ${ISTIO_SYSTEM_NS} istio ${BASE}/istio-telemetry/prometheus  -t > kustomize/default/istio-prometheus.gen.yaml
	bin/iop ${ISTIO_SYSTEM_NS} istio ${BASE}/istio-telemetry/grafana -t > kustomize/default/istio-grafana.gen.yaml

DEMO_OPTS="-f test/demo/values.yaml"

# Demo updates the demo profile. After testing it can be checked in - allowing reviewers to see any changes.
# For backward compat, demo profile uses istio-system.
run-build-demo: dep
	bin/iop ${ISTIO_SYSTEM_NS} istio ${BASE}/security/citadel -t ${DEMO_OPTS} > test/demo/citadel.gen.yaml
	bin/iop ${ISTIO_SYSTEM_NS} istio ${BASE}/istio-control/istio-config -t ${DEMO_OPTS} > test/demo/galley.gen.yaml
	bin/iop ${ISTIO_SYSTEM_NS} istio ${BASE}/istio-control/istio-discovery -t ${DEMO_OPTS} > test/demo/pilot.gen.yaml
	bin/iop ${ISTIO_SYSTEM_NS} istio ${BASE}/istio-control/istio-autoinject -t ${DEMO_OPTS} \
	  --set sidecarInjectorWebhook.enableNamespacesByDefault=${ENABLE_NAMESPACES_BY_DEFAULT} > test/demo/inject-allns.gen.yaml
	bin/iop ${ISTIO_SYSTEM_NS} istio ${BASE}/gateways/istio-ingress -t ${DEMO_OPTS} > test/demo/ingress.gen.yaml
	bin/iop ${ISTIO_SYSTEM_NS} istio ${BASE}/istio-telemetry/mixer-telemetry -t ${DEMO_OPTS} > test/demo/telemetry.gen.yaml

	# Extras present only in demo profile
	bin/iop ${ISTIO_SYSTEM_NS} istio ${BASE}/gateways/istio-egress -t ${DEMO_OPTS} > test/demo/egress.gen.yaml
	bin/iop ${ISTIO_SYSTEM_NS} istio ${BASE}/istio-telemetry/prometheus -t ${DEMO_OPTS} > test/demo/prometheus.gen.yaml
	bin/iop ${ISTIO_SYSTEM_NS} istio ${BASE}/istio-telemetry/grafana -t ${DEMO_OPTS} > test/demo/grafana.gen.yaml
	bin/iop ${ISTIO_SYSTEM_NS} istio ${BASE}/istio-policy -t ${DEMO_OPTS} > test/demo/policy.gen.yaml
	bin/iop ${ISTIO_SYSTEM_NS} istio ${BASE}/istio-telemetry/kiali -t ${DEMO_OPTS} > test/demo/kiali.gen.yaml
	bin/iop ${ISTIO_SYSTEM_NS} istio ${BASE}/istio-telemetry/tracing -t ${DEMO_OPTS} > test/demo/tracing.gen.yaml

install-full: ${TMPDIR} install-base-chart install-base install-ingress install-telemetry install-policy

.PHONY: ${GOPATH}/out/yaml/base
# Install base
install-base-chart: base
	kubectl apply -f base/files
	kubectl wait --for=condition=Established -f base/files

# Individual step to install or update base istio. Citadel or cert provisioning components is by default deployed
# into "istio-system" ns, while config, discovery, auto-inject components are deployed into "istio-control" ns.
# This setup is optimized for migration from 1.1 and testing - note that autoinject is enabled by default,
# since new integration tests seem to fail to inject
install-base: install-base-chart
	kubectl create ns ${ISTIO_SYSTEM_NS} || true
	# Autoinject global enabled - we won't be able to install injector
	kubectl label ns ${ISTIO_SYSTEM_NS} istio-injection=disabled --overwrite
	bin/iop ${ISTIO_SYSTEM_NS} istio-system-security ${BASE}/security/citadel ${IOP_OPTS} ${INSTALL_OPTS}
	kubectl wait deployments istio-citadel -n ${ISTIO_SYSTEM_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}
	kubectl create ns ${ISTIO_CONTROL_NS} || true
	kubectl label ns ${ISTIO_CONTROL_NS} istio-injection=disabled --overwrite
	bin/iop ${ISTIO_CONTROL_NS} istio-config ${BASE}/istio-control/istio-config ${IOP_OPTS} ${INSTALL_OPTS}
	bin/iop ${ISTIO_CONTROL_NS} istio-discovery ${BASE}/istio-control/istio-discovery ${IOP_OPTS}  ${INSTALL_OPTS}
	kubectl wait deployments istio-galley istio-pilot -n ${ISTIO_CONTROL_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}
	bin/iop ${ISTIO_CONTROL_NS} istio-autoinject ${BASE}/istio-control/istio-autoinject --set sidecarInjectorWebhook.enableNamespacesByDefault=${ENABLE_NAMESPACES_BY_DEFAULT} \
		 ${IOP_OPTS} ${INSTALL_OPTS}


# Some tests assumes ingress is in same namespace with pilot. If "ONE_NAMESPACE" is set to 1, then install the
# ingress with the defaults into "istio-system" ns, otherwise, install it into "istio-gateways" ns.
install-ingress:
	kubectl create ns ${ISTIO_INGRESS_NS} || true
	kubectl label ns ${ISTIO_INGRESS_NS} istio-injection=disabled --overwrite
	bin/iop ${ISTIO_INGRESS_NS} istio-ingress ${BASE}/gateways/istio-ingress  ${IOP_OPTS} ${INSTALL_OPTS}
	kubectl wait deployments istio-ingressgateway -n ${ISTIO_INGRESS_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}

wait-all-system:
	kubectl wait deployments istio-citadel -n ${ISTIO_SYSTEM_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}
	kubectl wait deployments istio-galley istio-pilot -n ${ISTIO_SYSTEM_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}
	kubectl wait deployments istio-ingressgateway -n ${ISTIO_SYSTEM_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}
	kubectl wait deployments istio-telemetry prometheus grafana -n ${ISTIO_SYSTEM_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}


install-egress:
	kubectl create ns ${ISTIO_EGRESS_NS} || true
	kubectl label ns ${ISTIO_EGRESS_NS} istio-injection=disabled --overwrite
	bin/iop ${ISTIO_EGRESS_NS} istio-egress ${BASE}/gateways/istio-egress  ${IOP_OPTS} ${INSTALL_OPTS}
	kubectl wait deployments istio-egressgateway -n ${ISTIO_EGRESS_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}

# If "ONE_NAMESPACE" is set to 1, then install the telemetry component with the defaults into "istio-control" ns,
# otherwise, install it into "istio-telemetry" ns.
install-telemetry:
	kubectl create ns ${ISTIO_TELEMETRY_NS} || true
	kubectl label ns ${ISTIO_TELEMETRY_NS} istio-injection=disabled --overwrite
	#bin/iop istio-telemetry istio-grafana $IBASE/istio-telemetry/grafana/
	bin/iop ${ISTIO_TELEMETRY_NS} istio-prometheus ${BASE}/istio-telemetry/prometheus/ ${IOP_OPTS} ${INSTALL_OPTS}
	bin/iop ${ISTIO_TELEMETRY_NS} istio-mixer ${BASE}/istio-telemetry/mixer-telemetry/ ${IOP_OPTS} ${INSTALL_OPTS}
	kubectl wait deployments istio-telemetry prometheus -n ${ISTIO_TELEMETRY_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}

# Install kiali separately with telemetry
install-kiali:
	bin/iop ${ISTIO_ADMIN_NS} istio-kiali ${BASE}/istio-telemetry/kiali/ ${IOP_OPTS}
	kubectl wait deployments istio-kiali -n ${ISTIO_ADMIN_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}

install-tracing:
	bin/iop ${ISTIO_NS} istio-tracing ${BASE}/istio-telemetry/tracing/  ${IOP_OPTS}
	kubectl wait deployments istio-tracing -n ${ISTIO_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}

install-policy:
	kubectl create ns ${ISTIO_POLICY_NS} || true
	kubectl label ns ${ISTIO_POLICY_NS} istio-injection=disabled --overwrite
	bin/iop ${ISTIO_POLICY_NS} istio-policy ${BASE}/istio-policy  ${IOP_OPTS} ${INSTALL_OPTS}
	kubectl wait deployments istio-policy -n ${ISTIO_POLICY_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}

# This target should only be used in situations in which the prom operator has not already been installed in a cluster.
install-prometheus-operator: PROM_OP_NS="prometheus-operator"
install-prometheus-operator:
	kubectl create ns ${PROM_OP_NS} || true
	kubectl label ns ${PROM_OP_NS} istio-injection=disabled --overwrite
	curl -s https://raw.githubusercontent.com/coreos/prometheus-operator/master/bundle.yaml | sed "s/namespace: default/namespace: ${PROM_OP_NS}/g" | kubectl apply -f -
	kubectl -n ${PROM_OP_NS} wait --for=condition=available --timeout=${WAIT_TIMEOUT} deploy/prometheus-operator
	# kubectl wait is problematic, as the CRDs may not exist before the command is issued.
	until timeout ${WAIT_TIMEOUT} kubectl get crds/prometheuses.monitoring.coreos.com; do echo "Waiting for CRDs to be created..."; done
	until timeout ${WAIT_TIMEOUT} kubectl get crds/alertmanagers.monitoring.coreos.com; do echo "Waiting for CRDs to be created..."; done
	until timeout ${WAIT_TIMEOUT} kubectl get crds/podmonitors.monitoring.coreos.com; do echo "Waiting for CRDs to be created..."; done
	until timeout ${WAIT_TIMEOUT} kubectl get crds/prometheusrules.monitoring.coreos.com; do echo "Waiting for CRDs to be created..."; done
	until timeout ${WAIT_TIMEOUT} kubectl get crds/servicemonitors.monitoring.coreos.com; do echo "Waiting for CRDs to be created..."; done


# This target expects that the prometheus operator (and its CRDs have already been installed).
# It is provided as a way to install Istio prometheus operator config in isolation.
install-prometheus-operator-config:
	kubectl create ns ${ISTIO_CONTROL_NS} || true
	kubectl label ns ${ISTIO_CONTROL_NS} istio-injection=disabled --overwrite
	# NOTE: we don't use iop to install, as it defaults to `--prune`, which is incompatible with the prom operator (it prunes the stateful set)
	bin/iop ${ISTIO_CONTROL_NS} istio-prometheus-operator ${BASE}/istio-telemetry/prometheus-operator/ -t ${PROM_OPTS} | kubectl apply -n ${ISTIO_CONTROL_NS} -f -

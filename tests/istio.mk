# tests/istio.mk defines E2E tests for Istio

E2E_TIMEOUT ?= 60m

# If set outside, it appears it is not possible to modify the variable.
E2E_ARGS ?=

ISTIOCTL_BIN ?= ${ISTIO_OUT}/istioctl

DEFAULT_EXTRA_E2E_ARGS = ${MINIKUBE_FLAGS}
DEFAULT_EXTRA_E2E_ARGS += --istioctl=${ISTIOCTL_BIN}
DEFAULT_EXTRA_E2E_ARGS += --mixer_tag=${TAG_VARIANT}
DEFAULT_EXTRA_E2E_ARGS += --pilot_tag=${TAG_VARIANT}
DEFAULT_EXTRA_E2E_ARGS += --proxy_tag=${TAG_VARIANT}
DEFAULT_EXTRA_E2E_ARGS += --ca_tag=${TAG_VARIANT}
DEFAULT_EXTRA_E2E_ARGS += --galley_tag=${TAG_VARIANT}
DEFAULT_EXTRA_E2E_ARGS += --sidecar_injector_tag=${TAG_VARIANT}
DEFAULT_EXTRA_E2E_ARGS += --app_tag=${TAG_VARIANT}
DEFAULT_EXTRA_E2E_ARGS += --mixer_hub=${HUB}
DEFAULT_EXTRA_E2E_ARGS += --pilot_hub=${HUB}
DEFAULT_EXTRA_E2E_ARGS += --proxy_hub=${HUB}
DEFAULT_EXTRA_E2E_ARGS += --ca_hub=${HUB}
DEFAULT_EXTRA_E2E_ARGS += --galley_hub=${HUB}
DEFAULT_EXTRA_E2E_ARGS += --sidecar_injector_hub=${HUB}
DEFAULT_EXTRA_E2E_ARGS += --app_hub=${HUB}

# Enable Istio CNI in helm template commands
export ENABLE_ISTIO_CNI ?= false

EXTRA_E2E_ARGS ?= ${DEFAULT_EXTRA_E2E_ARGS}

e2e_simple: build generate_e2e_yaml e2e_simple_run

e2e_simple_noauth: build generate_e2e_yaml e2e_simple_noauth_run

e2e_mixer: build generate_e2e_yaml e2e_mixer_run

e2e_dashboard: build generate_e2e_yaml e2e_dashboard_run

e2e_stackdriver: build generate_e2e_yaml e2e_stackdriver_run

e2e_pilotv2_v1alpha3: build test/local/noauth/e2e_pilotv2

e2e_bookinfo_envoyv2_v1alpha3: build test/local/auth/e2e_bookinfo_envoyv2

e2e_bookinfo_trustdomain: build test/local/auth/e2e_bookinfo_trustdomain

e2e_simple_run:
	go test -v -timeout ${E2E_TIMEOUT} ./tests/e2e/tests/simple -args --auth_enable=true \
	--egress=false --ingress=false \
	--valueFile test-values/values-e2e.yaml \
	--rbac_enable=false --cluster_wide ${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS}

e2e_simple_noauth_run:
	go test -v -timeout ${E2E_TIMEOUT} ./tests/e2e/tests/simple -args --auth_enable=false \
	--egress=false --ingress=false \
	--valueFile test-values/values-e2e.yaml \
	--rbac_enable=false --cluster_wide ${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS}

e2e_mixer_run:
	go test -v -timeout ${E2E_TIMEOUT} ./tests/e2e/tests/mixer \
	--auth_enable=false --egress=false --ingress=false --rbac_enable=false \
	--cluster_wide ${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS}

e2e_dashboard_run:
	go test -v -timeout ${E2E_TIMEOUT} ./tests/e2e/tests/dashboard -args ${E2E_ARGS} ${EXTRA_E2E_ARGS} -use_galley_config_validator -cluster_wide

e2e_bookinfo_run:
	go test -v -timeout ${E2E_TIMEOUT} ./tests/e2e/tests/bookinfo -args ${E2E_ARGS} ${EXTRA_E2E_ARGS}

e2e_stackdriver_run:
	go test -v -timeout ${E2E_TIMEOUT} ./tests/e2e/tests/stackdriver -args ${E2E_ARGS} ${EXTRA_E2E_ARGS} --cluster_wide --gcp_proj=${GCP_PROJ} --sa_cred=/etc/service-account/service-account.json

test/local/auth/e2e_simple: generate_e2e_yaml
	go test -v -timeout ${E2E_TIMEOUT} ./tests/e2e/tests/simple -args --auth_enable=true \
	--egress=false --ingress=false \
	--rbac_enable=false --use_local_cluster --cluster_wide ${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS}

test/local/noauth/e2e_simple: generate_e2e_yaml
	go test -v -timeout ${E2E_TIMEOUT} ./tests/e2e/tests/simple -args --auth_enable=false \
	--egress=false --ingress=false \
	--rbac_enable=false --use_local_cluster --cluster_wide ${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS}

test/local/e2e_mixer: generate_e2e_yaml
	go test -v -timeout ${E2E_TIMEOUT} ./tests/e2e/tests/mixer \
	--auth_enable=false --egress=false --ingress=false --rbac_enable=false \
	--cluster_wide ${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS}

# v1alpha3+envoyv2 test without MTLS
test/local/noauth/e2e_pilotv2: generate_e2e_yaml
	go test -v -timeout ${E2E_TIMEOUT} ./tests/e2e/tests/pilot \
		--auth_enable=false --ingress=false --rbac_enable=true --cluster_wide \
		${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS}
	# Run the pilot controller tests
	go test -v -timeout ${E2E_TIMEOUT} ./tests/e2e/tests/controller

# v1alpha3+envoyv2 test with MTLS
test/local/auth/e2e_pilotv2: generate_e2e_yaml
	go test -v -timeout ${E2E_TIMEOUT} ./tests/e2e/tests/pilot \
		--auth_enable=true --ingress=false --rbac_enable=true --cluster_wide \
		${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS}
	# Run the pilot controller tests
	go test -v -timeout ${E2E_TIMEOUT} ./tests/e2e/tests/controller

test/local/auth/e2e_bookinfo_envoyv2: generate_e2e_yaml
	go test -v -timeout ${E2E_TIMEOUT} ./tests/e2e/tests/bookinfo \
		--auth_enable=true --egress=true --ingress=false --rbac_enable=false \
		--cluster_wide ${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS}

test/local/auth/e2e_bookinfo_trustdomain: generate_e2e_yaml
	go test -v -timeout ${E2E_TIMEOUT} ./tests/e2e/tests/bookinfo \
		--auth_enable=true --trust_domain_enable --egress=true --ingress=false --rbac_enable=false \
		--cluster_wide ${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS}

.PHONY: generate_e2e_yaml
generate_e2e_yaml: $(e2e_files)

# Create yaml files for e2e tests. Applies values-e2e.yaml, then values-$filename.yaml
$(e2e_files): $(HOME)/.helm istio-init.yaml
	cat install/kubernetes/namespace.yaml > install/kubernetes/$@
	cat install/kubernetes/helm/istio-init/files/crd-* >> install/kubernetes/$@
	$(HELM) template \
		--name=istio \
		--namespace=istio-system \
		--set-string global.tag=${TAG_VARIANT} \
		--set-string global.hub=${HUB} \
		--set-string global.imagePullPolicy=$(PULL_POLICY) \
		--set istio_cni.enabled=${ENABLE_ISTIO_CNI} \
		${EXTRA_HELM_SETTINGS} \
		--values install/kubernetes/helm/istio/test-values/values-e2e.yaml \
		--values install/kubernetes/helm/istio/test-values/values-$@ \
		install/kubernetes/helm/istio >> install/kubernetes/$@


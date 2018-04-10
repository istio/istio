# Test-specific targets, included from top Makefile
ifeq (${TEST_ENV},minikube)

# In minikube env we don't need to push the images to dockerhub or gcr, it is all local,
# but we need to use the minikube's docker env.
# Note that tests simply use go/out, so going up 3 dirs from the os/arch/debug path
export KUBECONFIG=${GO_TOP}/out/minikube.conf
export TEST_ENV=minikube
MINIKUBE_FLAGS=--use_local_cluster --cluster_wide
.PHONY: minikube

# Prepare minikube
minikube:
	minikube update-context
	@echo "Minikube started ${KUBECONFIG}"
	minikube docker-env > ${ISTIO_OUT}/minikube.dockerenv

e2e_docker: minikube docker

else ifeq (${TEST_ENV},minikube-none)

# In minikube env we don't need to push the images to dockerhub or gcr, it is all local,
# but we need to use the minikube's docker env.
# Note that tests simply use go/out, so going up 3 dirs from the os/arch/debug path
export KUBECONFIG=${GO_TOP}/out/minikube.conf
export TEST_ENV=minikube-none
MINIKUBE_FLAGS=--use_local_cluster --cluster_wide
.PHONY: minikube

# Prepare minikube
minikube:
	minikube update-context
	@echo "Minikube started ${KUBECONFIG}"

e2e_docker: minikube docker


else

# All other test environments require the docker images to be pushed to a repo.
# The HUB is defined in user-specific .istiorc, TAG can be set or defaults to git version
e2e_docker: push

endif

# If set outside, it appears it is not possible to modify the variable.
E2E_ARGS ?=

DEFAULT_EXTRA_E2E_ARGS = ${MINIKUBE_FLAGS}
DEFAULT_EXTRA_E2E_ARGS += --istioctl=${ISTIO_OUT}/istioctl
DEFAULT_EXTRA_E2E_ARGS += --mixer_tag=${TAG}
DEFAULT_EXTRA_E2E_ARGS += --pilot_tag=${TAG}
DEFAULT_EXTRA_E2E_ARGS += --proxy_tag=${TAG}
DEFAULT_EXTRA_E2E_ARGS += --ca_tag=${TAG}
DEFAULT_EXTRA_E2E_ARGS += --mixer_hub=${HUB}
DEFAULT_EXTRA_E2E_ARGS += --pilot_hub=${HUB}
DEFAULT_EXTRA_E2E_ARGS += --proxy_hub=${HUB}
DEFAULT_EXTRA_E2E_ARGS += --ca_hub=${HUB}

EXTRA_E2E_ARGS ?= ${DEFAULT_EXTRA_E2E_ARGS}

# These arguments are only needed by upgrade test.
DEFAULT_UPGRADE_E2E_ARGS =
LAST_RELEASE := $(shell curl -L -s https://api.github.com/repos/istio/istio/releases/latest \
	| grep tag_name | sed "s/ *\"tag_name\": *\"\(.*\)\",*/\1/")
DEFAULT_UPGRADE_E2E_ARGS += --base_version=${LAST_RELEASE}
DEFAULT_UPGRADE_E2E_ARGS += --target_version=""
UPGRADE_E2E_ARGS ?= ${DEFAULT_UPGRADE_E2E_ARGS}

# Simple e2e test using fortio, approx 2 min
e2e_simple: istioctl generate_yaml
	go test -v -timeout 20m ./tests/e2e/tests/simple -args ${E2E_ARGS} ${EXTRA_E2E_ARGS}

e2e_simple_run:
	go test -v -timeout 20m ./tests/e2e/tests/simple -args ${E2E_ARGS} ${EXTRA_E2E_ARGS}

e2e_mixer: istioctl generate_yaml
	go test -v -timeout 20m ./tests/e2e/tests/mixer -args ${E2E_ARGS} ${EXTRA_E2E_ARGS}

e2e_dashboard: istioctl generate_yaml
	go test -v -timeout 20m ./tests/e2e/tests/dashboard -args ${E2E_ARGS} ${EXTRA_E2E_ARGS}

e2e_bookinfo: istioctl generate_yaml
	go test -v -timeout 60m ./tests/e2e/tests/bookinfo -args ${E2E_ARGS} ${EXTRA_E2E_ARGS}

e2e_upgrade: istioctl generate_yaml
	go test -v -timeout 20m ./tests/e2e/tests/upgrade -args ${E2E_ARGS} ${EXTRA_E2E_ARGS} ${UPGRADE_E2E_ARGS}

e2e_version_skew: istioctl generate_yaml
	go test -v -timeout 20m ./tests/e2e/tests/upgrade -args --smooth_check=true ${E2E_ARGS} ${EXTRA_E2E_ARGS} ${UPGRADE_E2E_ARGS}

e2e_all:
	$(MAKE) --keep-going e2e_simple e2e_mixer e2e_bookinfo e2e_dashboard e2e_upgrade

JUNIT_E2E_XML ?= $(ISTIO_OUT)/junit_e2e-all.xml
e2e_all_junit_report: | $(JUNIT_REPORT)
	mkdir -p $(dir $(JUNIT_E2E_XML))
	set -o pipefail; $(MAKE) e2e_all 2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_E2E_XML))

# The pilot tests cannot currently be part of e2e_all, since they requires some additional flags.
e2e_pilot: istioctl generate_yaml
	go test -v -timeout 20m ./tests/e2e/tests/pilot ${E2E_ARGS} ${EXTRA_E2E_ARGS}

## Targets for fast local development and staged CI.
# The test take a T argument. Example:
# make test/local/noauth/e2e_pilotv2 T=-test.run=TestPilot/ingress

test/minikube/auth/e2e_simple: generate_yaml
	set -o pipefail; go test -v -timeout 20m ./tests/e2e/tests/simple -args --auth_enable=true \
	  --skip_cleanup  -use_local_cluster -cluster_wide \
	  ${E2E_ARGS} ${EXTRA_E2E_ARGS}  ${T}\
           ${TESTOPTS} | tee ${OUT_DIR}/tests/test-report-auth-simple.raw

test/minikube/noauth/e2e_simple: generate_yaml
	mkdir -p ${OUT_DIR}/tests
	set -o pipefail; go test -v -timeout 20m ./tests/e2e/tests/simple -args --auth_enable=false \
	  --skip_cleanup  -use_local_cluster -cluster_wide -test.v \
	  ${E2E_ARGS} ${EXTRA_E2E_ARGS} ${T} \
           ${TESTOPTS} | tee ${OUT_DIR}/tests/test-report-noauth-simple.raw

# v1alpha1+envoy v1 test with MTLS
# Test runs in istio-system, using istio-auth.yaml generated config.
# This will only (re)run the test - call "make docker istio.yaml" (or "make pilot docker.pilot" if
# you only changed pilot) to build.
# Note: This test is used by CircleCI as "e2e-pilot".
test/local/auth/e2e_pilot: generate_yaml
	@mkdir -p ${OUT_DIR}/logs
	set -o pipefail; go test -v -timeout 20m ./tests/e2e/tests/pilot \
 	--skip_cleanup --auth_enable=true --egress=false --v1alpha3=false \
	${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS} \
		| tee ${OUT_DIR}/logs/test-report.raw

# v1alpha3+envoyv2 test without MTLS
test/local/noauth/e2e_pilotv2: generate_yaml-envoyv2_transition
	@mkdir -p ${OUT_DIR}/logs
	set -o pipefail; ISTIO_PROXY_IMAGE=proxyv2 go test -v -timeout 20m ./tests/e2e/tests/pilot \
 	--skip_cleanup --auth_enable=false --v1alpha3=true --egress=false --ingress=false --rbac_enable=false --v1alpha1=false --cluster_wide \
	${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS} \
		| tee ${OUT_DIR}/logs/test-report.raw

# v1alpha3+envoyv2 test without MTLS (not implemented yet). Still in progress, for tracking
test/local/noauth/e2e_simple_pilotv2: generate_yaml-envoyv2_transition
	@mkdir -p ${OUT_DIR}/logs
	set -o pipefail; ISTIO_PROXY_IMAGE=proxyv2 go test -v -timeout 20m ./tests/e2e/tests/simple \
	--skip_cleanup --auth_enable=false \
    ${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS} \
		| tee ${OUT_DIR}/logs/test-report.raw

# Dumpsys will get as much info as possible from the test cluster
# Can be run after tests. It will also process the auto-saved log output
# This assume istio runs in istio-system namespace, and 'skip-cleanup' was used in tests.
dumpsys:
	@mkdir -p ${OUT_DIR}/tests
	@mkdir -p ${OUT_DIR}/logs
	kubectl get all -o wide --all-namespaces
	kubectl cluster-info dump > ${OUT_DIR}/logs/cluster-info.dump.txt
	kubectl describe pods -n istio-system > ${OUT_DIR}/logs/pods-system.txt
	kubectl logs -n istio-system -listio=pilot -c discovery
	$(JUNIT_REPORT) <${OUT_DIR}/logs/test-report.raw >${OUT_DIR}/tests/test-report.xml

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
DEFAULT_EXTRA_E2E_ARGS += --galley_tag=${TAG}
DEFAULT_EXTRA_E2E_ARGS += --mixer_hub=${HUB}
DEFAULT_EXTRA_E2E_ARGS += --pilot_hub=${HUB}
DEFAULT_EXTRA_E2E_ARGS += --proxy_hub=${HUB}
DEFAULT_EXTRA_E2E_ARGS += --ca_hub=${HUB}
DEFAULT_EXTRA_E2E_ARGS += --galley_hub=${HUB}

EXTRA_E2E_ARGS ?= ${DEFAULT_EXTRA_E2E_ARGS}

# These arguments are only needed by upgrade test.
DEFAULT_UPGRADE_E2E_ARGS =
LAST_RELEASE := $(shell curl -L -s https://api.github.com/repos/istio/istio/releases/latest \
	| grep tag_name | sed "s/ *\"tag_name\": *\"\(.*\)\",*/\1/")
DEFAULT_UPGRADE_E2E_ARGS += --base_version=${LAST_RELEASE}
DEFAULT_UPGRADE_E2E_ARGS += --target_version=""
UPGRADE_E2E_ARGS ?= ${DEFAULT_UPGRADE_E2E_ARGS}

e2e_simple: istioctl generate_yaml e2e_simple_run
e2e_simple_auth: istioctl generate_yaml e2e_simple_auth_run
e2e_simple_noauth: istioctl generate_yaml e2e_simple_noauth_run

e2e_mixer: istioctl generate_yaml e2e_mixer_run

e2e_galley: istioctl generate_yaml e2e_galley_run

e2e_dashboard: istioctl generate_yaml e2e_dashboard_run

e2e_bookinfo: istioctl generate_yaml e2e_bookinfo_run

e2e_upgrade: istioctl generate_yaml e2e_upgrade_run

e2e_version_skew: istioctl generate_yaml e2e_version_skew_run

e2e_all: istioctl generate_yaml e2e_all_run

# *_run targets do not rebuild the artifacts and test with whatever is given
e2e_simple_run: e2e_simple_auth_run

e2e_simple_auth_run: out_dir
	set -o pipefail; go test -v -timeout 25m ./tests/e2e/tests/simple -args --auth_enable=true \
	--egress=false --ingress=false \
	--rbac_enable=false --cluster_wide ${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS} ${CAPTURE_LOG}

e2e_simple_noauth_run: out_dir
	set -o pipefail; go test -v -timeout 25m ./tests/e2e/tests/simple -args --auth_enable=false \
	--egress=false --ingress=false \
	--rbac_enable=false --cluster_wide ${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS} ${CAPTURE_LOG}

e2e_mixer_run: out_dir
	set -o pipefail; go test -v -timeout 25m ./tests/e2e/tests/mixer \
	--auth_enable=false --egress=false --ingress=false --rbac_enable=false \
	--cluster_wide ${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS} ${CAPTURE_LOG}

e2e_galley_run: out_dir
	go test -v -timeout 25m ./tests/e2e/tests/galley -args ${E2E_ARGS} ${EXTRA_E2E_ARGS} -use_galley_config_validator -cluster_wide

e2e_dashboard_run: out_dir
	go test -v -timeout 25m ./tests/e2e/tests/dashboard -args ${E2E_ARGS} ${EXTRA_E2E_ARGS}

e2e_bookinfo_run: out_dir
	go test -v -timeout 60m ./tests/e2e/tests/bookinfo -args ${E2E_ARGS} ${EXTRA_E2E_ARGS}

e2e_upgrade_run: out_dir
	go test -v -timeout 25m ./tests/e2e/tests/upgrade -args ${E2E_ARGS} ${EXTRA_E2E_ARGS} ${UPGRADE_E2E_ARGS}

e2e_version_skew_run: out_dir
	go test -v -timeout 25m ./tests/e2e/tests/upgrade -args --smooth_check=true ${E2E_ARGS} ${EXTRA_E2E_ARGS} ${UPGRADE_E2E_ARGS}

e2e_all_run: out_dir
	$(MAKE) --keep-going e2e_simple_run e2e_bookinfo_run e2e_dashboard_run e2e_upgrade_run e2e_version_skew_run

JUNIT_E2E_XML ?= $(ISTIO_OUT)/junit.xml
TARGET ?= e2e_all
with_junit_report: | $(JUNIT_REPORT)
	mkdir -p $(dir $(JUNIT_E2E_XML))
	set -o pipefail; $(MAKE) $(TARGET) 2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_E2E_XML))

e2e_all_junit_report:
	$(MAKE) with_junit_report TARGET=e2e_all

e2e_all_run_junit_report:
	$(MAKE) with_junit_report TARGET=e2e_all_run

# The pilot tests cannot currently be part of e2e_all, since they requires some additional flags.
e2e_pilot: out_dir istioctl generate_yaml
	go test -v -timeout 25m ./tests/e2e/tests/pilot ${E2E_ARGS} ${EXTRA_E2E_ARGS}

e2e_pilotv2_v1alpha3: | istioctl test/local/noauth/e2e_pilotv2

e2e_bookinfo_envoyv2_v1alpha3: | istioctl test/local/auth/e2e_bookinfo_envoyv2

# This is used to keep a record of the test results.
CAPTURE_LOG=| tee -a ${OUT_DIR}/tests/build-log.txt

## Targets for fast local development and staged CI.
# The test take a T argument. Example:
# make test/local/noauth/e2e_pilotv2 T=-test.run=TestPilot/ingress


.PHONY: out_dir
out_dir:
	@mkdir -p ${OUT_DIR}/{logs,tests}

test/local/auth/e2e_simple: out_dir generate_yaml
	set -o pipefail; go test -v -timeout 25m ./tests/e2e/tests/simple -args --auth_enable=true \
	--egress=false --ingress=false \
	--rbac_enable=false --use_local_cluster --cluster_wide ${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS} ${CAPTURE_LOG}

test/local/noauth/e2e_simple: out_dir generate_yaml
	set -o pipefail; go test -v -timeout 25m ./tests/e2e/tests/simple -args --auth_enable=false \
	--egress=false --ingress=false \
	--rbac_enable=false --use_local_cluster --cluster_wide ${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS} ${CAPTURE_LOG}

test/local/e2e_mixer: out_dir generate_yaml
	set -o pipefail; go test -v -timeout 25m ./tests/e2e/tests/mixer \
	--auth_enable=false --egress=false --ingress=false --rbac_enable=false \
	--cluster_wide ${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS} ${CAPTURE_LOG}

test/local/e2e_galley: out_dir istioctl generate_yaml
	set -o pipefail; go test -v -timeout 25m ./tests/e2e/tests/galley -args \
	-use_local_cluster -cluster_wide --use_galley_config_validator -test.v ${E2E_ARGS} ${EXTRA_E2E_ARGS} \
	${CAPTURE_LOG}

# v1alpha3+envoyv2 test without MTLS
test/local/noauth/e2e_pilotv2: out_dir generate_yaml
	set -o pipefail; go test -v -timeout 25m ./tests/e2e/tests/pilot \
		--auth_enable=false --egress=false --ingress=false --rbac_enable=true --cluster_wide \
		${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS} ${CAPTURE_LOG}
	# Run the pilot controller tests
	set -o pipefail; go test -v -timeout 25m ./tests/e2e/tests/controller ${CAPTURE_LOG}

# v1alpha3+envoyv2 test with MTLS
test/local/auth/e2e_pilotv2: out_dir generate_yaml
	set -o pipefail; go test -v -timeout 25m ./tests/e2e/tests/pilot \
		--auth_enable=true --egress=false --ingress=false --rbac_enable=true --cluster_wide \
		${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS} ${CAPTURE_LOG}
	# Run the pilot controller tests
	set -o pipefail; go test -v -timeout 25m ./tests/e2e/tests/controller ${CAPTURE_LOG}

test/local/cloudfoundry/e2e_pilotv2: out_dir
	sudo apt update
	sudo apt install -y iptables
	sudo iptables -t nat -A OUTPUT -d 127.1.1.1/32 -p tcp -j REDIRECT --to-port 15001
	set -o pipefail; go test -v -timeout 25m ./tests/e2e/tests/pilot/cloudfoundry ${T} \
		${CAPTURE_LOG}
	sudo iptables -t nat -F

test/local/auth/e2e_bookinfo_envoyv2: out_dir generate_yaml
	set -o pipefail; go test -v -timeout 25m ./tests/e2e/tests/bookinfo \
		--auth_enable=true --egress=true --ingress=false --rbac_enable=false \
		--cluster_wide ${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS} ${CAPTURE_LOG}

test/local/noauth/e2e_mixer_envoyv2: out_dir generate_yaml
	set -o pipefail; go test -v -timeout 25m ./tests/e2e/tests/mixer \
	--auth_enable=false --egress=false --ingress=false --rbac_enable=false \
	--cluster_wide ${E2E_ARGS} ${T} ${EXTRA_E2E_ARGS} ${CAPTURE_LOG}

junit-report: out_dir ${ISTIO_BIN}/go-junit-report
	${ISTIO_BIN}/go-junit-report < $(OUT_DIR)/tests/build-log.txt > $(OUT_DIR)/tests/junit.xml

# Dumpsys will get as much info as possible from the test cluster
# Can be run after tests. It will also process the auto-saved log output
# This assume istio runs in istio-system namespace, and 'skip-cleanup' was used in tests.
dumpsys: out_dir
	tools/dump_kubernetes.sh --output-directory ${OUT_DIR}/logs --error-if-nasty-logs

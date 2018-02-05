# Test-specific targets, included from top Makefile
ifeq (${TEST_ENV},minikube)

# In minikube env we don't need to push the images to dockerhub or gcr, it is all local,
# but we need to use the minikube's docker env.
# Note that tests simply use go/out, so going up 3 dirs from the os/arch/debug path
export KUBECONFIG=${GO_TOP}/out/minikube.conf
export TEST_ENV=minikube
MINIKUBE_FLAGS=-use_local_cluster -cluster_wide
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
MINIKUBE_FLAGS=-use_local_cluster -cluster_wide
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

EXTRA_E2E_ARGS = ${MINIKUBE_FLAGS}
EXTRA_E2E_ARGS += --istioctl ${ISTIO_OUT}/istioctl
EXTRA_E2E_ARGS += --mixer_tag ${TAG}
EXTRA_E2E_ARGS += --pilot_tag ${TAG}
EXTRA_E2E_ARGS += --proxy_tag ${TAG}
EXTRA_E2E_ARGS += --ca_tag ${TAG}
EXTRA_E2E_ARGS += --mixer_hub ${HUB}
EXTRA_E2E_ARGS += --pilot_hub ${HUB}
EXTRA_E2E_ARGS += --proxy_hub ${HUB}
EXTRA_E2E_ARGS += --ca_hub ${HUB}

# A make target to generate all the YAML files
generate_yaml:
	./install/updateVersion.sh >/dev/null 2>&1

# Simple e2e test using fortio, approx 2 min
e2e_simple: istioctl generate_yaml
	go test -v -timeout 20m ./tests/e2e/tests/simple -args ${E2E_ARGS} ${EXTRA_E2E_ARGS}

e2e_mixer: istioctl generate_yaml
	go test -v -timeout 20m ./tests/e2e/tests/mixer -args ${E2E_ARGS} ${EXTRA_E2E_ARGS}

e2e_bookinfo: istioctl generate_yaml
	go test -v -timeout 20m ./tests/e2e/tests/bookinfo -args ${E2E_ARGS} ${EXTRA_E2E_ARGS}

e2e_upgrade: istioctl generate_yaml
	go test -v -timeout 20m ./tests/e2e/tests/upgrade -args ${E2E_ARGS} ${EXTRA_E2E_ARGS}

e2e_all: e2e_simple e2e_mixer e2e_bookinfo

e2e_pilot:
	go install ./pilot/test/integration
	${ISTIO_BIN}/integration --logtostderr ${TESTOPTS} -hub ${HUB} -tag ${TAG}

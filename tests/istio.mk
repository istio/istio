# Test-specific targets, included from top Makefile
ifeq (${TEST_ENV},minikube)

# In minikube env we don't need to push the images to dockerhub or gcr, it is all local,
# but we need to use the minikube's docker env.
export KUBECONFIG=${OUT}/minikube.conf
export TEST_ENV=minikube
MINIKUBE_FLAGS=-use_local_cluster -cluster_wide
.PHONY: minikube

# Prepare minikube
minikube:
	minikube update-context
	@echo "Minikube started ${KUBECONFIG}"
	minikube docker-env > ${OUT}/minikube.dockerenv

e2e_docker: minikube docker

else ifeq (${TEST_ENV},minikube-none)

# In minikube env we don't need to push the images to dockerhub or gcr, it is all local,
# but we need to use the minikube's docker env.
export KUBECONFIG=${OUT}/minikube.conf
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
e2e_docker: docker push

endif

E2E_ARGS ?=
E2E_ARGS += $(if ifeq($V,1),-v 2)
E2E_ARGS += ${MINIKUBE_FLAGS}
E2E_ARGS += --istioctl ${GOPATH}/bin/istioctl
E2E_ARGS += --mixer_tag ${TAG}
E2E_ARGS += --pilot_tag ${TAG}
E2E_ARGS += --ca_tag ${TAG}
E2E_ARGS += --mixer_hub ${HUB}
E2E_ARGS += --pilot_hub ${HUB}
E2E_ARGS += --ca_hub ${HUB}

# A make target to generate all the YAML files
generate_yaml:
	./install/updateVersion.sh >/dev/null 2>&1

# Run the e2e tests. Targets correspond to the prow environments/tests
# The tests take > 10 m
# This uses the script (deprecated ?), still used by prow.
# TODO: move prow to use 'make e2e' and remove old script
e2e: istioctl generate_yaml
	./tests/e2e.sh ${E2E_ARGS}

# Simple e2e test using fortio, approx 2 min
e2e_simple: istioctl generate_yaml
	./tests/e2e.sh -s simple ${E2E_ARGS}

e2e_mixer: istioctl generate_yaml
	./tests/e2e.sh -s mixer ${E2E_ARGS}

e2e_bookinfo: istioctl generate_yaml
	./tests/e2e.sh -s bookinfo ${E2E_ARGS}

e2e_all: e2e_simple e2e_mixer e2e_bookinfo

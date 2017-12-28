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
E2E_ARGS += $(if ifeq($V,1),-alsologtostderr -test.v -v 2)
E2E_ARGS += ${MINIKUBE_FLAGS}
E2E_ARGS += --istioctl ${GOPATH}/bin/istioctl


# Run the e2e tests. Targets correspond to the prow environments/tests
# The tests take > 10 m
# This uses the script (deprecated ?), still used by prow.
# TODO: move prow to use 'make e2e' and remove old script
e2e: istioctl
	./tests/e2e.sh ${E2E_ARGS} --istioctl ${GOPATH}/bin/istioctl --mixer_tag ${TAG} --pilot_tag ${TAG} --ca_tag ${TAG} \
		--mixer_hub ${HUB} --pilot_hub ${HUB} --ca_hub ${HUB}

# Simple e2e test using fortio, approx 2 min
e2e_simple: istioctl
	@echo "=== E2E testing with ${TAG} and ${HUB}"
	go test  -v ${TEST_ARGS:-} ./tests/e2e/tests/simple -args ${E2E_ARGS} --mixer_tag ${TAG} --pilot_tag ${TAG} --ca_tag ${TAG} \
             --mixer_hub ${HUB} --pilot_hub ${HUB} --ca_hub ${HUB}


e2e_mixer: istioctl
	go test  -v ${TEST_ARGS:-} ./tests/e2e/tests/mixer -args ${E2E_ARGS} --mixer_tag ${TAG} --pilot_tag ${TAG} --ca_tag ${TAG} \
                          --mixer_hub ${HUB} --pilot_hub ${HUB} --ca_hub ${HUB}

e2e_bookinfo: istioctl
	go test -v ${TEST_ARGS:-} ./tests/e2e/tests/bookinfo -args ${E2E_ARGS} --mixer_tag ${TAG} --pilot_tag ${TAG} --ca_tag ${TAG} \
                         --mixer_hub ${HUB} --pilot_hub ${HUB} --ca_hub ${HUB}

e2e_all: e2e_simple e2e_mixer e2e_bookinfo

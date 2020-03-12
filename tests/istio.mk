# tests/istio.mk defines test targets for Istio
helm3/test/install:
	helm3 install istio-base manifests/base
	helm3 install -n istio-system istio-16 manifests/istio-control/istio-discovery -f manifests/global.yaml
	helm3 install -n istio-system istio-canary manifests/istio-control/istio-discovery -f manifests/global.yaml  \
		--set revision=canary

helm3/test/upgrade:
	helm3 upgrade istio-base manifests/base
	helm3 upgrade -n istio-system istio-16 manifests/istio-control/istio-discovery -f manifests/global.yaml
	helm3 upgrade -n istio-system istio-canary manifests/istio-control/istio-discovery -f manifests/global.yaml  \
		--set revision=canary

helm3/test/uninstall:
	helm3 delete -n istio-system istio-16 || true
	helm3 delete -n istio-system istio-canary || true
	helm3 delete istio-base || true
	kubectl delete crd -l release=istio || true

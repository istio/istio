
# Will install or upgrade a 'default' and 'canary' revisions.
# The canary has DNS capture enabled.
helm3/test/upgrade:
	kubectl create ns istio-system || true
	helm3 template --include-crds istio-base manifests/charts/base | kubectl apply -f -
	helm3 upgrade -i -n istio-system istio-16 manifests/charts/istio-control/istio-discovery \
		--set global.tag=${TAG} --set global.hub=${HUB} \
		-f manifests/charts/global.yaml \
		--set meshConfig.enablePrometheusMerge=true


	helm3 upgrade -i -n istio-system istio-canary manifests/charts/istio-control/istio-discovery \
		-f manifests/charts/global.yaml  \
		--set global.tag=${TAG} --set global.hub=${HUB} \
        --set revision=canary \
		--set meshConfig.enablePrometheusMerge=true \
        --set meshConfig.defaultConfig.proxyMetadata.DNS_CAPTURE=ALL \
        --set meshConfig.defaultConfig.proxyMetadata.DNS_AGENT=DNS-TLS

helm3/test/uninstall:
	helm3 delete -n istio-system istio-16 || true
	helm3 delete -n istio-system istio-canary || true
	helm3 delete istio-base || true
	kubectl delete crd -l release=istio || true

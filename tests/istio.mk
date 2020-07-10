
# Will install or upgrade a 'master' and 'canary' revisions.
# Use the new style meshConfig: the config map will use defaults+explicit config,
# no global or values.yaml override
helm3/base:
	kubectl create ns istio-system || true
	helm3 template --include-crds istio-base manifests/charts/base | kubectl apply -f -

helm3/master:
	helm3 upgrade -i -n istio-system istio-16 manifests/charts/istio-control/istio-discovery \
		--set global.tag=${TAG} --set global.hub=${HUB} \
        --set revision=master \
		-f manifests/charts/global.yaml \
		--set meshConfig.defaultConfig.accessLogFile=/dev/stdout \
		--set meshConfig.enablePrometheusMerge=true \
		--set meshConfig.proxyHttpPort=15002

helm3/canary:
    # The canary has DNS capture enabled.
	helm3 upgrade -i -n istio-system istio-canary manifests/charts/istio-control/istio-discovery \
		-f manifests/charts/global.yaml  \
		--set global.tag=${TAG} --set global.hub=${HUB} \
        --set revision=canary \
		--set meshConfig.enablePrometheusMerge=true \
        --set meshConfig.defaultConfig.proxyMetadata.ISTIO_META_DNS_CAPTURE=ALL

# For 1.15:  HELM3_INGRESS_OPTS="--set gateways.istio-ingressgateway.runAsRoot=true"
HELM3_INGRESS_OPTS=""


helm3/ingress:
	helm3 upgrade -i -n istio-system istio-ingress manifests/charts/gateways/istio-ingress \
		-f manifests/charts/global.yaml  \
		--set global.tag=${TAG} --set global.hub=${HUB} \
		--set gateways.istio-ingressgateway.name=ingress1 \
        --set revision=master ${HELM3_INGRESS_OPTS}

helm3/install: helm3/base helm3/master helm3/canary helm3/ingress

# Attempt to upgrade in-place istio-system. Typically only works if it was
# installed with helm3. Will use empty revision - so injection without revision.
helm3/default:
	helm3 upgrade -i -n istio-system istio-default manifests/charts/istio-control/istio-discovery \
		--set global.tag=${TAG} --set global.hub=${HUB} \
		-f manifests/charts/global.yaml \
		--set global.proxy.accessLogFile=/dev/stdout \
		--set meshConfig.enablePrometheusMerge=true

# Same - using helm3 template. This works even if old version installed
helm3/default-template:
	helm3 template -n istio-system istio-default manifests/charts/istio-control/istio-discovery \
		--set global.tag=${TAG} --set global.hub=${HUB} \
		-f manifests/charts/global.yaml \
		--set global.proxy.accessLogFile=/dev/stdout \
		--set meshConfig.enablePrometheusMerge=true |kubectl apply -f -

helm3/uninstall:
	helm3 delete -n istio-system istio-16 || true
	helm3 delete -n istio-system istio-canary || true
	helm3 delete istio-base || true
	kubectl delete crd -l release=istio || true

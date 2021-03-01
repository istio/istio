# Example and tests for running Istiod on CloudRun or KNative
# This is an experimental mode, to evaluate the k8s dependencies

# Project used for testing the integration.
ISTIO_REGION ?= us-central1
ISTIO_PROJECT ?= wlhe-cr
ISTIO_SA ?= 601426346923-compute@developer.gserviceaccount.com
ZONE ?= us-central1-c
ISTIO_SUFFIX?=-icq63pqnqq-uc.a.run.app
# NOTE: The XDS_ADDR won't be correct if the service name (istiod-<project
# id>-<cluster>-<rev>) is too long and gets truncated.
XDS_ADDR=istiod-${PROJECT_ID}-${CLUSTER}-${REV}${ISTIO_SUFFIX}:443
CLOUDRUN_ADDR=${XDS_ADDR}

# Test project/cluster where workloads run.
PROJECT_ID ?= costin-asm1
CLUSTER ?= big1

CPU ?= 1
MEM ?= 2G

# Istiod revision to deploy
REV ?= oss

# Image tag
TAG ?= asm-cr

# Hub for the proxy
PROXY_HUB ?= grc.io/wlhe-cr

$(ISTIO_OUT)/knative/telemetry-sd.yaml: manifests/charts/istio-control/istio-discovery/templates/telemetryv2_1.9.yaml manifests/charts/istio-control/istio-discovery/templates/telemetryv2_1.10.yaml tools/packaging/knative/values.telemetry-sd.yaml
	mkdir -p $(ISTIO_OUT)/knative/templates
	cp manifests/charts/istio-control/istio-discovery/templates/telemetryv2_1.9.yaml ${ISTIO_OUT}/knative/templates
	cp manifests/charts/istio-control/istio-discovery/templates/telemetryv2_1.10.yaml ${ISTIO_OUT}/knative/templates
	cp manifests/charts/istio-control/istio-discovery/Chart.yaml ${ISTIO_OUT}/knative/Chart.yaml
	helm template $(ISTIO_OUT)/knative -f tools/packaging/knative/values.telemetry-sd.yaml > $@

$(ISTIO_OUT)/knative/telemetry.yaml: manifests/charts/istio-control/istio-discovery/templates/telemetryv2_1.9.yaml manifests/charts/istio-control/istio-discovery/templates/telemetryv2_1.10.yaml
	mkdir -p $(ISTIO_OUT)/knative/templates
	cp manifests/charts/istio-control/istio-discovery/templates/telemetryv2_1.9.yaml ${ISTIO_OUT}/knative/templates
	cp manifests/charts/istio-control/istio-discovery/templates/telemetryv2_1.10.yaml ${ISTIO_OUT}/knative/templates
	cp manifests/charts/istio-control/istio-discovery/Chart.yaml ${ISTIO_OUT}/knative/Chart.yaml
	helm template $(ISTIO_OUT)/knative -f tools/packaging/knative/values.telemetry.yaml > $@

$(ISTIO_OUT)/knative/injection-template.yaml: manifests/charts/istio-control/istio-discovery/files/injection-template.yaml
	mkdir -p $(ISTIO_OUT)/knative
	cp tools/packaging/knative/injection-template-extra.yaml ${ISTIO_OUT}/knative/injection-template.yaml
	echo -e "\n  sidecar: |-" >> ${ISTIO_OUT}/knative/injection-template.yaml
	cat manifests/charts/istio-control/istio-discovery/files/injection-template.yaml | sed 's/^/    /g' >> ${ISTIO_OUT}/knative/injection-template.yaml

$(ISTIO_OUT)/knative/mutatingwebhook.yaml: manifests/charts/istio-control/istio-discovery/templates/mutatingwebhook.yaml
	mkdir -p $(ISTIO_OUT)/knative-wh/templates
	cp manifests/charts/istio-control/istio-discovery/templates/mutatingwebhook.yaml ${ISTIO_OUT}/knative-wh/templates
	cp manifests/charts/istio-control/istio-discovery/Chart.yaml ${ISTIO_OUT}/knative-wh/Chart.yaml
	helm template istio $(ISTIO_OUT)/knative-wh -n istio-system -f tools/packaging/knative/values.webhook.yaml | sed 's/name: istio-sidecar-injector-/name: istiod-/g' > $@

gen-gateway:
	helm template istio-asm manifests/charts/gateways/istio-ingress \
 		-n istio-system \
 		-f tools/packaging/knative/values.gateway.yaml \
 		--set global.hub="${HUB}"  \
 		--set global.tag="${TAG}"  \
 	    > tools/packaging/knative/gateway/gen-gateway.yaml


# Setup the CloudRun Istiod revision
knative/setup:
	#gcloud config unset api_endpoint_overrides/run
	# Slow start - but pinned
	# Alternative: --cpu 4 --memory 4G
	gcloud alpha run deploy istiod-${PROJECT_ID}-${CLUSTER}-${REV} --allow-unauthenticated \
	 --project ${ISTIO_PROJECT} --region ${ISTIO_REGION} --platform managed \
     --image gcr.io/${ISTIO_PROJECT}/cloudrun:${TAG} \
     --set-env-vars=${ENVEXTRA}REV=${REV},TAG=${TAG},CLUSTER=${CLUSTER},ZONE=${ZONE},PROJECT=${PROJECT_ID},XDS_ADDR=${XDS_ADDR} \
	 --port 8080 --concurrency 1000 --timeout 900 --cpu ${CPU} --memory ${MEM}

# Same, but with stackdriver and CA integrated
knative/setup_asm:
	REV=asm ENVEXTRA="ASM=1," $(MAKE) knative/setup

#!/bin/bash -xe

# Copyright 2018 Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

cd  "$( dirname "${BASH_SOURCE[0]}" )/.."
export IBASE="$(pwd)"
export WAIT_TIMEOUT=${WAIT_TIMEOUT:-5m}

# Install istio crds

function install_crds() {
    echo "${METHOD}ing custom ressource defintions"
    kubectl apply -f crds.yaml
    
    kubectl wait --for=condition=Established -f crds.yaml
}

# Cleanup all namespaces

function cleanup() {
    for namespace in istio-system  ${ISTIO_CONTROL_NS} istio-control istio-control-master istio-ingress istio-telemetry; do
        kubectl delete namespace $namespace --wait --ignore-not-found
    done

}

# Install citadel into namespace istio-system

function install_system() {
    echo "${METHOD}ing citadel.."
    bin/iop istio-system istio-system-security $IBASE/security/citadel/ $RESOURCES_FLAGS
    
    kubectl rollout status  deployment istio-citadel11 -n istio-system --timeout=$WAIT_TIMEOUT
}

# Install config, discovery and sidecar-injector into namespace istio-control

function install_control() {
    kubectl delete namespace ${ISTIO_CONTROL_NS} --wait --ignore-not-found
    echo "${METHOD}ing galley.."
    bin/iop ${ISTIO_CONTROL_NS} istio-config $IBASE/istio-control/istio-config --set configValidation=true --set global.istioNamespace=${ISTIO_CONTROL_NS} $RESOURCES_FLAGS
    echo "${METHOD}ing pilot.."
    bin/iop ${ISTIO_CONTROL_NS} istio-discovery $IBASE/istio-control/istio-discovery  --set global.istioNamespace=${ISTIO_CONTROL_NS} --set global.configNamespace=${ISTIO_CONTROL_NS} $RESOURCES_FLAGS 
    echo "${METHOD}ing auto-injector.."
    bin/iop ${ISTIO_CONTROL_NS} istio-autoinject $IBASE/istio-control/istio-autoinject --set global.istioNamespace=${ISTIO_CONTROL_NS} --set global.istioNamespace=${ISTIO_CONTROL_NS} $RESOURCES_FLAGS

    kubectl rollout status  deployment istio-galley -n ${ISTIO_CONTROL_NS} --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment istio-pilot  -n ${ISTIO_CONTROL_NS} --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment istio-sidecar-injector -n ${ISTIO_CONTROL_NS} --timeout=$WAIT_TIMEOUT
}

# Install discovery and ingress into namespace istio-ingress

function install_ingress() {
    echo "${METHOD}ing pilot.."
    bin/iop istio-ingress istio-discovery $IBASE/istio-control/istio-discovery  --set global.istioNamespace=${ISTIO_CONTROL_NS} --set global.configNamespace=${ISTIO_CONTROL_NS} $RESOURCES_FLAGS
    echo "${METHOD}ing ingress.."
    bin/iop istio-ingress istio-ingress $IBASE/gateways/istio-ingress  --set global.istioNamespace=${ISTIO_CONTROL_NS} $RESOURCES_FLAGS

    kubectl patch deployment -n istio-ingress ingressgateway --patch '{"spec": {"strategy": {"rollingUpdate": {"maxSurge": 1,"maxUnavailable": 0},"type": "RollingUpdate"}}}'
    kubectl rollout status  deployment istio-pilot -n istio-ingress --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment ingressgateway -n istio-ingress --timeout=$WAIT_TIMEOUT
}

# Install grafana, mixer and prometheus into namespace istio-telemetry

function install_telemetry() {
    echo "${METHOD}ing istio-grafana.."
    bin/iop istio-telemetry istio-grafana $IBASE/istio-telemetry/grafana/ --set global.istioNamespace=${ISTIO_CONTROL_NS} $RESOURCES_FLAGS
    echo "${METHOD}ing istio-mixer.."
    bin/iop istio-telemetry istio-mixer $IBASE/istio-telemetry/mixer-telemetry/ --set global.istioNamespace=${ISTIO_CONTROL_NS} $RESOURCES_FLAGS
    echo "${METHOD}ling istio-prometheus."
    bin/iop istio-telemetry istio-prometheus $IBASE/istio-telemetry/prometheus/ --set global.istioNamespace=${ISTIO_CONTROL_NS} $RESOURCES_FLAGS

    kubectl rollout status  deployment grafana -n istio-telemetry --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment istio-telemetry -n istio-telemetry --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment prometheus -n istio-telemetry --timeout=$WAIT_TIMEOUT
}

# Switch to other istio-control-namespace
function switch_istio_control() {
    if [ "$ISTIO_CONTROL_NS" != "$ISTIO_CONTROL_OLD" ]; then
        ACTIVE_NAMESPACES=$(kubectl get namespaces --no-headers -l istio-env=${ISTIO_CONTROL_OLD} -o=custom-columns=NAME:.metadata.name)
        for ns in $ACTIVE_NAMESPACES; do
            kubectl label namespaces ${ns} --overwrite istio-env=${ISTIO_CONTROL_NS}
        done
        if [ $REMOVE_OLD_CONTROL = true ]; then
            kubectl delete namespace $ISTIO_CONTROL_OLD --wait --ignore-not-found
        fi
        for ns in $ACTIVE_NAMESPACES; do
            kubectl set env --all deployment --env="LAST_MANUAL_RESTART=$(date +%s)" --namespace=$ns
        done
    fi
}

COMMAND="install_all"
METHOD=Install
ISTIO_CONTROL_OLD=$(kubectl get namespaces -o=jsonpath='{$.items[:1].metadata.labels.istio-env}' -l istio-env)
ISTIO_CONTROL_OLD=${ISTIO_CONTROL_OLD:-istio-control}
REMOVE_OLD_CONTROL=false

while [ $# -gt 0 ]
do
    case "$1" in
        --update)
            METHOD=Update
            ;;
        --remove-old-control)
            REMOVE_OLD_CONTROL=true
            ;;
        cleanup) COMMAND=$1 ;;
        install_crds) COMMAND=$1 ;;
        install_system) COMMAND=$1 ;;
        install_control) COMMAND=$1 ;;
        install_ingress) COMMAND=$1 ;;
        install_telemetry) COMMAND=$1 ;;
        switch_istio_control) COMMAND=$1 ;;
    esac
    shift 1
done

if [ "$METHOD" = Update ]; then
  case "$ISTIO_CONTROL_OLD" in
    *-master) ISTIO_CONTROL_NS=istio-control ;;
    *) ISTIO_CONTROL_NS=istio-control-master ;;
  esac
else
  ISTIO_CONTROL_NS=${ISTIO_CONTROL_OLD}
fi

case "$COMMAND" in
    cleanup) cleanup ;;
    install_crds) install_crds ;;
    install_system) install_system ;;
    install_control) install_control ;;
    install_ingress) install_ingress ;;
    install_telemetry) install_telemetry ;;
    switch_istio_control) switch_istio_control ;;
    install_all) install_crds &&  install_system && install_control && install_ingress && install_telemetry;;
esac

# Temporarily disabled - fails in circle:  && switch_istio_control

echo "Finished"

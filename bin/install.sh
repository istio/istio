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

# Install citadel into namespace istio-system

function install_system() {
    if [ "$METHOD" = Install ]; then
        kubectl delete namespace istio-system --wait --ignore-not-found
    fi
    echo "${METHOD}ing citadel.."
    bin/iop istio-system istio-system-security $IBASE/security/citadel/ $RESOURCES_FLAGS
    
    kubectl get deployments -n istio-system
    kubectl wait deployments istio-citadel11 -n istio-system --for=condition=available --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment istio-citadel11 -n istio-system --timeout=$WAIT_TIMEOUT
    kubectl get deployments -n istio-system
    kubectl get pod -n istio-system
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

    kubectl get deployments -n ${ISTIO_CONTROL_NS}
    kubectl wait deployments istio-galley istio-pilot istio-sidecar-injector -n ${ISTIO_CONTROL_NS} --for=condition=available --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment istio-galley -n ${ISTIO_CONTROL_NS} --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment istio-pilot  -n ${ISTIO_CONTROL_NS} --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment istio-sidecar-injector -n ${ISTIO_CONTROL_NS} --timeout=$WAIT_TIMEOUT

    kubectl get deployments -n ${ISTIO_CONTROL_NS}
    kubectl get pod -n ${ISTIO_CONTROL_NS}
}

# Install discovery and ingress into namespace istio-ingress

function install_ingress() {
    if [ "$METHOD" = Install ]; then
        kubectl delete namespace istio-ingress --wait --ignore-not-found
    else
        kubectl patch deployment -n istio-ingress ingressgateway --patch '{"spec": {"strategy": {"rollingUpdate": {"maxSurge": 1,"maxUnavailable": 0},"type": "RollingUpdate"}}}'
    fi
    echo "${METHOD}ing pilot.."
    bin/iop istio-ingress istio-discovery $IBASE/istio-control/istio-discovery  --set global.istioNamespace=${ISTIO_CONTROL_NS} --set global.configNamespace=${ISTIO_CONTROL_NS} $RESOURCES_FLAGS
    echo "${METHOD}ing ingress.."
    bin/iop istio-ingress istio-ingress $IBASE/gateways/istio-ingress  --set global.istioNamespace=${ISTIO_CONTROL_NS} $RESOURCES_FLAGS

    kubectl get deployments -n istio-ingress
    kubectl wait deployments ingressgateway  istio-pilot -n istio-ingress --for=condition=available --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment ingressgateway -n istio-ingress --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment  istio-pilot -n istio-ingress --timeout=$WAIT_TIMEOUT
    kubectl get deployments -n istio-ingress
    kubectl get pod -n istio-ingress
}

# Install grafana, mixer and prometheus into namespace istio-telemetry

function install_telemetry() {
    if [ "$METHOD" = Install ]; then
        kubectl delete namespace istio-telemetry --wait --ignore-not-found
    fi
    echo "${METHOD}ing istio-grafana.."
    bin/iop istio-telemetry istio-grafana $IBASE/istio-telemetry/grafana/ --set global.istioNamespace=${ISTIO_CONTROL_NS} $RESOURCES_FLAGS
    echo "${METHOD}ing istio-mixer.."
    bin/iop istio-telemetry istio-mixer $IBASE/istio-telemetry/mixer-telemetry/ --set global.istioNamespace=${ISTIO_CONTROL_NS} $RESOURCES_FLAGS
    echo "${METHOD}ling istio-prometheus."
    bin/iop istio-telemetry istio-prometheus $IBASE/istio-telemetry/prometheus/ --set global.istioNamespace=${ISTIO_CONTROL_NS} $RESOURCES_FLAGS
    kubectl get deployments -n istio-telemetry
    kubectl wait deployments grafana istio-telemetry prometheus -n istio-telemetry --for=condition=available --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment grafana -n istio-telemetry --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment istio-telemetry -n istio-telemetry --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment prometheus -n istio-telemetry --timeout=$WAIT_TIMEOUT
    kubectl get deployments -n istio-telemetry
    kubectl get pod -n istio-telemetry
}

# Switch to other istio-control-namespace

function switch_istio_control() {
    if [ "$ISTIO_CONTROL_NS" != "$ISTIO_CONTROL_OLD" ]; then
        for ns in $(kubectl get namespaces --no-headers -l istio-env=${ISTIO_CONTROL_OLD} -o=custom-columns=NAME:.metadata.name); do        kubectl label namespaces default --overwrite istio-env=${ISTIO_CONTROL_NS}
            kubectl label namespaces ${ns} --overwrite istio-env=${ISTIO_CONTROL_NS}
        done
        kubectl set env --all deployment --env="LAST_MANUAL_RESTART=$(date +%s)" --namespace=default
        kubectl delete namespace $ISTIO_CONTROL_OLD --wait --ignore-not-found
    fi
}

COMMAND="install_all"
METHOD=Install
ISTIO_CONTROL_OLD=$(kubectl get namespaces -o=jsonpath='{$.items[:1].metadata.labels.istio-env}' -l istio-env)
ISTIO_CONTROL_OLD=${ISTIO_CONTROL_OLD:-istio-control}

while [ $# -gt 0 ]
do
    case "$1" in
        --update)
            METHOD=Update
            ;;
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
    install_crds) install_crds ;;
    install_system) install_system ;;
    install_control) install_control ;;
    install_ingress) install_ingress ;;
    install_telemetry) install_telemetry ;;
    switch_istio_control) switch_istio_control ;;
    install_all) install_crds &&  install_system && install_control && install_ingress && install_telemetry && switch_istio_control;;
esac

echo "Finished"
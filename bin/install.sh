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
    echo "Installing custom ressource defintions"
    kubectl apply -f crds.yaml
    
    kubectl wait --for=condition=Established -f crds.yaml
}

# Install citadel into namespace istio-system

function install_system() {
    kubectl delete namespace istio-system --wait --ignore-not-found
    echo "Installing citadel.."
    bin/iop istio-system istio-system-security $IBASE/security/citadel/
    
    kubectl get deployments -n istio-system
    kubectl wait deployments istio-citadel11 -n istio-system --for=condition=available --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment istio-citadel11 -n istio-system --timeout=$WAIT_TIMEOUT
    kubectl get deployments -n istio-system
    kubectl get pod -n istio-system
}

# Install config, discovery and sidecar-injector into namespace istio-control

function install_control() {
    kubectl delete namespace istio-control --wait --ignore-not-found
    echo "Installing galley.."
    bin/iop istio-control istio-config $IBASE/istio-control/istio-config --set configValidation=true
    echo "Installing pilot.."
    bin/iop istio-control istio-discovery $IBASE/istio-control/istio-discovery
    echo "Installing auto-injector.."
    bin/iop istio-control istio-autoinject $IBASE/istio-control/istio-autoinject --set global.istioNamespace=istio-control

    kubectl get deployments -n istio-control
    kubectl wait deployments istio-galley istio-pilot istio-sidecar-injector -n istio-control --for=condition=available --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment istio-galley -n istio-control --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment istio-pilot  -n istio-control --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment istio-sidecar-injector -n istio-control --timeout=$WAIT_TIMEOUT

    kubectl get deployments -n istio-control
    kubectl get pod -n istio-control 
}

# Install discovery and ingress into namespace istio-ingress

function install_ingress() {
    kubectl delete namespace istio-ingress --wait --ignore-not-found
    echo "Installing pilot.."
    bin/iop istio-ingress istio-discovery $IBASE/istio-control/istio-discovery
    echo "Installing ingress.."
    bin/iop istio-ingress istio-ingress $IBASE/gateways/istio-ingress  --set global.istioNamespace=istio-control

    kubectl get deployments -n istio-ingress
    kubectl wait deployments ingressgateway  istio-pilot -n istio-ingress --for=condition=available --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment ingressgateway -n istio-ingress --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment  istio-pilot -n istio-ingress --timeout=$WAIT_TIMEOUT
    kubectl get deployments -n istio-ingress
    kubectl get pod -n istio-ingress
}

# Install grafana, mixer and prometheus into namespace istio-telemetry

function install_telemetry() {
    kubectl delete namespace istio-telemetry --wait --ignore-not-found
    echo "Installing istio-grafana.."
    bin/iop istio-telemetry istio-grafana $IBASE/istio-telemetry/grafana/ --set global.istioNamespace=istio-control
    echo "Installing istio-mixer.."
    bin/iop istio-telemetry istio-mixer $IBASE/istio-telemetry/mixer-telemetry/ --set global.istioNamespace=istio-control
    echo "Installing istio-prometheus."
    bin/iop istio-telemetry istio-prometheus $IBASE/istio-telemetry/prometheus/ --set global.istioNamespace=istio-control
    kubectl get deployments -n istio-telemetry
    kubectl wait deployments grafana istio-telemetry prometheus -n istio-telemetry --for=condition=available --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment grafana -n istio-telemetry --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment istio-telemetry -n istio-telemetry --timeout=$WAIT_TIMEOUT
    kubectl rollout status  deployment prometheus -n istio-telemetry --timeout=$WAIT_TIMEOUT
    kubectl get deployments -n istio-telemetry
    kubectl get pod -n istio-telemetry
}


case "$1" in
    install_crds) install_crds ;;
    install_system) install_system ;;
    install_control) install_control ;;
    install_ingress) install_ingress ;;
    install_telemetry) install_telemetry ;;
    *) install_crds &&  install_system && install_control && install_ingress && install_telemetry;;
esac

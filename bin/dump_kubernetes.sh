#!/bin/sh
#
# Uses kubectl to collect cluster information.
# Dumps:
# - Logs of every container of every pod of every namespace.
# - Resource configurations for ingress, endpoints, custom resource
#   definitions, configmaps, secrets (names only) and "all" as defined by
#   kubectl.

OUT_DIR="${1:-istio-dump}"
LOG_DIR="${OUT_DIR}/logs"
RESOURCES_FILE="${OUT_DIR}/resources.yaml"

check_prerequisites() {
  PREREQUISITES=$*
  for prerequisite in ${PREREQUISITES}; do
    if ! command -v "${prerequisite}" > /dev/null; then
      echo "\"${prerequisite}\" is required. Please install it."
      return 1
    fi
  done
}

dump_logs() {
  NAMESPACES=$(kubectl get namespaces -o=jsonpath="{.items[*].metadata.name}")
  for namespace in ${NAMESPACES}; do
    PODS=$(kubectl get --namespace="${namespace}" pods -o=jsonpath='{.items[*].metadata.name}')
    for pod in ${PODS}; do
      CONTAINERS=$(kubectl get --namespace="${namespace}" pod "${pod}" -o=jsonpath='{.spec.containers[*].name}')
      for container in ${CONTAINERS}; do
        mkdir -p "${LOG_DIR}/${namespace}/${pod}"
        LOG_FILE_HEAD="${LOG_DIR}/${namespace}/${pod}/${container}"

        LOG_FILE="${LOG_FILE_HEAD}.log"
        kubectl logs --namespace="${namespace}" "${pod}" "${container}" > "${LOG_FILE}"

        LOG_PREVIOUS_FILE="${LOG_FILE_HEAD}_previous.log"
        RESTART_COUNT=$(kubectl get --namespace="${namespace}" pod "${pod}" -o=jsonpath='{.status.containerStatuses[?(@.name == '\""${container}"\"')].restartCount}')
        if [ "${RESTART_COUNT}" -gt 0 ]; then
          kubectl logs --namespace="${namespace}" --previous "${pod}" "${container}" 2> /dev/null > "${LOG_PREVIOUS_FILE}"
        fi
      done
    done
  done
}

dump_resources() {
  mkdir -p "${OUT_DIR}"
  # Only works in Kubernetes 1.8.0 and above.
  kubectl get --all-namespaces --export all,ingress,endpoints,customresourcedefinitions,configmaps,secrets -o yaml > "${RESOURCES_FILE}"
}

check_prerequisites kubectl
dump_logs
dump_resources

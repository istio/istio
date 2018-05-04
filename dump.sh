#!/bin/sh
#
# Uses kubectl to collect cluster information.
# Dumps:
# - Logs of every container of every pod of every namespace.
# - Resource configurations for ingress, endpoints, custom resource
#   definitions, and "all" as defined by kubectl.

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
        LOG_FILE_PREVIOUS="${LOG_FILE_HEAD}_previous.log"
        kubectl logs --namespace="${namespace}" "${pod}" "${container}" > "${LOG_FILE}"
        kubectl logs --namespace="${namespace}" --previous "${pod}" "${container}" > "${LOG_FILE_PREVIOUS}" 2> /dev/null
      done
    done
  done
}

dump_resources() {
  # Only works in Kubernetes 1.8.0 and above.
  kubectl get --all-namespaces --export all,ingress,endpoints,customresourcedefinition -o yaml > "${RESOURCES_FILE}"
}

check_prerequisites kubectl
dump_logs
dump_resources

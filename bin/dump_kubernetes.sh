#!/bin/bash
#
# Uses kubectl to collect cluster information.
# Dumps:
# - Logs of every container of every pod of every namespace.
# - Resource configurations for ingress, endpoints, custom resource
#   definitions, configmaps, secrets (names only) and "all" as defined by
#   kubectl.

readonly OUT_DIR="${1:-istio-dump}"
readonly LOG_DIR="${OUT_DIR}/logs"
readonly RESOURCES_FILE="${OUT_DIR}/resources.yaml"

check_prerequisites() {
  local prerequisites=$*
  for prerequisite in ${prerequisites}; do
    if ! command -v "${prerequisite}" > /dev/null; then
      echo "\"${prerequisite}\" is required. Please install it."
      return 1
    fi
  done
}

dump_logs() {
  local namespaces
  namespaces=$(kubectl get \
      namespaces -o=jsonpath="{.items[*].metadata.name}")
  for namespace in ${namespaces}; do
    local pods
    pods=$(kubectl get --namespace="${namespace}" \
        pods -o=jsonpath='{.items[*].metadata.name}')
    for pod in ${pods}; do
      local containers
      containers=$(kubectl get --namespace="${namespace}" \
          pod "${pod}" -o=jsonpath='{.spec.containers[*].name}')
      for container in ${containers}; do
        mkdir -p "${LOG_DIR}/${namespace}/${pod}"
        local log_file_head="${LOG_DIR}/${namespace}/${pod}/${container}"

        local log_file="${log_file_head}.log"
        kubectl logs --namespace="${namespace}" "${pod}" "${container}" \
            > "${log_file}"

        local filter="?(@.name == \"${container}\")"
        local json_path='{.status.containerStatuses['${filter}'].restartCount}'
        local restart_count
        restart_count=$(kubectl get --namespace="${namespace}" \
            pod "${pod}" -o=jsonpath="${json_path}")
        if [ "${restart_count}" -gt 0 ]; then
          local log_previous_file
          log_previous_file="${log_file_head}_previous.log"
          kubectl logs --namespace="${namespace}" \
              --previous "${pod}" "${container}" \
              > "${log_previous_file}"
        fi
      done
    done
  done
}

dump_resources() {
  mkdir -p "${OUT_DIR}"
  # Only works in Kubernetes 1.8.0 and above.
  kubectl get --all-namespaces --export \
      all,ingress,endpoints,customresourcedefinitions,configmaps,secrets \
      -o yaml > "${RESOURCES_FILE}"
}

check_prerequisites kubectl
dump_logs
dump_resources

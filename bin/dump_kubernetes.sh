#!/bin/bash
#
# Uses kubectl to collect cluster information.
# Dumps:
# - Logs of every container of every pod of every namespace.
# - Resource configurations for ingress, endpoints, custom resource
#   definitions, configmaps, secrets (names only) and "all" as defined by
#   kubectl.

error() {
  echo "$*" >&2
}

usage() {
  error "usage: dump_kubernetes.sh [options]"
  error ""
  error "  -d, --output-directory   directory to output files; defaults to"
  error "                               \"istio-dump\""
  error "  -q, --quiet              if present, do not log"
  exit 1
}

log() {
  local msg="${1}"
  if [ "${QUIET}" = false ]; then
    printf "%s\n" "${msg}"
  fi
}

parse_args() {
  while [ "$#" -gt 0 ]; do
    case "${1}" in
      -d|--output-directory)
        local out_dir="${2}"
        shift 2 # Shift past option and value.
        ;;
      -q|--quiet)
        local quiet=true
        shift # Shift past flag.
        ;;
      *)
        usage
        ;;
    esac
  done

  readonly OUT_DIR="${out_dir:-istio-dump}"
  readonly QUIET="${quiet:-false}"
  readonly LOG_DIR="${OUT_DIR}/logs"
  readonly RESOURCES_FILE="${OUT_DIR}/resources.yaml"
}

check_prerequisites() {
  local prerequisites=$*
  for prerequisite in ${prerequisites}; do
    if ! command -v "${prerequisite}" > /dev/null; then
      error "\"${prerequisite}\" is required. Please install it."
      return 1
    fi
  done
}

dump_time() {
  mkdir -p "${OUT_DIR}"
  date -u > "${OUT_DIR}/DUMP_TIME"
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
        log "Retrieving logs for ${namespace}/${pod}/${container}"

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
          log "Retrieving previous logs for ${namespace}/${pod}/${container}"

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
  log "Retrieving resource configurations"

  mkdir -p "${OUT_DIR}"
  # Only works in Kubernetes 1.8.0 and above.
  kubectl get --all-namespaces --export \
      all,ingresses,endpoints,customresourcedefinitions,configmaps,secrets,events \
      -o yaml > "${RESOURCES_FILE}"
}

main() {
  parse_args "$@"
  check_prerequisites kubectl
  dump_time
  dump_logs
  dump_resources
}

main "$@"

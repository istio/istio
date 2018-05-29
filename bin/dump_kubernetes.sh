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
  error "  -z, --archive            if present, archives and removes the output"
  error "                               directory"
  error "  -q, --quiet              if present, do not log"
  exit 1
}

log() {
  local msg="${1}"
  if [ "${QUIET}" = false ]; then
    printf '%s\n' "${msg}"
  fi
}

parse_args() {
  while [ "$#" -gt 0 ]; do
    case "${1}" in
      -d|--output-directory)
        local out_dir="${2}"
        shift 2 # Shift past option and value.
        ;;
      -z|--archive)
        local should_archive=true
        shift # Shift past flag.
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
  readonly SHOULD_ARCHIVE="${should_archive:-false}"
  readonly QUIET="${quiet:-false}"
  readonly LOG_DIR="${OUT_DIR}/logs"
  readonly RESOURCES_FILE="${OUT_DIR}/resources.yaml"
  readonly ISTIO_RESOURCES_FILE="${OUT_DIR}/istio-resources.yaml"
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

  local istio_resources
  # Trim to only first field; join by comma; remove last comma.
  istio_resources=$(kubectl get crd --no-headers \
      | cut -d ' ' -f 1 \
      | tr '\n' ',' \
      | sed 's/,$//')
  kubectl get "${istio_resources}" --all-namespaces -o yaml > "${ISTIO_RESOURCES_FILE}"

  kubectl cluster-info dump > ${OUT_DIR}/logs/cluster-info.dump.txt
  kubectl describe pods -n istio-system > ${OUT_DIR}/logs/pods-system.txt
  kubectl get event --all-namespaces -o wide > ${OUT_DIR}/logs/events.txt
}

dump_pilot_url(){
  local pilot_pod=$1
  local url=$2
  local dname=$3
  local outfile

  outfile="${dname}/$(basename "${url}")"

  log "Fetching ${url} from pilot"
  kubectl --namespace istio-system exec -i -t "${pilot_pod}" -c istio-proxy -- curl "http://localhost:8080/${url}" > "${outfile}"
}

dump_pilot() {
  local pilot_pod
  local pilot_dir

  pilot_dir="${OUT_DIR}/pilot"
  mkdir -p "${pilot_dir}"

  pilot_pod=$(kubectl --namespace istio-system get pods -listio=pilot -o=jsonpath='{.items[0].metadata.name}')

  dump_pilot_url "${pilot_pod}" debug/configz "${pilot_dir}"
  dump_pilot_url "${pilot_pod}" debug/endpointz "${pilot_dir}"
  dump_pilot_url "${pilot_pod}" debug/adsz "${pilot_dir}"
  dump_pilot_url "${pilot_pod}" metrics "${pilot_dir}"
}

archive() {
  local parent_dir
  parent_dir=$(dirname "${OUT_DIR}")
  local dir
  dir=$(basename "${OUT_DIR}")

  pushd "${parent_dir}" > /dev/null || exit
  tar -czf "${dir}.tar.gz" "${dir}"
  popd > /dev/null || exit

  log "Wrote ${parent_dir}/${dir}.tar.gz"
}

main() {
  parse_args "$@"
  check_prerequisites kubectl
  dump_time
  dump_pilot
  dump_logs
  dump_resources

  if [ "${SHOULD_ARCHIVE}" = true ] ; then
    archive
    rm -r "${OUT_DIR}"
  fi
  log "Wrote to ${OUT_DIR}"
}

main "$@"

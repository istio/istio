#!/bin/bash
#
# Uses kubectl to collect cluster information.
# Dumps:
# - Logs of every container of every pod of every namespace.
# - Resource configurations for ingress, endpoints, custom resource
#   definitions, configmaps, secrets (names only) and "all" as defined by
#   kubectl.

COREDUMP_DIR="/var/lib/istio"

error() {
  echo "$*" >&2
}

usage() {
  error 'Collect all possible data from a Kubernetes cluster using kubectl.'
  error ''
  error 'Usage:'
  error '  dump_kubernetes.sh [options]'
  error ''
  error 'Options:'
  error '  -d, --output-directory   directory to output files; defaults to'
  error '                               "istio-dump"'
  error '  -z, --archive            if present, archives and removes the output'
  error '                               directory'
  error '  -q, --quiet              if present, do not log'
  error '  --error-if-nasty-logs    if present, exit with 255 if any logs'
  error '                               contain errors'
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
      --error-if-nasty-logs)
        local should_check_logs_for_errors=true
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
  readonly SHOULD_CHECK_LOGS_FOR_ERRORS="${should_check_logs_for_errors:-false}"
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

dump_logs_for_container() {
  local namespace="${1}"
  local pod="${2}"
  local container="${3}"

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
}

copy_core_dumps_if_istio_proxy() {
  local namespace="${1}"
  local pod="${2}"
  local container="${3}"
  local got_core_dump=false

  if [ "istio-proxy" = "${container}" ]; then
    local out_dir="${LOG_DIR}/${namespace}/${pod}"
    mkdir -p "${out_dir}"
    local core_dumps
    core_dumps=$(kubectl exec -n "${namespace}" "${pod}" -c "${container}" -- \
        find ${COREDUMP_DIR} -name 'core.*')
    for f in ${core_dumps}; do
      local out_file
      out_file="${out_dir}/$(basename "${f}")"

      kubectl exec -n "${namespace}" "${pod}" -c "${container}" -- \
          cat "${f}" > "${out_file}"

      log "Copied ${namespace}/${pod}/${container}:${f} to ${out_file}"
      got_core_dump=true
    done
  fi
  if [ "${got_core_dump}" = true ]; then
    return 254
  fi
}

# Run functions on each container. Each argument should be a function which
# takes 3 args: ${namespace} ${pod} ${container}.
# If any of the called functions returns error, tap_containers returns
# immediately with that error.
tap_containers() {
  local functions=( "$@" )

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

        for f in "${functions[@]}"; do
          "${f}" "${namespace}" "${pod}" "${container}" || return $?
        done

      done
    done
  done

  return 0
}

dump_kubernetes_resources() {
  log "Retrieving kubernetes resource configurations"

  mkdir -p "${OUT_DIR}"
  # Only works in Kubernetes 1.8.0 and above.
  kubectl get --all-namespaces --export \
      all,jobs,ingresses,endpoints,customresourcedefinitions,configmaps,secrets,events \
      -o yaml > "${RESOURCES_FILE}"
}

dump_istio_custom_resource_definitions() {
  log "Retrieving istio resource configurations"

  local istio_resources
  # Trim to only first field; join by comma; remove last comma.
  istio_resources=$(kubectl get customresourcedefinitions \
      --no-headers 2> /dev/null \
      | cut -d ' ' -f 1 \
      | tr '\n' ',' \
      | sed 's/,$//')

  if [ ! -z "${istio_resources}" ]; then
    kubectl get "${istio_resources}" --all-namespaces -o yaml \
        > "${ISTIO_RESOURCES_FILE}"
  fi
}

dump_resources() {
  dump_kubernetes_resources
  dump_istio_custom_resource_definitions

  mkdir -p "${OUT_DIR}"
  kubectl cluster-info dump > "${OUT_DIR}/cluster-info.dump.txt"
  kubectl describe pods -n istio-system > "${OUT_DIR}/istio-system-pods.txt"
  kubectl get events --all-namespaces -o wide > "${OUT_DIR}/events.txt"
}

dump_pilot_url(){
  local pilot_pod=$1
  local url=$2
  local dname=$3
  local outfile

  outfile="${dname}/$(basename "${url}")"

  log "Fetching ${url} from pilot"
  kubectl -n istio-system exec -i -t "${pilot_pod}" -c istio-proxy -- \
      curl "http://localhost:8080/${url}" > "${outfile}"
}

dump_pilot() {
  local pilot_pod
  pilot_pod=$(kubectl -n istio-system get pods -l istio=pilot \
      -o jsonpath='{.items[*].metadata.name}')

  if [ ! -z "${pilot_pod}" ]; then
    local pilot_dir="${OUT_DIR}/pilot"
    mkdir -p "${pilot_dir}"

    dump_pilot_url "${pilot_pod}" debug/configz "${pilot_dir}"
    dump_pilot_url "${pilot_pod}" debug/endpointz "${pilot_dir}"
    dump_pilot_url "${pilot_pod}" debug/adsz "${pilot_dir}"
    dump_pilot_url "${pilot_pod}" metrics "${pilot_dir}"
  fi
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

check_logs_for_errors() {
  log "Searching logs for errors."
  grep -R --include "${LOG_DIR}/*.log" --ignore-case -e 'segmentation fault'
}

main() {
  local exit_code=0
  parse_args "$@"
  check_prerequisites kubectl
  dump_time
  dump_pilot
  dump_resources
  exit_code=tap_containers dump_logs_for_container copy_core_dumps_if_istio_proxy

  if [ "${SHOULD_CHECK_LOGS_FOR_ERRORS}" = true ]; then
    if ! check_logs_for_errors; then
      exit_code=255
    fi
  fi

  if [ "${SHOULD_ARCHIVE}" = true ] ; then
    archive
    rm -r "${OUT_DIR}"
  fi
  log "Wrote to ${OUT_DIR}"

  return ${exit_code}
}

main "$@"

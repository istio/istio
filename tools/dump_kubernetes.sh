#!/bin/bash

# Copyright Istio Authors
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

#
# Uses kubectl to collect cluster information.
# Dumps:
# - Logs of every container of every pod of every namespace.
# - Resource configurations for ingress, endpoints, custom resource
#   definitions, configmaps, secrets (names only) and "all" as defined by
#   kubectl.

COREDUMP_DIR="/var/lib/istio"

# Limit total log size to 100MB by default
DEFAULT_MAX_LOG_BYTES=100000000

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
  error '  -m, --max-bytes          max total bytes, 0=no limit, default='${DEFAULT_MAX_LOG_BYTES}
  error '  -l, --label              if set, dump logs only for pods with given labels e.g. "-l app=pilot -l istio=galley"'
  error '  -n, --namespace          if set, dump logs only for pods in the given namespaces e.g. "-n default -n istio-system"'
  error '  -p, --pod                if set, dump logs only for pod matching these values e.g. "-n istio-system -p istio-policy-7857db5f54-wj59d -p istio-pilot-5cb9d848b5-xx25q"'
  error '  -c, --control-plane      if set, dump debugging information from control plane'
  error '  -u, --cluster            if set, dump logs from cluster'
  error '  -x, --proxy              if set, dump logs from proxis e.g "-x istio=ingressgateway"'
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
  local max_bytes="${DEFAULT_MAX_LOG_BYTES}"
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
      -m|--max-bytes)
        max_bytes="${2}"
        shift 2
        ;;
      -l|--label)
        pod_labels+="${2} "
        shift 2
        ;;
      -n|--namespace)
        namespaces+="${2} "
        shift 2
        ;;
      -p|--pod)
        pods+="${2} "
        shift 2
        ;;
      -c|--control-plane)
        local control_plane=true
        shift # Shift past flag.
        ;;
      -u|--cluster)
        local cluster=true
        shift # Shift past flag.
        ;;
      -x|--proxy)
        proxy_label="${2}"
        shift 2
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
  readonly MAX_LOG_BYTES="${max_bytes}"
  readonly CONTROL_PLANE="${control_plane:-false}"
  readonly ONLY_CLUSTER="${cluster:-false}"
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

# mv_unless_max_exceeded src_file dest_file performs mv src_file dest_file unless the global variable stored_log_bytes
# would exceed max_log_bytes.
# If total not exceeded, file is moved and max_log_bytes updated, otherwise an error is logged.
# src_file is always deleted when calling this function.
mv_unless_max_exceeded() {
  local src_file="${1}"
  local dst_file="${2}"

  file_size=$(wc -c "${src_file}" | awk '{print $1}')
  local nsb=$((stored_log_bytes + file_size))

  if (("${nsb}" > "${MAX_LOG_BYTES}")); then
    log "Not storing ${log_file} because appending its ${file_size} bytes would exceed max logged bytes ${MAX_LOG_BYTES}"
    rm "${src_file}"
  else
    dirn=$(dirname "${dst_file}")
    mkdir -p "${dirn}"
    mv "${src_file}" "${dst_file}"
    stored_log_bytes="${nsb}"
  fi
}

dump_logs_for_container() {
  local namespace="${1}"
  local pod="${2}"
  local container="${3}"

  log "Retrieving logs for ${namespace}/${pod}/${container}"

  mkdir -p "${LOG_DIR}/${namespace}/${pod}"
  local log_file_head="${LOG_DIR}/${namespace}/${pod}/${container}"
  local temp_log_file="${LOG_DIR}/temp_log_file.log"

  local log_file="${log_file_head}.log"
  kubectl logs --namespace="${namespace}" "${pod}" "${container}" \
      > "${temp_log_file}"
  mv_unless_max_exceeded "${temp_log_file}" "${log_file}"

  local filter="?(@.name == \"${container}\")"
  local json_path='{.status.containerStatuses['${filter}'].restartCount}'
  local restart_count
  restart_count=$(kubectl get --namespace="${namespace}" \
      pod "${pod}" -o=jsonpath="${json_path}")
  # (There will be no restart_count if the pod status is for example "Pending")
  if [ -n "${restart_count}" ] && [ "${restart_count}" -gt 0 ]; then
    log "Retrieving previous logs for ${namespace}/${pod}/${container}"

    local log_previous_file
    log_previous_file="${log_file_head}_previous.log"
    kubectl logs --namespace="${namespace}" \
        --previous "${pod}" "${container}" \
        > "${temp_log_file}"
    mv_unless_max_exceeded "${temp_log_file}" "${log_previous_file}"
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
  local functions=("$@")
  if [ -z "${namespaces}" ]; then
    namespaces=$(kubectl get \
        namespaces -o=jsonpath="{.items[*].metadata.name}")
  fi
  local pod_match=0
  for namespace in ${namespaces}; do
    local lpods=""
    if [ -z "${pods}" ]; then
      if [ -n "${pod_labels}" ]; then
        for label in $pod_labels; do
          lpods+=$(kubectl get --namespace="${namespace}" -l"${label}" \
              pods -o=jsonpath='{.items[*].metadata.name}')" "
        done
      else
          lpods=$(kubectl get --namespace="${namespace}" \
              pods -o=jsonpath='{.items[*].metadata.name}')
      fi
    else
      lpods=${pods}
    fi
    for pod in ${lpods}; do
      local containers
      containers=$(kubectl get --namespace="${namespace}" \
          pod "${pod}" -o=jsonpath='{.spec.containers[*].name}')
      for container in ${containers}; do
        pod_match=1
        for f in "${functions[@]}"; do
          "${f}" "${namespace}" "${pod}" "${container}" || return $?
        done
      done
    done
  done
  if [ -n "${pods}" ] && [ $pod_match == 0 ]; then
    log "Not pod matched the provided -p option"
  fi

  return 0
}

dump_kubernetes_resources() {
  log "Retrieving Kubernetes resource configurations"

  mkdir -p "${OUT_DIR}"
  # Only works in Kubernetes 1.8.0 and above.
  if [ -z "${namespaces}" ]; then
    kubectl get --all-namespaces --export \
        all,jobs,ingresses,endpoints,customresourcedefinitions,configmaps,secrets,events \
        -o yaml > "${RESOURCES_FILE}"
  else
    for namespace in ${namespaces}; do
      NAMESPACE_RESOURCES_FILE="${RESOURCES_FILE//resources.yaml/$namespace}-namespace-resources.yaml"
      kubectl get -n "${namespace}" --export \
          all,jobs,ingresses,endpoints,customresourcedefinitions,configmaps,secrets,events \
          -o yaml > "${NAMESPACE_RESOURCES_FILE}"
    done
  fi
}

dump_istio_custom_resource_definitions() {
  log "Retrieving Istio resource configurations"

  local istio_resources
  # Trim to only first field; join by comma; remove last comma.
  istio_resources=$(kubectl get customresourcedefinitions \
      --no-headers 2> /dev/null \
      | cut -d ' ' -f 1 \
      | tr '\n' ',' \
      | sed 's/,$//')

  if [ -n "${istio_resources}" ]; then
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
  if [ -z "${namespaces}" ]; then
    kubectl get events --all-namespaces -o wide > "${OUT_DIR}/events.txt"
  else
    for namespace in ${namespaces}; do
      NAMESPACE_EVENT_FILE="${OUT_DIR}/${namespace}-events.txt"
      kubectl get events -n "${namespace}" -o wide > "${NAMESPACE_EVENT_FILE}"
    done
  fi
}

dump_istio_status() {
  istioctl version > "${OUT_DIR}/istioctl-version.txt"
  istioctl proxy-status > "${OUT_DIR}/istioctl-proxy-status.txt"
  kubectl get configmap -n istio-system > "${OUT_DIR}/istio-system-configmap.txt"
  kubectl get configmap -n istio-system > "${OUT_DIR}/istio-system-configmap.txt"
  kubectl get configmap istio -n istio-system && kubectl get configmap istio-sidecar-injector -n istio-system > "${OUT_DIR}/istio-system-configmaps.txt"
}

dump_control_plane() {
  # dump_control_plane Should be called for only one namespace (and that would
  # be "istio-system").
  # This ns variable is kept to use for other namespaces if needed later
  local ns="${namespaces}"
  local describe_dir="${OUT_DIR}/describe"
  mkdir -p "${describe_dir}"
  kubectl -n "${ns}" get pods -o yaml > "${OUT_DIR}/${ns}-pods.yaml"
  api_resources=$(kubectl api-resources -n "${ns}" -o name)
  for api_resource in ${api_resources}; do
    kubectl describe "${api_resource}" -n "${ns}" > "${describe_dir}/${ns}-${api_resource}.txt"
    if [ ! -s "${describe_dir}/${ns}-${api_resource}.txt" ]; then
      rm -f "${describe_dir}/${ns}-${api_resource}.txt"
  fi
  done
}

dump_cluster() {
  kubectl get nodes --all-namespaces -o yaml > "${OUT_DIR}/all-namespaces-nodes.yaml"
  kubectl get services --all-namespaces -o yaml > "${OUT_DIR}/all-namespaces-services.yaml"
  kubectl get pods --all-namespaces -o yaml > "${OUT_DIR}/all-namespaces-pods.yaml"
}

dump_url(){
  local pod=$1
  local url=$2
  local path=$3
  local dname=$4
  local outfile

  outfile="${dname}/$(basename "${path}")"

  log "Fetching ${url}${path} from $pod"
  kubectl -n istio-system exec -i -t "${pod}" -c istio-proxy -- \
      curl "${url}${path}" > "${outfile}"
}

dump_pilot() {
  local pilot_pod
  pilot_pod=$(kubectl -n istio-system get pods -l istio=pilot \
      -o jsonpath='{.items[*].metadata.name}')

  if [ -n "${pilot_pod}" ]; then
    local pilot_dir="${OUT_DIR}/pilot"
    mkdir -p "${pilot_dir}"

    local lurl="http://localhost:8080/"
    dump_url "${pilot_pod}" "${lurl}" "debug/configz" "${pilot_dir}"
    dump_url "${pilot_pod}" "${lurl}" "debug/endpointz" "${pilot_dir}"
    dump_url "${pilot_pod}" "${lurl}" "debug/adsz" "${pilot_dir}"
    dump_url "${pilot_pod}" "${lurl}" "debug/authenticationz" "${pilot_dir}"
    dump_url "${pilot_pod}" "${lurl}" "metrics" "${pilot_dir}"
  fi
}

dump_proxy() {
  proxy_pod=$(kubectl get po -n istio-system -l "${proxy_label}" \
	  -o=jsonpath="{.items[*].metadata.name}")

  local proxy_dir="${OUT_DIR}/proxy/${proxy_label}"
  mkdir -p "${proxy_dir}"
  local lurl="http://localhost:15000/"
  dump_url "${proxy_pod}" "${lurl}" "config_dump" "${proxy_dir}"
  dump_url "${proxy_pod}" "${lurl}" "clusters" "${proxy_dir}"
  dump_url "${proxy_pod}" "${lurl}" "stats" "${proxy_dir}"
  dump_url "${proxy_pod}" "${lurl}" "certs" "${proxy_dir}"
  dump_url "${proxy_pod}" "${lurl}" "memory" "${proxy_dir}"

  kubectl logs "${proxy_pod}" -n istio-system -c istio-proxy > "${proxy_dir}/proxy_logs.txt"
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
  if [ "${CONTROL_PLANE}" = true ] ; then
    namespaces="istio-system"
    check_prerequisites istioctl
    dump_time
    dump_control_plane
    tap_containers "dump_logs_for_container"
    dump_istio_status
    exit_code=$?
  elif [ "${ONLY_CLUSTER}" = true ] ; then
    dump_time
    dump_cluster
    exit_code=$?
  elif [ -n "${proxy_label}" ] ; then
    dump_time
    dump_proxy
    exit_code=$?
  else # old dump behavior
    dump_time
    dump_pilot
    dump_resources
    tap_containers "dump_logs_for_container" "copy_core_dumps_if_istio_proxy"
    exit_code=$?
  fi

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

stored_log_bytes=0

main "$@"

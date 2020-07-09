#!/bin/bash

# Copyright 2019 Istio Authors
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

# Script to install Istio CNI on a Kubernetes host.
# - Expects the host CNI binary path to be mounted at /host/opt/cni/bin.
# - Expects the host CNI network config path to be mounted at /host/etc/cni/net.d.
# - Expects the desired CNI config in the CNI_NETWORK_CONFIG env variable.

# Ensure all variables are defined, and that the script fails when an error is hit.
set -u -e

# Helper function for raising errors
# Usage:
# some_command || exit_with_error "some_command_failed: maybe try..."
function exit_with_error() {
  echo "$1"
  exit 1
}

function exit_graceful() {
  exit 0
}

# Adds the given file to FILE_CLEANUP_ARR for cleanup on exit
function add_file_to_cleanup() {
  FILE_CLEANUP_ARR+=( "$1" )
}

function rm_bin_files() {
  echo "Removing existing binaries"
  rm -f /host/opt/cni/bin/istio-cni /host/opt/cni/bin/istio-iptables
}

# Creates a temp file with the given filepath prefix
# Output: filepath of the temp file
# Usage:
# TMP_FILE=$(create_temp_file "filepath/prefix")
# Recommended usage:
# TMP_FILE=$(create_temp_file "filepath/prefix") && add_file_to_cleanup "${TMP_FILE}"
function create_temp_file() {
  local filepath=$1
  local temp_file
  temp_file=$(mktemp "${filepath}.tmp.XXXXXX") || exit_with_error "Failed to create temp file. Exiting (1)..."
  # mktemp creates files with read and write permissions only for owner
  # go tests require additional read permissions so default to 644
  chmod 644 "${temp_file}"
  echo "${temp_file}"
}

# Renames the given file to the new file name (renames to same dir for atomicity)
# Output: filepath of the renamed file
# Usage:
# NEW_FILE=$(atomically_rename_file "old_filepath" "new_filename")
function atomically_rename_file() {
  local old_filepath=$1
  local new_filename=$2
  local dirpath
  dirpath=$(dirname "${old_filepath}")
  local new_filepath="${dirpath}/${new_filename}"
  mv "${old_filepath}" "${new_filepath}" || \
  exit_with_error "Failed to rename file. This may be caused by the node's selinux configuration."
  echo "${new_filepath}"
}

# find_cni_conf_file
#   Finds the CNI config file in the mounted CNI config dir.
#   - Follows the same semantics as kubelet
#     https://github.com/kubernetes/kubernetes/blob/954996e231074dc7429f7be1256a579bedd8344c/pkg/kubelet/dockershim/network/cni/cni.go#L144-L184
function find_cni_conf_file() {
  local cni_cfg=
  for cfgf in "${MOUNTED_CNI_NET_DIR}"/*; do
    if [ "${cfgf: -5}" = ".conf" ]; then
      # check if it's a valid CNI .conf file
      local type
      type=$(jq 'has("type")' < "${cfgf}" 2>/dev/null)
      if [ "${type}" = "true" ]; then
          cni_cfg=$(basename "${cfgf}")
          break
      fi
    elif [ "${cfgf: -9}" = ".conflist" ]; then
      # Check that the file is json and has top level "name" and "plugins" keys
      # NOTE: "cniVersion" key is not required by libcni (kubelet) to be present
      local name
      local plugins
      name=$(jq 'has("name")' < "${cfgf}" 2>/dev/null)
      plugins=$(jq 'has("plugins")' < "${cfgf}" 2>/dev/null)
      if [ "${name}" = "true" ] && [ "${plugins}" = "true" ]; then
          cni_cfg=$(basename "${cfgf}")
          break
      fi
    fi
  done
  echo "${cni_cfg}"
}

# Initializes required variables
function init() {
  declare -a FILE_CLEANUP_ARR

  # The directory on the host where CNI networks are installed. Defaults to
  # /etc/cni/net.d, but can be overridden by setting CNI_NET_DIR.  This is used
  # for populating absolute paths in the CNI network config to assets
  # which are installed in the CNI network config directory.
  HOST_CNI_NET_DIR=${CNI_NET_DIR:-/etc/cni/net.d}
  MOUNTED_CNI_NET_DIR=${MOUNTED_CNI_NET_DIR:-/host/etc/cni/net.d}

  KUBECFG_FILE_NAME=${KUBECFG_FILE_NAME:-ZZZ-istio-cni-kubeconfig}
  CFGCHECK_INTERVAL=${CFGCHECK_INTERVAL:-1}

  # Whether the Istio CNI plugin should be installed as a chained plugin (defaults to true) or as a standalone plugin (when false)
  CHAINED_CNI_PLUGIN=${CHAINED_CNI_PLUGIN:-true}

  # if user sets CNI_CONF_NAME, script will use that value
  CNI_CONF_NAME=${CNI_CONF_NAME:-}
  # else if a CNI config file exists, default to first CNI config file (will overwrite config)
  # use internal var to allow loop to restart without modifying user defined env var
  cni_conf_name=${CNI_CONF_NAME:-$(find_cni_conf_file)}

  if [ "${CHAINED_CNI_PLUGIN}" == "true" ]; then
    # chained CNI plugin
    # waits until a main CNI plugin writes a CNI config file
    echo "cni_conf_name is null. Finding config file..."
    while : ; do
      if [ -z "${cni_conf_name}" ]; then
        cni_conf_name=$(find_cni_conf_file)
      elif [ ! -e "${MOUNTED_CNI_NET_DIR}/${cni_conf_name}" ]; then
        if [ "${cni_conf_name: -5}" = ".conf" ] && [ -e "${MOUNTED_CNI_NET_DIR}/${cni_conf_name}list" ]; then
            echo "${cni_conf_name} doesn't exist, but ${cni_conf_name}list does; Using it instead."
            cni_conf_name="${cni_conf_name}list"
        elif [ "${cni_conf_name: -9}" = ".conflist" ] && [ -e "${MOUNTED_CNI_NET_DIR}/${cni_conf_name:0:-4}" ]; then
            echo "${cni_conf_name} doesn't exist, but ${cni_conf_name:0:-4} does; Using it instead."
            cni_conf_name="${cni_conf_name:0:-4}"
        else
          echo "CNI config file ${MOUNTED_CNI_NET_DIR}/${cni_conf_name} does not exist. Waiting for file to be written..."
        fi
      else
        echo "${MOUNTED_CNI_NET_DIR}/${cni_conf_name} exists."
        break
      fi
      sleep "${CFGCHECK_INTERVAL}"
    done
  else
    # standalone CNI plugin
    # if no existing CNI config file, default to a filename that is not likely to be lexicographically first
    cni_conf_name=${CNI_CONF_NAME:-YYY-istio-cni.conflist}
  fi

  trap exit_graceful SIGINT
  trap exit_graceful SIGTERM
  trap cleanup EXIT
}

function install_binaries() {
  # Choose which default cni binaries should be copied
  local skip_cni_binaries=${SKIP_CNI_BINARIES:-""}
  skip_cni_binaries=",${skip_cni_binaries},"
  local update_cni_binaries=${UPDATE_CNI_BINARIES:-"true"}

  # Place the new binaries to the host's cni bin directory if the directory is writable.
  for dir in /host/opt/cni/bin /host/secondary-bin-dir
  do
    if [ ! -w "${dir}" ];
    then
      echo "${dir} is non-writable, skipping"
      continue
    fi
    for path in /opt/cni/bin/*;
    do
      local filename
      filename=$(basename "${path}")
      local tmp=",${filename},"
      if [ "${skip_cni_binaries#*$tmp}" != "${skip_cni_binaries}" ];
      then
        echo "${filename} is in SKIP_CNI_BINARIES, skipping"
        continue
      fi
      if [ "${update_cni_binaries}" != "true" ] && [ -f "${dir}/${filename}" ];
      then
        echo "${dir}/${filename} is already here and UPDATE_CNI_BINARIES isn't true, skipping"
        continue
      fi
      # Copy files atomically by first copying into the same directory then renaming.
      # shellcheck disable=SC2015
      cp "${path}" "${dir}/${filename}.tmp" && mv "${dir}/${filename}.tmp" "${dir}/${filename}" || exit_with_error "Failed to copy ${path} into ${dir}. This may be caused by the node's selinux configuration."
    done

    echo "Wrote Istio CNI binaries to ${dir}."
  done
}

function sleep_check_install() {
  while [ "${should_sleep}" = "true" ]; do
    sleep "${CFGCHECK_INTERVAL}"
    local cfgfile_nm
    cfgfile_nm=$(find_cni_conf_file)
    if [ "${cfgfile_nm}" != "${cni_conf_name}" ]; then
      if [ -n "${CNI_CONF_NAME}" ]; then
         # Install was run with overridden cni config file so don't error out on the preempt check.
         # Likely the only use for this is testing this script.
         echo "WARNING: Configured CNI config file \"${MOUNTED_CNI_NET_DIR}/${cni_conf_name}\" preempted by \"${cfgfile_nm}\"."
      else
         echo "ERROR: CNI config file \"${MOUNTED_CNI_NET_DIR}/${cni_conf_name}\" preempted by \"${cfgfile_nm}\"."
         break
      fi
    fi
    if [ -e "${MOUNTED_CNI_NET_DIR}/${cni_conf_name}" ]; then
      local istiocni_conf
      if [ "${CHAINED_CNI_PLUGIN}" == "true" ]; then
        istiocni_conf=$(jq '.plugins[]? | select(.type == "istio-cni")' < "${MOUNTED_CNI_NET_DIR}/${cni_conf_name}")
        if [ -z "${istiocni_conf}" ]; then
          echo "ERROR: istio-cni CNI config removed from file: \"${MOUNTED_CNI_NET_DIR}/${cni_conf_name}\""
          break
        fi
      else
        istiocni_conf=$(jq -r '.type' < "${MOUNTED_CNI_NET_DIR}/${cni_conf_name}")
        if [ "${istiocni_conf}" != "istio-cni" ]; then
          echo "ERROR: istio-cni CNI config file modified: \"${MOUNTED_CNI_NET_DIR}/${cni_conf_name}\""
          break
        fi
      fi
    else
      echo "ERROR: CNI config file \"${MOUNTED_CNI_NET_DIR}/${cni_conf_name}\" removed."
      break
    fi
  done
}

function cleanup() {
  echo "Cleaning up."

  if [ -e "${MOUNTED_CNI_NET_DIR}/${cni_conf_name}" ]; then
    if [ "${CHAINED_CNI_PLUGIN}" == "true" ]; then
      local cni_conf_data
      local tmp_cni_conf_file
      local cni_conf_file
      echo "Removing istio-cni config from CNI chain config in ${MOUNTED_CNI_NET_DIR}/${cni_conf_name}"
      cni_conf_data=$(jq 'del( .plugins[]? | select(.type == "istio-cni"))' < "${MOUNTED_CNI_NET_DIR}/${cni_conf_name}")
      # Rewrite the config file atomically: write into a temp file in the same directory then rename.
      tmp_cni_conf_file=$(create_temp_file "${MOUNTED_CNI_NET_DIR}/${cni_conf_name}") && add_file_to_cleanup "${tmp_cni_conf_file}"
      cat > "${tmp_cni_conf_file}" <<EOF
${cni_conf_data}
EOF
      cni_conf_file=$(atomically_rename_file "${tmp_cni_conf_file}" "${cni_conf_name}")
      echo "Removed Istio CNI config from ${cni_conf_file}"
    else
      echo "Removing istio-cni net.d conf file: ${MOUNTED_CNI_NET_DIR}/${cni_conf_name}"
      rm "${MOUNTED_CNI_NET_DIR}/${cni_conf_name}"
    fi
  fi

  for file in "${FILE_CLEANUP_ARR[@]}"; do
    echo "Removing file if exists:  ${file}"
    rm -f "${file}"
  done

  rm_bin_files

  echo "Exiting."
}

while : ; do
  # include init in loop in case environment variables have changed
  init

  install_binaries

  # Create a temp file in the same directory as the target, in order for the final rename to be atomic.
  TMP_CNI_CONF_FILE="$(create_temp_file "${MOUNTED_CNI_NET_DIR}/${cni_conf_name}")" && add_file_to_cleanup "${TMP_CNI_CONF_FILE}"
  # If specified, overwrite the network configuration file.
  : "${CNI_NETWORK_CONFIG_FILE:=}"
  : "${CNI_NETWORK_CONFIG:=}"
  if [ -e "${CNI_NETWORK_CONFIG_FILE}" ]; then
    echo "Using CNI config template from ${CNI_NETWORK_CONFIG_FILE}."
    cp "${CNI_NETWORK_CONFIG_FILE}" "${TMP_CNI_CONF_FILE}"
  elif [ -n "${CNI_NETWORK_CONFIG}" ]; then
    echo "Using CNI config template from CNI_NETWORK_CONFIG environment variable."
    cat > "${TMP_CNI_CONF_FILE}" <<EOF
${CNI_NETWORK_CONFIG}
EOF
  fi

  # Set variables for writing kubeconfig file for the CNI plugin
  SERVICE_ACCOUNT_PATH=/var/run/secrets/kubernetes.io/serviceaccount
  KUBE_CA_FILE=${KUBE_CA_FILE:-${SERVICE_ACCOUNT_PATH}/ca.crt}
  SKIP_TLS_VERIFY=${SKIP_TLS_VERIFY:-false}
  # Pull out service account token.
  SERVICEACCOUNT_TOKEN=$(cat "${SERVICE_ACCOUNT_PATH}/token")

  # Check if we're running as a k8s pod.
  if [ -f "${SERVICE_ACCOUNT_PATH}/token" ]; then
    # We're running as a k8d pod - expect some variables.
    if [ -z "${KUBERNETES_SERVICE_HOST}" ]; then
      echo "KUBERNETES_SERVICE_HOST not set"; exit 1;
    fi
    if [ -z "${KUBERNETES_SERVICE_PORT}" ]; then
      echo "KUBERNETES_SERVICE_PORT not set"; exit 1;
    fi

    if [ "${SKIP_TLS_VERIFY}" = "true" ]; then
      TLS_CFG="insecure-skip-tls-verify: true"
    elif [ -f "${KUBE_CA_FILE}" ]; then
      TLS_CFG="certificate-authority-data: "$(base64 < "${KUBE_CA_FILE}" | tr -d '\n')
    fi

    # Write a kubeconfig file for the CNI plugin.  Do this
    # to skip TLS verification for now.  We should eventually support
    # writing more complete kubeconfig files. This is only used
    # if the provided CNI network config references it.
    # Create / overwrite this file atomically.
    KUBECFG_FILE="${MOUNTED_CNI_NET_DIR}/${KUBECFG_FILE_NAME}"
    TMP_KUBECFG_FILE="$(create_temp_file "${KUBECFG_FILE}")" && add_file_to_cleanup "${TMP_KUBECFG_FILE}"
    chmod "${KUBECONFIG_MODE:-600}" "${TMP_KUBECFG_FILE}"
    cat > "${TMP_KUBECFG_FILE}" <<EOF
# Kubeconfig file for Istio CNI plugin.
apiVersion: v1
kind: Config
clusters:
- name: local
  cluster:
    server: ${KUBERNETES_SERVICE_PROTOCOL:-https}://[${KUBERNETES_SERVICE_HOST}]:${KUBERNETES_SERVICE_PORT}
    $TLS_CFG
users:
- name: istio-cni
  user:
    token: "${SERVICEACCOUNT_TOKEN}"
contexts:
- name: istio-cni-context
  context:
    cluster: local
    user: istio-cni
current-context: istio-cni-context
EOF
    KUBECFG_FILE=$(atomically_rename_file "${TMP_KUBECFG_FILE}" "${KUBECFG_FILE_NAME}") && add_file_to_cleanup "${KUBECFG_FILE}"
  fi


  # Insert any of the supported "auto" parameters.
  sed -e "s/__KUBERNETES_SERVICE_HOST__/${KUBERNETES_SERVICE_HOST}/g" \
      -e "s/__KUBERNETES_SERVICE_PORT__/${KUBERNETES_SERVICE_PORT}/g" \
      -e "s/__KUBERNETES_NODE_NAME__/${KUBERNETES_NODE_NAME:-$(hostname)}/g" \
      -e "s/__KUBECONFIG_FILENAME__/${KUBECFG_FILE_NAME}/g" \
      -e "s~__KUBECONFIG_FILEPATH__~${HOST_CNI_NET_DIR}/${KUBECFG_FILE_NAME}~g" \
      -e "s~__LOG_LEVEL__~${LOG_LEVEL:-warn}~g" \
      -i "${TMP_CNI_CONF_FILE}"

  OLD_CNI_CONF_NAME=${OLD_CNI_CONF_NAME:-${cni_conf_name}}

  # Log the config file before inserting service account token.
  # This way auth token is not visible in the logs.
  echo -n "CNI config: "
  cat "${TMP_CNI_CONF_FILE}"

  sed -e "s/__SERVICEACCOUNT_TOKEN__/${SERVICEACCOUNT_TOKEN:-}/g" -i "${TMP_CNI_CONF_FILE}"

  if [ "${CHAINED_CNI_PLUGIN}" == "true" ]; then
    if [ -e "${MOUNTED_CNI_NET_DIR}/${cni_conf_name}" ]; then
        # This section overwrites an existing plugins list entry to for istio-cni
        TMP_CNI_CONF_DATA=$(cat "${TMP_CNI_CONF_FILE}")
        CNI_CONF_DATA=$(jq --argjson TMP_CNI_CONF_DATA "${TMP_CNI_CONF_DATA}" -f /filter.jq < "${MOUNTED_CNI_NET_DIR}/${cni_conf_name}")
        echo "${CNI_CONF_DATA}" > "${TMP_CNI_CONF_FILE}"
    fi

    # If the old config filename ends with .conf, rename it to .conflist, because it has changed to be a list
    if [ "${cni_conf_name: -5}" = ".conf" ]; then
        echo "Renaming ${cni_conf_name} extension to .conflist"
        cni_conf_name="${cni_conf_name}list"
    fi

    # Delete old CNI config files for upgrades.
    if [ "${cni_conf_name}" != "${OLD_CNI_CONF_NAME}" ]; then
        rm -f "${MOUNTED_CNI_NET_DIR}/${OLD_CNI_CONF_NAME}"
    fi
  fi

  # Move the temporary CNI config into place.
  CNI_CONF_FILE=$(atomically_rename_file "${TMP_CNI_CONF_FILE}" "${cni_conf_name}")
  echo "Wrote Istio CNI config in ${CNI_CONF_FILE}"

  # Unless told otherwise, sleep forever.
  # This prevents Kubernetes from restarting the pod repeatedly.
  should_sleep=${SLEEP:-"true"}
  echo "Done configuring CNI.  Sleep=$should_sleep"
  if [ "${should_sleep}" = "true" ]; then
    sleep_check_install
    echo "Invalid configuration. Restarting script..."
  else
    break
  fi
done

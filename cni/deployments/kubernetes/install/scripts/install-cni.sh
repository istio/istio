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
exit_with_error(){
  echo "$1"
  exit 1
}

exit_graceful(){
  exit 0
}

function rm_bin_files() {
  echo "Removing existing binaries"
  rm -f /host/opt/cni/bin/istio-cni /host/opt/cni/bin/istio-iptables
}
# find_cni_conf_file
#   Finds the CNI config file in the mounted CNI config dir.
#   - Follows the same semantics as kubelet
#     https://github.com/kubernetes/kubernetes/blob/954996e231074dc7429f7be1256a579bedd8344c/pkg/kubelet/dockershim/network/cni/cni.go#L144-L184
#
function find_cni_conf_file() {
    cni_cfg=
    for cfgf in "${MOUNTED_CNI_NET_DIR}"/*; do
        if [ "${cfgf: -5}" = ".conf" ]; then
            # check if it's a valid CNI .conf file
            type=$(jq 'has("type")' < "${cfgf}" 2>/dev/null)
            if [ "${type}" = "true" ]; then
                cni_cfg=$(basename "${cfgf}")
                break
            fi
        elif [ "${cfgf: -9}" = ".conflist" ]; then
            # Check that the file is json and has top level "name" and "plugins" keys
            # NOTE: "cniVersion" key is not required by libcni (kubelet) to be present
            name=$(jq 'has("name")' < "${cfgf}" 2>/dev/null)
            plugins=$(jq 'has("plugins")' < "${cfgf}" 2>/dev/null)
            if [ "${name}" = "true" ] && [ "${plugins}" = "true" ]; then
                cni_cfg=$(basename "${cfgf}")
                break
            fi
        fi
    done
    echo "$cni_cfg"
}

function check_install() {
  cfgfile_nm=$(find_cni_conf_file)
  if [ "${cfgfile_nm}" != "${CNI_CONF_NAME}" ]; then
    if [ -n "${CNI_CONF_NAME_OVERRIDE}" ]; then
       # Install was run with overridden cni config file so don't error out on the preempt check.
       # Likely the only use for this is testing this script.
       echo "WARNING: Configured CNI config file \"${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}\" preempted by \"$cfgfile_nm\"."
    else
       echo "ERROR: CNI config file \"${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}\" preempted by \"$cfgfile_nm\"."
       exit 1
    fi
  fi
  if [ -e "${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}" ]; then
    if [ "${CHAINED_CNI_PLUGIN}" == "true" ]; then
      istiocni_conf=$(jq '.plugins[]? | select(.type == "istio-cni")' < "${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}")
      if [ -z "${istiocni_conf}" ]; then
        echo "ERROR: istio-cni CNI config removed from file: \"${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}\""
        exit 1
      fi
    else
      istiocni_conf=$(jq -r '.type' < "${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}")
      if [ "${istiocni_conf}" != "istio-cni" ]; then
        echo "ERROR: istio-cni CNI config file modified: \"${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}\""
        exit 1
      fi
    fi
  else
    echo "ERROR: CNI config file \"${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}\" removed."
    exit 1
  fi
}

function cleanup() {
  echo "Cleaning up and exiting."

  if [ -e "${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}" ]; then
    if [ "${CHAINED_CNI_PLUGIN}" == "true" ]; then
      echo "Removing istio-cni config from CNI chain config in ${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}"
      CNI_CONF_DATA=$(jq 'del( .plugins[]? | select(.type == "istio-cni"))' < "${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}")
      # Rewrite the config file atomically: write into a temp file in the same directory then rename.
      echo "${CNI_CONF_DATA}" > "${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}.tmp"
      mv "${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}.tmp" "${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}"
    else
      echo "Removing istio-cni net.d conf file: ${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}"
      rm "${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}"
    fi
  fi
  if [ -e "${MOUNTED_CNI_NET_DIR}/${KUBECFG_FILE_NAME}" ]; then
    echo "Removing istio-cni kubeconfig file: ${MOUNTED_CNI_NET_DIR}/${KUBECFG_FILE_NAME}"
    rm "${MOUNTED_CNI_NET_DIR}/${KUBECFG_FILE_NAME}"
  fi
  rm_bin_files
  echo "Exiting."
}

# The directory on the host where CNI networks are installed. Defaults to
# /etc/cni/net.d, but can be overridden by setting CNI_NET_DIR.  This is used
# for populating absolute paths in the CNI network config to assets
# which are installed in the CNI network config directory.
HOST_CNI_NET_DIR=${CNI_NET_DIR:-/etc/cni/net.d}
MOUNTED_CNI_NET_DIR=${MOUNTED_CNI_NET_DIR:-/host/etc/cni/net.d}

CNI_CONF_NAME_OVERRIDE=${CNI_CONF_NAME:-}

# default to first file in `ls` output
# if dir is empty, default to a filename that is not likely to be lexicographically first in the dir
CNI_CONF_NAME=${CNI_CONF_NAME:-$(find_cni_conf_file)}
CNI_CONF_NAME=${CNI_CONF_NAME:-YYY-istio-cni.conflist}
KUBECFG_FILE_NAME=${KUBECFG_FILE_NAME:-ZZZ-istio-cni-kubeconfig}
CFGCHECK_INTERVAL=${CFGCHECK_INTERVAL:-1}

# Whether the Istio CNI plugin should be installed as a chained plugin (defaults to true) or as a standalone plugin (when false)
CHAINED_CNI_PLUGIN=${CHAINED_CNI_PLUGIN:-true}

trap exit_graceful SIGINT
trap exit_graceful SIGTERM
trap cleanup EXIT

# Choose which default cni binaries should be copied
SKIP_CNI_BINARIES=${SKIP_CNI_BINARIES:-""}
SKIP_CNI_BINARIES=",$SKIP_CNI_BINARIES,"
UPDATE_CNI_BINARIES=${UPDATE_CNI_BINARIES:-"true"}

# Place the new binaries if the directory is writable.
for dir in /host/opt/cni/bin /host/secondary-bin-dir
do
  if [ ! -w "$dir" ];
  then
    echo "$dir is non-writable, skipping"
    continue
  fi
  for path in /opt/cni/bin/*;
  do
    filename=$(basename "$path")
    tmp=",$filename,"
    if [ "${SKIP_CNI_BINARIES#*$tmp}" != "$SKIP_CNI_BINARIES" ];
    then
      echo "$filename is in SKIP_CNI_BINARIES, skipping"
      continue
    fi
    if [ "${UPDATE_CNI_BINARIES}" != "true" ] && [ -f "$dir/$filename" ];
    then
      echo "$dir/$filename is already here and UPDATE_CNI_BINARIES isn't true, skipping"
      continue
    fi
    # Copy files atomically by first copying into the same directory then renaming.
    # shellcheck disable=SC2015
    cp "${path}" "${dir}/${filename}.tmp" && mv "${dir}/${filename}.tmp" "${dir}/${filename}" || exit_with_error "Failed to copy $path into $dir. This may be caused by selinux configuration on the host."
  done

  echo "Wrote Istio CNI binaries to $dir."
done

# Create a temp file in the same directory as the target, in order for the final rename to be atomic.
TMP_CONF="${MOUNTED_CNI_NET_DIR}/istio-cni.conf.tmp"
true > "${TMP_CONF}"
# If specified, overwrite the network configuration file.
: "${CNI_NETWORK_CONFIG_FILE:=}"
: "${CNI_NETWORK_CONFIG:=}"
if [ -e "${CNI_NETWORK_CONFIG_FILE}" ]; then
  echo "Using CNI config template from ${CNI_NETWORK_CONFIG_FILE}."
  cp "${CNI_NETWORK_CONFIG_FILE}" "${TMP_CONF}"
elif [ -n "${CNI_NETWORK_CONFIG}" ]; then
  echo "Using CNI config template from CNI_NETWORK_CONFIG environment variable."
  cat >"${TMP_CONF}" <<EOF
${CNI_NETWORK_CONFIG}
EOF
fi


SERVICE_ACCOUNT_PATH=/var/run/secrets/kubernetes.io/serviceaccount
KUBE_CA_FILE=${KUBE_CA_FILE:-$SERVICE_ACCOUNT_PATH/ca.crt}
SKIP_TLS_VERIFY=${SKIP_TLS_VERIFY:-false}
# Pull out service account token.
SERVICEACCOUNT_TOKEN=$(cat "$SERVICE_ACCOUNT_PATH/token")

# Check if we're running as a k8s pod.
if [ -f "$SERVICE_ACCOUNT_PATH/token" ]; then
  # We're running as a k8d pod - expect some variables.
  if [ -z "${KUBERNETES_SERVICE_HOST}" ]; then
    echo "KUBERNETES_SERVICE_HOST not set"; exit 1;
  fi
  if [ -z "${KUBERNETES_SERVICE_PORT}" ]; then
    echo "KUBERNETES_SERVICE_PORT not set"; exit 1;
  fi

  if [ "$SKIP_TLS_VERIFY" = "true" ]; then
    TLS_CFG="insecure-skip-tls-verify: true"
  elif [ -f "$KUBE_CA_FILE" ]; then
    TLS_CFG="certificate-authority-data: "$(base64 < "$KUBE_CA_FILE" | tr -d '\n')
  fi

  # Write a kubeconfig file for the CNI plugin.  Do this
  # to skip TLS verification for now.  We should eventually support
  # writing more complete kubeconfig files. This is only used
  # if the provided CNI network config references it.
  # Create / overwrite this file atomically.
  touch "${MOUNTED_CNI_NET_DIR}/${KUBECFG_FILE_NAME}.tmp"
  chmod "${KUBECONFIG_MODE:-600}" "${MOUNTED_CNI_NET_DIR}/${KUBECFG_FILE_NAME}.tmp"
  cat > "${MOUNTED_CNI_NET_DIR}/${KUBECFG_FILE_NAME}.tmp" <<EOF
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
  mv "${MOUNTED_CNI_NET_DIR}/${KUBECFG_FILE_NAME}.tmp" "${MOUNTED_CNI_NET_DIR}/${KUBECFG_FILE_NAME}"

fi


# Insert any of the supported "auto" parameters.
sed -e "s/__KUBERNETES_SERVICE_HOST__/${KUBERNETES_SERVICE_HOST}/g" \
    -e "s/__KUBERNETES_SERVICE_PORT__/${KUBERNETES_SERVICE_PORT}/g" \
    -e "s/__KUBERNETES_NODE_NAME__/${KUBERNETES_NODE_NAME:-$(hostname)}/g" \
    -e "s/__KUBECONFIG_FILENAME__/${KUBECFG_FILE_NAME}/g" \
    -e "s~__KUBECONFIG_FILEPATH__~${HOST_CNI_NET_DIR}/${KUBECFG_FILE_NAME}~g" \
    -e "s~__LOG_LEVEL__~${LOG_LEVEL:-warn}~g" \
    -i "${TMP_CONF}"

CNI_OLD_CONF_NAME=${CNI_OLD_CONF_NAME:-${CNI_CONF_NAME}}

# Log the config file before inserting service account token.
# This way auth token is not visible in the logs.
echo -n "CNI config: "
cat "${TMP_CONF}"

sed -e "s/__SERVICEACCOUNT_TOKEN__/${SERVICEACCOUNT_TOKEN:-}/g" -i "${TMP_CONF}"

if [ "${CHAINED_CNI_PLUGIN}" == "true" ]; then
  if [ ! -e "${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}" ] && [ "${CNI_CONF_NAME: -5}" = ".conf" ] && [ -e "${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}list" ]; then
      echo "${CNI_CONF_NAME} doesn't exist, but ${CNI_CONF_NAME}list does; Using it instead."
      CNI_CONF_NAME="${CNI_CONF_NAME}list"
  fi

  if [ -e "${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}" ]; then
      # This section overwrites an existing plugins list entry to for istio-cni
      CNI_TMP_CONF_DATA=$(cat "${TMP_CONF}")
      CNI_CONF_DATA=$(jq --argjson CNI_TMP_CONF_DATA "$CNI_TMP_CONF_DATA" -f /filter.jq < "${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}")
      echo "${CNI_CONF_DATA}" > "${TMP_CONF}"
  fi

  # If the old config filename ends with .conf, rename it to .conflist, because it has changed to be a list
  if [ "${CNI_CONF_NAME: -5}" = ".conf" ]; then
      echo "Renaming ${CNI_CONF_NAME} extension to .conflist"
      CNI_CONF_NAME="${CNI_CONF_NAME}list"
  fi

  # Delete old CNI config files for upgrades.
  if [ "${CNI_CONF_NAME}" != "${CNI_OLD_CONF_NAME}" ]; then
      rm -f "${MOUNTED_CNI_NET_DIR}/${CNI_OLD_CONF_NAME}"
  fi
fi

# Move the temporary CNI config into place.
mv "${TMP_CONF}" "${MOUNTED_CNI_NET_DIR}/${CNI_CONF_NAME}" || \
  exit_with_error "Failed to mv files. This may be caused by selinux configuration on the host, or something else."

echo "Created CNI config ${CNI_CONF_NAME}"

# Unless told otherwise, sleep forever.
# This prevents Kubernetes from restarting the pod repeatedly.
should_sleep=${SLEEP:-"true"}
echo "Done configuring CNI.  Sleep=$should_sleep"
while [ "$should_sleep" = "true" ]; do
  sleep "$CFGCHECK_INTERVAL"
  check_install
done

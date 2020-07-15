function remove_istio_cni() {
  local cni_config_file=$1
  local cni_conf_data
  cni_conf_data=$(jq 'del( .plugins[]? | select(.type == "istio-cni"))' < "${cni_config_file}")
  # Rewrite the config file atomically: write into a temp file in the same directory then rename.
  cat > "${cni_config_file}.tmp" <<EOF
${cni_conf_data}
EOF
  mv "${cni_config_file}.tmp" "${cni_config_file}"
}

echo "Removing istio-cni config from CNI chain config in $1"
remove_istio_cni "$1"
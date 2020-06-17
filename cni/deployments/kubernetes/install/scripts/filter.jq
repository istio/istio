if has("type") then
   .plugins = [.]
   | del(.plugins[0].cniVersion)
   | to_entries
   | map(select(.key=="plugins"))
   | from_entries
   | .plugins += [$CNI_TMP_CONF_DATA]
   | .name = "k8s-pod-network"
   | .cniVersion = "0.3.0"
else
  del(.plugins[]? | select(.type == "istio-cni"))
  | .plugins += [$CNI_TMP_CONF_DATA]
end
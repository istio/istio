{{ define "accesslist.yaml.tpl" }}
allowed:
    - spiffe://cluster.local/ns/{{ .Release.Namespace }}/sa/istio-mixer-service-account
{{- end }}

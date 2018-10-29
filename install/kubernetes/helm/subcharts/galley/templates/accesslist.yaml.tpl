{{ define "accesslist.yaml.tpl" }}
allowed:
    - spiffe://cluster.local/ns/{{ .Release.Namespace }}/sa/istio-mixer-service-account
    - spiffe://cluster.local/ns/{{ .Release.Namespace }}/sa/istio-pilot-service-account
{{- end }}

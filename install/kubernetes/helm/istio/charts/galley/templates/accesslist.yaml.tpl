{{ define "accesslist.yaml.tpl" }}
allowed:
    - spiffe://{{ .Values.global.identityDomain }}/ns/{{ .Release.Namespace }}/sa/istio-mixer-service-account
    - spiffe://{{ .Values.global.identityDomain }}/ns/{{ .Release.Namespace }}/sa/istio-pilot-service-account
{{- end }}

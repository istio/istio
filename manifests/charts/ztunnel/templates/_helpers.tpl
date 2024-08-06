{{- define "istio-labels" -}}
app.kubernetes.io/name: ztunnel
{{ include "istio.labels" . }}
{{- end -}}

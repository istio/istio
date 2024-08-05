{{- define "gateway.labels" -}}
app.kubernetes.io/name: istiod
{{ include "istio.labels" . }}
{{ include "gateway.selectorLabels" . }}
{{- range $key, $val := .Values.labels }}
{{- if and (ne $key "app") (ne $key "istio") }}
{{ $key | quote }}: {{ $val | quote }}
{{- end }}
{{- end }}
{{- end }}

{{- define "gateway.selectorLabels" -}}
app: {{ (.Values.labels.app | quote) | default (include "gateway.name" .) }}
istio: {{ (.Values.labels.istio | quote) | default (include "gateway.name" . | trimPrefix "istio-") }}
{{- end }}


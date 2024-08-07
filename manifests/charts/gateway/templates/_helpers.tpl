{{- define "gateway.name" -}}
{{- if eq .Release.Name "RELEASE-NAME" -}}
  {{- .Values.name | default "istio-ingressgateway" -}}
{{- else -}}
  {{- .Values.name | default .Release.Name | default "istio-ingressgateway" -}}
{{- end -}}
{{- end }}

{{- define "gateway.labels" -}}
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

{{/*
Keep sidecar injection labels together
https://istio.io/latest/docs/setup/additional-setup/sidecar-injection/#controlling-the-injection-policy
*/}}
{{- define "gateway.sidecarInjectionLabels" -}}
sidecar.istio.io/inject: "true"
{{- with .Values.revision }}
istio.io/rev: {{ . | quote }}
{{- end }}
{{- end }}

{{- define "gateway.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- .Values.serviceAccount.name | default (include "gateway.name" .)    }}
{{- else }}
{{- .Values.serviceAccount.name | default "default" }}
{{- end }}
{{- end }}

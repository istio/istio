{{- define "gateway.name" -}}
{{- if eq .Release.Name "RELEASE-NAME" -}}
  {{- .Values.name | default "istio-ingressgateway" -}}
{{- else -}}
  {{- .Values.name | default .Release.Name | default "istio-ingressgateway" -}}
{{- end -}}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "gateway.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "gateway.labels" -}}
helm.sh/chart: {{ include "gateway.chart" . }}
{{ include "gateway.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- range $key, $val := .Values.labels }}
{{ $key }}: {{ $val | quote }}
{{- end }}
{{- end }}

{{- define "gateway.selectorLabels" -}}
app: {{ include "gateway.name" . }}
istio: {{ (include "gateway.name" .) | trimPrefix "istio-" }}
app.kubernetes.io/name: {{ include "gateway.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "gateway.annotations" -}}
{{- range $key, $val := .Values.annotations }}
{{ $key }}: {{ $val | quote }}
{{- end }}
{{- end }}

{{- define "gateway.serviceAccountName" -}}
{{- default .Values.serviceAccount.name (include "gateway.name" .)  }}
{{- end }}

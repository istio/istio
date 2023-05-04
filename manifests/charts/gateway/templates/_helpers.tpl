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
app.kubernetes.io/name: {{ include "gateway.name" . }}
{{- range $key, $val := .Values.labels }}
{{- if not (or (eq $key "app") (eq $key "istio")) }}
{{ $key | quote }}: {{ $val | quote }}
{{- end }}
{{- end }}
{{- end }}

{{- define "gateway.podLabels" -}}
{{ include "gateway.selectorLabels" . }}
{{- range $key, $val := .Values.labels }}
{{- if not (or (eq $key "app") (eq $key "istio")) }}
{{ $key | quote }}: {{ $val | quote }}
{{- end }}
{{- end }}
{{- end }}

{{- define "gateway.selectorLabels" -}}
{{- if hasKey .Values.labels "app" }}
{{- with .Values.labels.app }}app: {{.|quote}}
{{- end}}
{{- else }}app: {{ include "gateway.name" . }}
{{- end }}
{{- if hasKey .Values.labels "istio" }}
{{- with .Values.labels.istio }}
istio: {{.|quote}}
{{- end}}
{{- else }}
istio: {{ include "gateway.name" . | trimPrefix "istio-" }}
{{- end }}
{{- end }}

{{- define "gateway.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- .Values.serviceAccount.name | default (include "gateway.name" .)    }}
{{- else }}
{{- .Values.serviceAccount.name | default "default" }}
{{- end }}
{{- end }}

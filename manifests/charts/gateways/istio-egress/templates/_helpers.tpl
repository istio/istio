{{- define "istio-egress.name" -}}
{{- default .Chart.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "istio-egress.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "istio-egress.labels" -}}
helm.sh/chart: {{ include "istio-egress.chart" . }}
{{ include "istio-egress.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "istio-egress.selectorLabels" -}}
app.kubernetes.io/name: {{ include "istio-egress.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

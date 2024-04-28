{{- define "gateway.name" -}}
{{- if eq .Release.Name "RELEASE-NAME" -}}
  {{- .Values.name | default "istio-ingressgateway" -}}
{{- else -}}
  {{- .Values.name | default .Release.Name | default "istio-ingressgateway" -}}
{{- end -}}
{{- end }}

{{/*
Create chart name and version as used by the helm.sh/chart label.
*/}}
{{- define "gateway.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "gateway.labels" -}}
helm.sh/chart: {{ include "gateway.chart" . }}
{{ include "gateway.selectorLabels" . }}
app.kubernetes.io/name: {{ include "gateway.name" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
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

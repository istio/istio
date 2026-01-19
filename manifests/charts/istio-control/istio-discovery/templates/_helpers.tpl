{{/* Default Prometheus is enabled if its enabled and there are no config overrides set */}}
{{ define "default-prometheus" }}
{{- and
  (not .Values.meshConfig.defaultProviders)
  .Values.telemetry.enabled .Values.telemetry.v2.enabled .Values.telemetry.v2.prometheus.enabled
}}
{{- end }}

{{/* SD has metrics and logging split. Default metrics are enabled if SD is enabled */}}
{{ define "default-sd-metrics" }}
{{- and
  (not .Values.meshConfig.defaultProviders)
  .Values.telemetry.enabled .Values.telemetry.v2.enabled .Values.telemetry.v2.stackdriver.enabled
}}
{{- end }}

{{/* SD has metrics and logging split. */}}
{{ define "default-sd-logs" }}
{{- and
  (not .Values.meshConfig.defaultProviders)
  .Values.telemetry.enabled .Values.telemetry.v2.enabled .Values.telemetry.v2.stackdriver.enabled
}}
{{- end }}

{{/*
Render resource requirements, omitting any nil values.
*/}}
{{- define "istiod.resources" -}}
{{- with .limits }}{{- if or .cpu .memory }}
limits:
  {{- with .cpu }}
  cpu: {{ . }}
  {{- end }}
  {{- with .memory }}
  memory: {{ . }}
  {{- end }}
{{- end }}{{- end }}
{{- with .requests }}{{- if or .cpu .memory }}
requests:
  {{- with .cpu }}
  cpu: {{ . }}
  {{- end }}
  {{- with .memory }}
  memory: {{ . }}
  {{- end }}
{{- end }}{{- end }}
{{- end -}}

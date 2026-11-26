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
{{- range $key := list "limits" "requests" }}
  {{- $resources := index $ $key }}
  {{- if $resources }}
    {{- $hasValues := false }}
    {{- range $name, $value := $resources }}
      {{- if $value }}
        {{- $hasValues = true }}
      {{- end }}
    {{- end }}
    {{- if $hasValues }}
{{ $key }}:
      {{- range $name, $value := $resources }}
        {{- if $value }}
  {{ $name }}: {{ $value }}
        {{- end }}
      {{- end }}
    {{- end }}
  {{- end }}
{{- end }}
{{- end -}}

{{- define "istiod.resourceGroup" -}}
{{- $group := .Values.resourceGroup | default "default" -}}
{{- if not (has $group (list "default" "shared" "workload")) -}}
{{- fail (printf "resourceGroup must be one of default, shared or workload, got %q" $group) -}}
{{- end -}}
{{- $group -}}
{{- end }}

{{- define "istiod.renderShared" -}}
{{- if ne (include "istiod.resourceGroup" .) "workload" -}}true{{- end -}}
{{- end }}

{{- define "istiod.renderWorkload" -}}
{{- if ne (include "istiod.resourceGroup" .) "shared" -}}true{{- end -}}
{{- end }}

{{- define "istiod.workloadName" -}}
{{- if eq (include "istiod.resourceGroup" .) "workload" -}}
{{- .Release.Name -}}
{{- else -}}
istiod{{- if not (eq .Values.revision "") }}-{{ .Values.revision }}{{- end }}
{{- end -}}
{{- end }}

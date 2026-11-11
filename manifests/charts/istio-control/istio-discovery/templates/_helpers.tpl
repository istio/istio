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

{{- define "istiod.zones" -}}
{{- if .Values.zonalDeployments.enabled -}}
{{- if not .Values.zonalDeployments.zones -}}
{{- fail "zonalDeployments.zones must list at least one zone when zonalDeployments.enabled is true" -}}
{{- end -}}
{{- range .Values.zonalDeployments.zones -}}
{{- if not (and (kindIs "map" .) .name) -}}
{{- fail "every entry in zonalDeployments.zones must be a map with a name" -}}
{{- end -}}
{{- end -}}
{{- toJson .Values.zonalDeployments.zones -}}
{{- else -}}
{{- toJson (list (dict "name" "")) -}}
{{- end -}}
{{- end -}}

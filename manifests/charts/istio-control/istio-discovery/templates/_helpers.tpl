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

{{- define "istio-image" -}}
    {{- if contains "/" .Values.pilot.image }}
        image: "{{ .Values.pilot.image }}"
    {{- else }}
        image: "{{ .Values.pilot.hub | default .Values.global.hub }}/{{ .Values.pilot.image | default "pilot" }}:{{ .Values.pilot.tag | default .Values.global.tag }}{{with (.Values.pilot.variant | default .Values.global.variant)}}-{{.}}{{end}}"
    {{- end }}
    {{- if or .Values.pilot.pullPolicy .Values.global.imagePullPolicy }}
        imagePullPolicy: {{ .Values.pilot.pullPolicy | default .Values.global.imagePullPolicy }}
    {{- end }}
{{- end }}

{{/* Prometheus is enabled if its enabled and there are no config overrides set */}}
{{ define "prometheus" }}
{{- and
  (not .Values.meshConfig.defaultProviders)
  .Values.telemetry.enabled .Values.telemetry.v2.enabled .Values.telemetry.v2.prometheus.enabled
  (not (or
    .Values.telemetry.v2.prometheus.configOverride.gateway
    .Values.telemetry.v2.prometheus.configOverride.inboundSidecar
    .Values.telemetry.v2.prometheus.configOverride.outboundSidecar
  )) }}
{{- end }}

{{/* SD has metrics and logging split. Metrics are enabled if SD is enabled and there are no config overrides set */}}
{{ define "sd-metrics" }}
{{- and
  (not .Values.meshConfig.defaultProviders)
  .Values.telemetry.enabled .Values.telemetry.v2.enabled .Values.telemetry.v2.stackdriver.enabled
  (not (or
    .Values.telemetry.v2.stackdriver.configOverride
    .Values.telemetry.v2.stackdriver.disableOutbound ))
}}
{{- end }}

{{/* SD has metrics and logging split. */}}
{{ define "sd-logs" }}
{{- and
  (not .Values.meshConfig.defaultProviders)
  .Values.telemetry.enabled .Values.telemetry.v2.enabled .Values.telemetry.v2.stackdriver.enabled
  (not (or
    .Values.telemetry.v2.stackdriver.configOverride
    (has .Values.telemetry.v2.stackdriver.outboundAccessLogging (list "" "ERRORS_ONLY"))
    (has .Values.telemetry.v2.stackdriver.inboundAccessLogging (list "" "ALL"))
    .Values.telemetry.v2.stackdriver.disableOutbound ))
}}
{{- end }}
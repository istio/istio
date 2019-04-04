{{/* tolerations - https://kubernetes.io/docs/concepts/configuration/taint-and-toleration/ */}}

{{- define "gatewaytolerations" }}
{{- if or .root.Values.global.defaultTolerations .tolerations }}
{{- $tolerations := default .root.Values.global.defaultTolerations .tolerations }}
tolerations:
{{ toYaml $tolerations | indent 2 }}
{{- end }}
{{- end }}

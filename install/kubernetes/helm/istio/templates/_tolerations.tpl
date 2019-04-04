{{/* tolerations - https://kubernetes.io/docs/concepts/configuration/taint-and-toleration/ */}}

{{- define "tolerations" }}
{{- if or .Values.global.defaultTolerations .Values.tolerations }}
{{- $tolerations := default .Values.global.defaultTolerations .Values.tolerations }}
tolerations:
{{ toYaml $tolerations | indent 2 }}
{{- end }}
{{- end }}

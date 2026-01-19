{{ define "ztunnel.release-name" }}{{ .Values.resourceName| default "ztunnel" }}{{ end }}

{{/*
Render resource requirements, omitting any nil values.
*/}}
{{- define "ztunnel.resources" -}}
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

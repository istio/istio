{{/*
Render resource requirements, omitting any nil values.
*/}}
{{- define "istio-ingress.resources" -}}
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

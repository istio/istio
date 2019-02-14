{{- define "podDisruptionBudget.spec" }}
{{- if .minAvailable }}
  minAvailable: {{ .minAvailable }}
{{- end }}
{{- if .maxUnavailable }}
  maxUnavailable: {{ .maxUnavailable }}
{{- end }}
{{- end }}
{{- define "podDisruptionBudget.spec" }}
{{- if .minAvailable }}
  minAvailable: {{ .minAvailable }}
{{- else if .maxUnavailable }}
  maxUnavailable: {{ .maxUnavailable }}
{{- end }}
{{- end }}

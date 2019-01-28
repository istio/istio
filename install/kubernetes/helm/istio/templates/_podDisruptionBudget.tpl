{{- define "podDisruptionBudget.spec" }}
  minAvailable: 1
  maxUnavailable: 1
{{- end }}

{{- define "istio-labels" }}
    app.kubernetes.io/name: ztunnel
    {{- if .Release.Service }}
    app.kubernetes.io/managed-by: {{ .Release.Service }}
    {{- end }}
    {{- if .Release.Name}}
    app.kubernetes.io/instance: {{ .Release.Name }}
    {{- end }}
    app.kubernetes.io/part-of: istio
{{- end }}

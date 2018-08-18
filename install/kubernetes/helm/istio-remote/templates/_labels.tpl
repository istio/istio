{{- define "common_labels" }}
    chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
    release: {{ .Release.Name }}
    version: {{ .Chart.Version }}
    heritage: {{ .Release.Service }}
{{- end }}
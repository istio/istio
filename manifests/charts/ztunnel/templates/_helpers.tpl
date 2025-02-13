{{ define "ztunnel.release-name" }}{{ .Values.resourceName| default .Release.Name }}{{ end }}

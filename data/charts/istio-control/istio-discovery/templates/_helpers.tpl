{{/* vim: set filetype=mustache: */}}
{{- define "istio.configmap.checksum" -}}
{{- print $.Template.BasePath "/configmap.yaml" | sha256sum -}}
{{- end -}}

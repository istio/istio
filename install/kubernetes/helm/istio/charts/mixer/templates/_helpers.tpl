{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "mixer.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
*/}}
{{- define "mixer.fullname" -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Service account name.
*/}}
{{- define "mixer.serviceAccountName" -}}
{{- if .Values.global.rbacEnabled -}}
{{- template "mixer.fullname" . -}}-service-account
{{- else }}
{{- .Values.serviceAccountName | trunc 63 | trimSuffix "-" -}}-service-account
{{- end -}}
{{- end -}}

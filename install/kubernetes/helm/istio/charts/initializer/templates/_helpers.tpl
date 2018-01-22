{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "initializer.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
*/}}
{{- define "initializer.fullname" -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Service account name.
*/}}
{{- define "initializer.serviceAccountName" -}}
{{- if .Values.global.rbac.create -}}
{{- template "initializer.fullname" . -}}
{{- else }}
{{- .Values.rbac.serviceAccountName | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

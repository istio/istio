{{ define "ztunnel.release-name" }}{{ .Values.resourceName| default "ztunnel" }}{{ end }}

{{ define "ztunnel.service-account-name" }}{{ .Values.serviceAccount.name | default (include "ztunnel.release-name" .) }}{{ end }}

{{/*
Merge common annotations with revision
*/}}
{{- define "ztunnel.common-annotations" -}}
{{- $mergedAnnotations := dict }}
{{- if .Values.annotations }}
  {{- $mergedAnnotations = merge $mergedAnnotations .Values.annotations }}
{{- end }}
{{- if .Values.revision }}
  {{- $mergedAnnotations = set $mergedAnnotations "istio.io/rev" .Values.revision }}
{{- end }}
{{- toYaml $mergedAnnotations }}
{{- end }}

{{/*
Merge annotations for ServiceAccount (includes serviceAccount.annotations)
*/}}
{{- define "ztunnel.serviceaccount-annotations" -}}
{{- $commonAnnotations := include "ztunnel.common-annotations" . | fromYaml }}
{{- $mergedAnnotations := $commonAnnotations | default dict }}
{{- if .Values.serviceAccount.annotations }}
  {{- $mergedAnnotations = merge $mergedAnnotations .Values.serviceAccount.annotations }}
{{- end }}
{{- toYaml $mergedAnnotations }}
{{- end }}

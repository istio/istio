{{/* For and validating webhook use proxy service name if set */}}
{{- define "webhookServiceName" }}
{{- $serviceName := .Values.istiodWebhookProxy.serviceName }}
{{- if not (eq $serviceName "") }}
{{- $serviceName }}
{{- else }}
{{- printf "istiod-%s" .Values.defaultRevision | trimSuffix "-default" }}
{{- end }}
{{- end }}

{{/* For validating webhook use proxy namespace if set */}}
{{- define "webhookNamespace" }}
{{- $namespace := .Values.istiodWebhookProxy.namespace }}
{{- ternary $namespace .Values.global.istioNamespace (not (eq $namespace "")) }}
{{- end }}

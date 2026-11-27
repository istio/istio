{{- define "gateway.name" -}}
{{- if eq .Release.Name "RELEASE-NAME" -}}
  {{- .Values.name | default "istio-ingressgateway" -}}
{{- else -}}
  {{- .Values.name | default .Release.Name | default "istio-ingressgateway" -}}
{{- end -}}
{{- end }}

{{- define "gateway.labels" -}}
{{ include "gateway.selectorLabels" . }}
{{- $selectorLabels := .Values.selectorLabels | default (dict) -}}
{{- range $key, $val := .Values.labels }}
{{- if and (ne $key "app") (ne $key "istio") (not (hasKey $selectorLabels $key)) }}
{{ $key | quote }}: {{ $val | quote }}
{{- end }}
{{- end }}
{{- end }}

{{- define "gateway.serviceSelectorLabels" -}}
{{- $app := dig "app" nil (.Values.service.selectorLabels | default (dict)) | default (dig "app" nil (.Values.selectorLabels | default (dict))) | default .Values.labels.app -}}
{{- $istio := dig "istio" nil (.Values.service.selectorLabels | default (dict)) | default (dig "istio" nil (.Values.selectorLabels | default (dict))) | default .Values.labels.istio -}}
app: {{ ($app | quote) | default (include "gateway.name" .) }}
istio: {{ ($istio | quote) | default (include "gateway.name" . | trimPrefix "istio-") }}
{{- $extraServiceLabels := dict -}}
{{- range $key, $val := .Values.selectorLabels }}
{{- if and (ne $key "app") (ne $key "istio") }}
{{- $_ := set $extraServiceLabels $key $val -}}
{{- end }}
{{- end }}
{{- range $key, $val := .Values.service.selectorLabels }}
{{- if and (ne $key "app") (ne $key "istio") }}
{{- $_ := set $extraServiceLabels $key $val -}}
{{- end }}
{{- end }}
{{- with $extraServiceLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{- define "gateway.selectorLabels" -}}
{{- $app := dig "app" nil (.Values.selectorLabels | default (dict)) | default .Values.labels.app -}}
{{- $istio := dig "istio" nil (.Values.selectorLabels | default (dict)) | default .Values.labels.istio -}}
app: {{ ($app | quote) | default (include "gateway.name" .) }}
istio: {{ ($istio | quote) | default (include "gateway.name" . | trimPrefix "istio-") }}
{{- range $key, $val := .Values.selectorLabels }}
{{- if and (ne $key "app") (ne $key "istio") }}
{{ $key | quote }}: {{ $val | quote }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Keep sidecar injection labels together
https://istio.io/latest/docs/setup/additional-setup/sidecar-injection/#controlling-the-injection-policy
*/}}
{{- define "gateway.sidecarInjectionLabels" -}}
sidecar.istio.io/inject: "true"
{{- with .Values.revision }}
istio.io/rev: {{ . | quote }}
{{- end }}
{{- end }}

{{- define "gateway.serviceAccountName" -}}
{{- if or .Values.serviceAccount.create (eq (include "gateway.resourceGroup" .) "workload") }}
{{- .Values.serviceAccount.name | default (include "gateway.name" .)    }}
{{- else }}
{{- .Values.serviceAccount.name | default "default" }}
{{- end }}
{{- end }}

{{/*
Render a single network gateway port entry with validation.
Expects a dict with keys: ports (the networkGatewayPorts map), name (port name), defaultTargetPort (fallback).
*/}}
{{- define "gateway.networkGatewayPort" -}}
{{- $cfg := index .ports .name | required (printf "networkGatewayPorts.%s is required when networkGateway is set" .name) -}}
- name: {{ .name }}
  port: {{ $cfg.port }}
  targetPort: {{ $cfg.targetPort | default .defaultTargetPort }}
  protocol: {{ $cfg.protocol | default "TCP" }}
{{- with $cfg.nodePort }}
  nodePort: {{ . }}
{{- end }}
{{- end -}}

{{/*
Render resource requirements, omitting any nil values.
*/}}
{{- define "gateway.resources" -}}
{{- range $key := list "limits" "requests" }}
  {{- $resources := index $ $key }}
  {{- if $resources }}
    {{- $hasValues := false }}
    {{- range $name, $value := $resources }}
      {{- if $value }}
        {{- $hasValues = true }}
      {{- end }}
    {{- end }}
    {{- if $hasValues }}
{{ $key }}:
      {{- range $name, $value := $resources }}
        {{- if $value }}
  {{ $name }}: {{ $value }}
        {{- end }}
      {{- end }}
    {{- end }}
  {{- end }}
{{- end }}
{{- end -}}

{{- define "gateway.resourceGroup" -}}
{{- $group := .Values.resourceGroup | default "default" -}}
{{- if not (has $group (list "default" "shared" "workload")) -}}
{{- fail (printf "resourceGroup must be one of default, shared or workload, got %q" $group) -}}
{{- end -}}
{{- $group -}}
{{- end }}

{{- define "gateway.renderShared" -}}
{{- if ne (include "gateway.resourceGroup" .) "workload" -}}true{{- end -}}
{{- end }}

{{- define "gateway.renderWorkload" -}}
{{- if ne (include "gateway.resourceGroup" .) "shared" -}}true{{- end -}}
{{- end }}

{{- define "gateway.workloadName" -}}
{{- if eq (include "gateway.resourceGroup" .) "workload" -}}
{{- if not .Values.name -}}
{{- fail "name must be set to the gateway name of the shared release when resourceGroup is workload" -}}
{{- end -}}
{{- .Release.Name -}}
{{- else -}}
{{- include "gateway.name" . -}}
{{- end -}}
{{- end }}

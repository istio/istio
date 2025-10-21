{{- define "gateway.name" -}}
{{- if eq .Release.Name "RELEASE-NAME" -}}
  {{- .Values.name | default "istio-ingressgateway" -}}
{{- else -}}
  {{- .Values.name | default .Release.Name | default "istio-ingressgateway" -}}
{{- end -}}
{{- end }}

{{- define "gateway.labels" -}}
{{ include "gateway.selectorLabels" . }}
{{- range $key, $val := .Values.labels }}
{{- if and (ne $key "app") (ne $key "istio") }}
{{ $key | quote }}: {{ $val | quote }}
{{- end }}
{{- end }}
{{- end }}

{{- define "gateway.selectorLabels" -}}
app: {{ (.Values.labels.app | quote) | default (include "gateway.name" .) }}
istio: {{ (.Values.labels.istio | quote) | default (include "gateway.name" . | trimPrefix "istio-") }}
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
{{- if .Values.serviceAccount.create }}
{{- .Values.serviceAccount.name | default (include "gateway.name" .)    }}
{{- else }}
{{- .Values.serviceAccount.name | default "default" }}
{{- end }}
{{- end }}

{{/*
Clean null values from a dictionary recursively
This is needed because Helm's merge functions can introduce null values
which Kubernetes interprets as 0 or causes validation errors
*/}}
{{- define "gateway.cleanNullValues" -}}
{{- $result := dict }}
{{- range $key, $val := . }}
  {{- if kindIs "map" $val }}
    {{- $cleaned := include "gateway.cleanNullValues" $val | fromYaml }}
    {{- if $cleaned }}
      {{- $_ := set $result $key $cleaned }}
    {{- end }}
  {{- else if ne $val nil }}
    {{- $_ := set $result $key $val }}
  {{- end }}
{{- end }}
{{- $result | toYaml }}
{{- end }}

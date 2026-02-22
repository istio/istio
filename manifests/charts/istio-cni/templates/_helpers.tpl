{{- define "name" -}}
    istio-cni
{{- end }}


{{- define "istio-tag" -}}
    {{ .Values.tag | default .Values.global.tag }}{{with (.Values.variant | default .Values.global.variant)}}-{{.}}{{end}}
{{- end }}

{{/*
Render resource requirements, omitting any nil values.
*/}}
{{- define "istio-cni.resources" -}}
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

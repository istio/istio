{{/*
Render resource requirements, omitting any nil values.
*/}}
{{- define "istio-ingress.resources" -}}
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

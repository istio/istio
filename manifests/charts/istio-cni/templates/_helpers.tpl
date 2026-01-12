{{- define "name" -}}
    istio-cni
{{- end }}


{{- define "istio-tag" -}}
    {{ .Values.tag | default .Values.global.tag }}{{with (.Values.variant | default .Values.global.variant)}}-{{.}}{{end}}
{{- end }}

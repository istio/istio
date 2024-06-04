{{- define "name" -}}
    istio-cni
{{- end }}


{{- define "istio-tag" -}}
    {{ .Values.cni.tag | default .Values.global.tag }}{{with (.Values.cni.variant | default .Values.global.variant)}}-{{.}}{{end}}
{{- end }}

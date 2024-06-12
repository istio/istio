{{- define "ztunnel.istio-image" -}}
    {{- if contains "/" .Values.image }}
        image: "{{ .Values.image }}"
    {{- else }}
        image: "{{ .Values.hub | default .Values.global.hub }}/{{ .Values.image | default "ztunnel" }}:{{ .Values.tag | default .Values.global.tag }}{{with (.Values.variant | default .Values.global.variant)}}-{{.}}{{end}}"
    {{- end }}
    {{- if or .Values.pullPolicy .Values.global.imagePullPolicy }}
        imagePullPolicy: {{ .Values.pullPolicy | default .Values.global.imagePullPolicy }}
    {{- end }}
{{- end }}

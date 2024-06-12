{{- define "istio-cni.istio-image" -}}
    {{- if contains "/" .Values.cni.image }}
        image: "{{ .Values.cni.image }}"
    {{- else }}
        image: "{{ .Values.cni.hub | default .Values.global.hub }}/{{ .Values.cni.image | default "istio-cni" }}:{{ .Values.cni.tag | default .Values.global.tag }}{{with (.Values.cni.variant | default .Values.global.variant)}}-{{.}}{{end}}"
    {{- end }}
    {{- if or .Values.cni.pullPolicy .Values.global.imagePullPolicy }}
        imagePullPolicy: {{ .Values.cni.pullPolicy | default .Values.global.imagePullPolicy }}
    {{- end }}
{{- end }}

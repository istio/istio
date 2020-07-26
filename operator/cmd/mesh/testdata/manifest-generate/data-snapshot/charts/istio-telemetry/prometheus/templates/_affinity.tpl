{{/* affinity - https://kubernetes.io/docs/concepts/configuration/assign-pod-node/ */}}

{{- define "nodeaffinity" }}
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
    {{- include "nodeAffinityRequiredDuringScheduling" . }}
    preferredDuringSchedulingIgnoredDuringExecution:
    {{- include "nodeAffinityPreferredDuringScheduling" . }}
{{- end }}

{{- define "nodeAffinityRequiredDuringScheduling" }}
      nodeSelectorTerms:
      - matchExpressions:
        - key: kubernetes.io/arch
          operator: In
          values:
        {{- range $key, $val := .Values.global.arch }}
          {{- if gt ($val | int) 0 }}
          - {{ $key | quote }}
          {{- end }}
        {{- end }}
        {{- $nodeSelector := default .Values.global.defaultNodeSelector .Values.prometheus.nodeSelector -}}
        {{- range $key, $val := $nodeSelector }}
        - key: {{ $key }}
          operator: In
          values:
          - {{ $val | quote }}
        {{- end }}
{{- end }}

{{- define "nodeAffinityPreferredDuringScheduling" }}
  {{- range $key, $val := .Values.global.arch }}
    {{- if gt ($val | int) 0 }}
    - weight: {{ $val | int }}
      preference:
        matchExpressions:
        - key: kubernetes.io/arch
          operator: In
          values:
          - {{ $key | quote }}
    {{- end }}
  {{- end }}
{{- end }}

{{- define "podAntiAffinity" }}
{{- if or .Values.prometheus.podAntiAffinityLabelSelector .Values.prometheus.podAntiAffinityTermLabelSelector}}
  podAntiAffinity:
    {{- if .Values.prometheus.podAntiAffinityLabelSelector }}
    requiredDuringSchedulingIgnoredDuringExecution:
    {{- include "podAntiAffinityRequiredDuringScheduling" . }}
    {{- end }}
    {{- if .Values.prometheus.podAntiAffinityTermLabelSelector }}
    preferredDuringSchedulingIgnoredDuringExecution:
    {{- include "podAntiAffinityPreferredDuringScheduling" . }}
    {{- end }}
{{- end }}
{{- end }}

{{- define "podAntiAffinityRequiredDuringScheduling" }}
    {{- range $index, $item := .Values.prometheus.podAntiAffinityLabelSelector }}
    - labelSelector:
        matchExpressions:
        - key: {{ $item.key }}
          operator: {{ $item.operator }}
          {{- if $item.values }}
          values:
          {{- $vals := split "," $item.values }}
          {{- range $i, $v := $vals }}
          - {{ $v | quote }}
          {{- end }}
          {{- end }}
      topologyKey: {{ $item.topologyKey }}
    {{- end }}
{{- end }}

{{- define "podAntiAffinityPreferredDuringScheduling" }}
    {{- range $index, $item := .Values.prometheus.podAntiAffinityTermLabelSelector }}
    - podAffinityTerm:
        labelSelector:
          matchExpressions:
          - key: {{ $item.key }}
            operator: {{ $item.operator }}
            {{- if $item.values }}
            values:
            {{- $vals := split "," $item.values }}
            {{- range $i, $v := $vals }}
            - {{ $v | quote }}
            {{- end }}
            {{- end }}
        topologyKey: {{ $item.topologyKey }}
      weight: 100
    {{- end }}
{{- end }}

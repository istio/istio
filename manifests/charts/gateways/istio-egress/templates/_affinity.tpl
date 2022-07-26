{{/* affinity - https://kubernetes.io/docs/concepts/configuration/assign-pod-node/ */}}

{{ define "nodeaffinity" }}
nodeAffinity:
  requiredDuringSchedulingIgnoredDuringExecution:
  {{- include "nodeAffinityRequiredDuringScheduling" . }}
  preferredDuringSchedulingIgnoredDuringExecution:
  {{- include "nodeAffinityPreferredDuringScheduling" . }}
{{- end }}

{{- define "nodeAffinityRequiredDuringScheduling" }}
  {{- $nodeSelector := default .global.defaultNodeSelector .nodeSelector -}}
  {{- if or .global.arch $nodeSelector }}
      nodeSelectorTerms:
      - matchExpressions:
        {{- range $key, $val := .global.arch }}
        - key: kubernetes.io/arch
          operator: In
          values:
          {{- if gt ($val | int) 0 }}
          - {{ $key | quote }}
          {{- end }}
        {{- end }}
        {{- range $key, $val := $nodeSelector }}
        - key: {{ $key }}
          operator: In
          values:
          - {{ $val | quote }}
        {{- end }}
  {{- end }}
{{- end }}

{{- define "nodeAffinityPreferredDuringScheduling" }}
  {{- range $key, $val := .global.arch }}
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
{{- if or .podAntiAffinityLabelSelector .podAntiAffinityTermLabelSelector}}
  podAntiAffinity:
    {{- if .podAntiAffinityLabelSelector }}
    requiredDuringSchedulingIgnoredDuringExecution:
    {{- include "podAntiAffinityRequiredDuringScheduling" . }}
    {{- end }}
    {{- if .podAntiAffinityTermLabelSelector }}
    preferredDuringSchedulingIgnoredDuringExecution:
    {{- include "podAntiAffinityPreferredDuringScheduling" . }}
    {{- end }}
{{- end }}
{{- end }}

{{- define "podAntiAffinityRequiredDuringScheduling" }}
    {{- range $index, $item := .podAntiAffinityLabelSelector }}
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
      {{- if $item.namespaces }}
      namespaces:
      {{- $ns := split "," $item.namespaces }}
      {{- range $i, $n := $ns }}
      - {{ $n | quote }}
      {{- end }}
      {{- end }}
    {{- end }}
{{- end }}

{{- define "podAntiAffinityPreferredDuringScheduling" }}
    {{- range $index, $item := .podAntiAffinityTermLabelSelector }}
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

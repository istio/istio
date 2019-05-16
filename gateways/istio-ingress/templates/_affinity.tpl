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
        - key: beta.kubernetes.io/arch
          operator: In
          values:
        {{- range $key, $val := .global.arch }}
          {{- if gt ($val | int) 0 }}
          - {{ $key }}
          {{- end }}
        {{- end }}
        {{- $nodeSelector := default .global.defaultNodeSelector .nodeSelector -}}
        {{- range $key, $val := $nodeSelector }}
        - key: {{ $key }}
          operator: In
          values:
          - {{ $val }}
        {{- end }}
{{- end }}

{{- define "nodeAffinityPreferredDuringScheduling" }}
  {{- range $key, $val := .global.arch }}
    {{- if gt ($val | int) 0 }}
    - weight: {{ $val | int }}
      preference:
        matchExpressions:
        - key: beta.kubernetes.io/arch
          operator: In
          values:
          - {{ $key }}
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
          - {{ $v }}
          {{- end }}
          {{- end }}
      topologyKey: {{ $item.topologyKey }}
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
            - {{ $v }}
            {{- end }}
            {{- end }}
        topologyKey: {{ $item.topologyKey }}
      weight: 100
    {{- end }}
{{- end }}

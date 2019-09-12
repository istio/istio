{{/* affinity - https://kubernetes.io/docs/concepts/configuration/assign-pod-node/ */}}

{{- define "gatewaynodeaffinity" }}
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
    {{- include "gatewayNodeAffinityRequiredDuringScheduling" . }}
    preferredDuringSchedulingIgnoredDuringExecution:
    {{- include "gatewayNodeAffinityPreferredDuringScheduling" . }}
{{- end }}

{{- define "gatewayNodeAffinityRequiredDuringScheduling" }}
      nodeSelectorTerms:
      - matchExpressions:
        - key: beta.kubernetes.io/arch
          operator: In
          values:
        {{- range $key, $val := .root.Values.global.arch }}
          {{- if gt ($val | int) 0 }}
          - {{ $key | quote }}
          {{- end }}
        {{- end }}
        {{- $nodeSelector := default .root.Values.global.defaultNodeSelector .nodeSelector -}}
        {{- range $key, $val := $nodeSelector }}
        - key: {{ $key }}
          operator: In
          values:
          - {{ $val | quote }}
        {{- end }}
{{- end }}

{{- define "gatewayNodeAffinityPreferredDuringScheduling" }}
  {{- range $key, $val := .root.Values.global.arch }}
    {{- if gt ($val | int) 0 }}
    - weight: {{ $val | int }}
      preference:
        matchExpressions:
        - key: beta.kubernetes.io/arch
          operator: In
          values:
          - {{ $key | quote }}
    {{- end }}
  {{- end }}
{{- end }}

{{- define "gatewaypodAntiAffinity" }}
{{- if or .podAntiAffinityLabelSelector .podAntiAffinityTermLabelSelector}}
  podAntiAffinity:
    {{- if .podAntiAffinityLabelSelector }}
    requiredDuringSchedulingIgnoredDuringExecution:
    {{- include "gatewaypodAntiAffinityRequiredDuringScheduling" . }}
    {{- end }}
    {{- if .podAntiAffinityTermLabelSelector }}
    preferredDuringSchedulingIgnoredDuringExecution:
    {{- include "gatewaypodAntiAffinityPreferredDuringScheduling" . }}
    {{- end }}
{{- end }}
{{- end }}

{{- define "gatewaypodAntiAffinityRequiredDuringScheduling" }}
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
    {{- end }}
{{- end }}

{{- define "gatewaypodAntiAffinityPreferredDuringScheduling" }}
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

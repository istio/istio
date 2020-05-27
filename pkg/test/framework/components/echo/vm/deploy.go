package vm

const deploymentYaml = `
{{- $cluster := .Cluster }}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ $.Service }}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: {{ $.Service }}
  template:
    metadata:
      labels:
        app: {{ $.Service }}
    spec:
{{- if $.ServiceAccount }}
      serviceAccountName: {{ $.Service }}
{{- end }}
      containers:
      - name: app
        image: {{ $.Hub }}/app_sidecar:{{ $.Tag }}
        imagePullPolicy: {{ $.PullPolicy }}
        securityContext:
          runAsUser: 1
        args:
          - --cluster
          - "{{ $cluster }}"
{{- range $i, $p := $.ContainerPorts }}
{{- if eq .Protocol "GRPC" }}
          - --grpc
{{- else if eq .Protocol "TCP" }}
          - --tcp
{{- else }}
          - --port
{{- end }}
          - "{{ $p.Port }}"
{{- end }}
        readinessProbe:
          httpGet:
            path: /
            port: 8080
          initialDelaySeconds: 1
          periodSeconds: 2
          failureThreshold: 10
        livenessProbe:
          tcpSocket:
            port: tcp-health-port
          initialDelaySeconds: 10
          periodSeconds: 10
          failureThreshold: 10`

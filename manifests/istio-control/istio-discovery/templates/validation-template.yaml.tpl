{{ define "validatingwebhookconfiguration.yaml.tpl" }}
apiVersion: admissionregistration.k8s.io/v1beta1
kind: ValidatingWebhookConfiguration
metadata:
  name: istio-galley # unchanged for now
  namespace: {{ .Release.Namespace }}
  labels:
    app: galley
    release: {{ .Release.Name }}
    istio: galley
webhooks:
  - name: validation.istio.io
    clientConfig:
      service:
        name: istio-pilot
        namespace: {{ .Release.Namespace }}
        path: "/validate"
        port: 443
      caBundle: ""
    rules:
      - operations:
        - CREATE
        - UPDATE
        apiGroups:
        - config.istio.io
        - rbac.istio.io
        - security.istio.io
        - authentication.istio.io
        - networking.istio.io
        apiVersions:
        - "*"
        resources:
        - "*"
    failurePolicy: Fail
    sideEffects: None
---
{{ end }}

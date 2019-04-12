{{ define "validatingwebhookconfiguration.yaml.tpl" }}
apiVersion: admissionregistration.k8s.io/v1beta1
kind: ValidatingWebhookConfiguration
metadata:
  name: istio-galley
  labels:
    app: {{ template "galley.name" . }}
    chart: {{ template "galley.chart" . }}
    heritage: {{ .Release.Service }}
    release: {{ .Release.Name }}
    istio: galley
webhooks:
{{- if .Values.global.configValidation }}
  - name: pilot.validation.istio.io
    clientConfig:
      service:
        name: istio-galley
        namespace: {{ .Release.Namespace }}
        path: "/admitpilot"
      caBundle: ""
    rules:
      - operations:
        - CREATE
        - UPDATE
        apiGroups:
        - config.istio.io
        apiVersions:
        - v1alpha2
        resources:
        - httpapispecs
        - httpapispecbindings
        - quotaspecs
        - quotaspecbindings
      - operations:
        - CREATE
        - UPDATE
        apiGroups:
        - rbac.istio.io
        apiVersions:
        - "*"
        resources:
        - "*"
      - operations:
        - CREATE
        - UPDATE
        apiGroups:
        - authentication.istio.io
        apiVersions:
        - "*"
        resources:
        - "*"
      - operations:
        - CREATE
        - UPDATE
        apiGroups:
        - networking.istio.io
        apiVersions:
        - "*"
        resources:
        - destinationrules
        - envoyfilters
        - gateways
        - serviceentries
        - sidecars
        - virtualservices
    failurePolicy: Fail
    sideEffects: None
  - name: mixer.validation.istio.io
    clientConfig:
      service:
        name: istio-galley
        namespace: {{ .Release.Namespace }}
        path: "/admitmixer"
      caBundle: ""
    rules:
      - operations:
        - CREATE
        - UPDATE
        apiGroups:
        - config.istio.io
        apiVersions:
        - v1alpha2
        resources:
        - rules
        - attributemanifests
        - circonuses
        - deniers
        - fluentds
        - kubernetesenvs
        - listcheckers
        - memquotas
        - noops
        - opas
        - prometheuses
        - rbacs
        - solarwindses
        - stackdrivers
        - cloudwatches
        - dogstatsds
        - statsds
        - stdios
        - apikeys
        - authorizations
        - checknothings
        # - kuberneteses
        - listentries
        - logentries
        - metrics
        - quotas
        - reportnothings
        - tracespans
        - adapters
        - handlers
        - instances
        - templates
        - zipkins
    failurePolicy: Fail
    sideEffects: None
{{- end }}
{{- end }}

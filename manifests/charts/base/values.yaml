defaults:
  global:

    # ImagePullSecrets for control plane ServiceAccount, list of secrets in the same namespace
    # to use for pulling any images in pods that reference this ServiceAccount.
    # Must be set for any cluster configured with private docker registry.
    imagePullSecrets: []

    # Used to locate istiod.
    istioNamespace: istio-system

    istiod:
      enableAnalysis: false

    configValidation: true
    externalIstiod: false
    remotePilotAddress: ""

    # Platform where Istio is deployed. Possible values are: "openshift", "gcp".
    # An empty value means it is a vanilla Kubernetes distribution, therefore no special
    # treatment will be considered.
    platform: ""

    # Setup how istiod Service is configured. See https://kubernetes.io/docs/concepts/services-networking/dual-stack/#services
    # This is intended only for use with external istiod.
    ipFamilyPolicy: ""
    ipFamilies: []

  base:
    # Used for helm2 to add the CRDs to templates.
    enableCRDTemplates: false

    # Validation webhook configuration url
    # For example: https://$remotePilotAddress:15017/validate
    validationURL: ""

    # For istioctl usage to disable istio config crds in base
    enableIstioConfigCRDs: true

  defaultRevision: "default"

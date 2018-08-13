FROM koalaman/shellcheck:v0.5.0 AS shellcheck

FROM istio/ci:go1.10-k8s1.10.4-helm2.7.2-minikube0.25

COPY --from=shellcheck /bin/shellcheck /bin/shellcheck

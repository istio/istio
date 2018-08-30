{{ define "install-custom-resources.sh.tpl" }}
#!/bin/sh
set -e

if [ "$#" -ne "1" ]; then
    echo "first argument should be path to custom resource yaml"
    exit 1
fi

pathToResourceYAML=${1}

/kubectl get validatingwebhookconfiguration istio-galley 2>/dev/null
if [ "$?" -eq 0 ]; then
    echo "istio-galley validatingwebhookconfiguration found - waiting for istio-galley deployment to be ready"
    while true; do
        kubectl -n {{ .Release.Namespace }} get deployment istio-galley 2>/dev/null
        if [ "$?" -eq 0 ]; then
            break
        fi
        sleep 1
    done
    /kubectl -n {{ .Release.Namespace }} rollout status deployment istio-galley
    echo "istio-galley deployment ready for configuration validation"
fi
sleep 5
/kubectl apply -f ${pathToResourceYAML}
{{ end }}

# Experimental Kustomize support

Organization: each directory corresponds to a namespace ( 'environment' ).

Inside each component will have a directory, named to match the name of the directory where the helm template is defined.

A 'kustomization.yaml' file inside the directory can apply the normal kustomize rules. It should expect a 'k8s.yaml'
resource.

## Usage

"helm template" will be used with the normal values/global/user settings, and generate a k8s.yaml file under
$OUT/$NAMESPACE/$COMPONENT

If the kustomize file exists, it will be applied before running "kubectl apply --prune".

Table of Contents
=================

   * [Ansible playbook to install Istio](#ansible-playbook-to-install-istio)
      * [Prerequisites](#prerequisites)
      * [Execution](#execution)
      * [Typical use cases](#typical-use-cases)

# Ansible playbook to install Istio

The Ansible scenario defined within this project will let you to : 

- Install `Istio` Distribution and set the path of the `istioctl` go client (if you execute the command manually)
- Deploy Istio on Openshift or Kubernetes by specifying different parameters (version, enable auth, deploy bookinfo, ...)
- Specify the addons to be deployed such as `Grafana`, `Prometheus`, `Servicegraph`, `Zipkin` or `Jaeger`

## Prerequisites

- [Ansible 2.4](http://docs.ansible.com/ansible/latest/intro_installation.html)

Refer to the Ansible Installation Doc on how to install Ansible on your machine.
To use [Minishift](https://docs.openshift.org/latest/minishift/command-ref/minishift_start.html) or [Minikube](https://kubernetes.io/docs/getting-started-guides/minikube/) for local clusters, please refer to their respective documentation. 

## Execution

The role assumes that the user :
- Can access a Kubernetes or Openshift cluster via `kubectl` or `oc` respectively and is authenticated against the cluster. 
- Has the `admin` role on the OpenShift platform

Remark : Furthermore the minimum Kubernetes version that is compatible is `1.7.0` (`3.7.0` is the corresponding OpenShift version).   

**Important**: All invocations of the Ansible playbooks need to take place at the `install/ansible` path.
Failing to do so will result in unexpected errors 

The simplest execution command looks like the following:
 
```bash
ansible-playbook main.yml
```

Remarks:
- The role tries it's best to be idempotent, so running the playbook multiple times should be have the same effect as running it a single time.   
- The default parameters that apply to this role can be found in `istio/defaults/main.yml`.

The full list of configurable parameters is as follows:

| Parameter | Description | Values |
| --- | --- | --- |
| `cluster_flavour` | Defines whether the target cluster is a Kubernetes or an Openshift cluster. | Valid values are `k8s` and `ocp` (default) |
| `github_api_token` | The API token used for authentication when calling the GitHub API | Any valid GitHub API token or empty (default) |
| `cmd_path` | Can be used when the user does not have the `oc` or `kubectl` binary on the PATH | Defaults to expecting the binary is on the path | 
| `istio.release_tag_name` | Should be a valid Istio release version. If left empty, the latest Istio release will be installed | `0.2.12`, `0.3.0`, `0.4.0` (default) |
| `istio.dest` | Destination folder you want to install on your machine istio distribution | `~/.istio` (default) |
| `istio.auth` | Boolean value to install istio using MUTUAL_TLS | `true` and `false` (default) |
| `istio.namespace` | The namespace where istio will be installed | `istio-system` (default) |
| `istio.addon` | Which Istio addons should be installed as well | This field is an array field, which by default contains `grafana`, `prometheus`, `zipkin`, `jaeger` (disables Zipkin if selected) and `servicegraph` |
| `istio.delete_resources` | Boolean value to delete resources created under the istio namespace | `true` and `false` (default)|
| `istio.samples` | Array containing the names of the samples that should be installed | Valid names are: `bookinfo`, `helloworld`, `httpbin`, `sleep` 


An example of an invocation where we want to deploy Jaeger instead of Zipkin would be:
```bash
ansible-playbook main.yml -e '{"istio": {"addon": ["grafana","prometheus","jaeger","servicegraph"]}}'
```


This playbook will take care of downloading and installing Istio locally on your machine, before deploying the necessary Kubernetes / Openshift
pods, services etc. on to the cluster

### Note on istio.delete_resources

Activating the `istio.delete_resources` flag will result in any Istio related resources being deleted from the cluster before Istio is reinstalled.

In order to avoid any inconsistency issues, this flag should only be used to reinstall the same version of Istio on a cluster. If a new version
of Istio need to be reinstalled, then it is advisable to delete the `istio-system` namespace before executing the playbook (in which case the 
`istio.delete_resources` flag does not need to be activated)  

## Typical use cases

The following commands are some examples of how a user could install Istio using this Ansible role

- User executes installs Istio accepting all defaults
```bash
ansible-playbook main.yml
```

- User installs Istio on to a Kubernetes cluster 
```bash
ansible-playbook main.yml -e '{"cluster_flavour": "k8s"}' 
```

- User installs Istio on to a Kubernetes cluster and the path to `kubectl` is explicitly set (perhaps it's not on the PATH)
```bash
ansible-playbook main.yml -e '{"cluster_flavour": "k8s", "cmd_path": "~/kubectl"}' 
```

- User wants to install Istio on Openshift with settings other than the default
```bash
ansible-playbook main.yml -e '{"istio": {"release_tag_name": "0.4.0", "auth": true, "delete_resources": true}}'
```

- User wants to install Istio on Openshift but with custom add-on settings
```bash
ansible-playbook main.yml -e '{"istio": {"delete_resources": true, "addon": ["grafana", "prometheus", "jaeger"]}}'
```

- User wants to install Istio on Openshift and additionally wants to deploy some of the samples
```bash
ansible-playbook main.yml -e '{"istio": {"samples": ["helloworld", "bookinfo"]}}'
```

The list of available addons can be found at `istio/vars.main.yml` under the name `istio_all_addons`.
It should be noted that when Jaeger is enabled, Zipkin is disabled whether or not it's been selected in the addons section.

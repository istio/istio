// Copyright 2018 The Operator-SDK Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ansible

import (
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"

	"github.com/spf13/afero"
)

const K8sStatusPythonFile = "library/k8s_status.py"

// K8sStatus - the k8s status module tmpl wrapper
type K8sStatus struct {
	input.Input
}

// GetInput - gets the input
func (k *K8sStatus) GetInput() (input.Input, error) {
	if k.Path == "" {
		k.Path = K8sStatusPythonFile
	}
	return k.Input, nil
}

func (s K8sStatus) SetFS(_ afero.Fs) {}

func (k K8sStatus) CustomRender() ([]byte, error) {
	return []byte(k8sStatusTmpl), nil
}

const k8sStatusTmpl = `#!/usr/bin/python
# -*- coding: utf-8 -*-

from __future__ import absolute_import, division, print_function

import re
import copy

from ansible.module_utils.k8s.common import AUTH_ARG_SPEC, COMMON_ARG_SPEC, KubernetesAnsibleModule

try:
    from openshift.dynamic.exceptions import DynamicApiError
except ImportError as exc:
    class KubernetesException(Exception):
        pass


__metaclass__ = type

ANSIBLE_METADATA = {'metadata_version': '1.1',
                    'status': ['preview'],
                    'supported_by': 'community'}

DOCUMENTATION = '''

module: k8s_status

short_description: Update the status for a Kubernetes API resource

version_added: "2.7"

author: "Fabian von Feilitzsch (@fabianvf)"

description:
  - Sets the status field on a Kubernetes API resource. Only should be used if you are using Ansible to
    implement a controller for the resource being modified.

options:
  status:
    type: dict
    description:
    - A object containing ` + "`key: value`" + ` pairs that will be set on the status object of the specified resource.
    - One of I(status) or I(conditions) is required.
  conditions:
    type: list
    description:
    - A list of condition objects that will be set on the status.conditions field of the specified resource.
    - Unless I(force) is C(true) the specified conditions will be merged with the conditions already set on the status field of the specified resource.
    - Each element in the list will be validated according to the conventions specified in the
      [Kubernetes API conventions document](https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#spec-and-status).
    - 'The fields supported for each condition are:
      ` + "`type`" + ` (required),
      ` + "`status`" + ` (required, one of "True", "False", "Unknown"),
      ` + "`reason`" + ` (single CamelCase word),
      ` + "`message`" + `,
      ` + "`lastHeartbeatTime`" + ` (RFC3339 datetime string), and
      ` + "`lastTransitionTime`" + ` (RFC3339 datetime string).'
    - One of I(status) or I(conditions) is required.'
  api_version:
    description:
    - Use to specify the API version. Use in conjunction with I(kind), I(name), and I(namespace) to identify a
      specific object.
    required: yes
    aliases:
    - api
    - version
  kind:
    description:
    - Use to specify an object model. Use in conjunction with I(api_version), I(name), and I(namespace) to identify a
      specific object.
    required: yes
  name:
    description:
    - Use to specify an object name. Use in conjunction with I(api_version), I(kind) and I(namespace) to identify a
      specific object.
    required: yes
  namespace:
    description:
    - Use to specify an object namespace. Use in conjunction with I(api_version), I(kind), and I(name)
      to identify a specific object.
  force:
    description:
    - If set to C(True), the status will be set using ` + "`PUT`" + ` rather than ` + "`PATCH`" + `, replacing the full status object.
    default: false
    type: bool
  host:
    description:
    - Provide a URL for accessing the API. Can also be specified via K8S_AUTH_HOST environment variable.
  api_key:
    description:
    - Token used to authenticate with the API. Can also be specified via K8S_AUTH_API_KEY environment variable.
  kubeconfig:
    description:
    - Path to an instance Kubernetes config file. If not provided, and no other connection
      options are provided, the openshift client will attempt to load the default
      configuration file from I(~/.kube/config.json). Can also be specified via K8S_AUTH_KUBECONFIG environment
      variable.
  context:
    description:
    - The name of a context found in the config file. Can also be specified via K8S_AUTH_CONTEXT environment variable.
  username:
    description:
    - Provide a username for authenticating with the API. Can also be specified via K8S_AUTH_USERNAME environment
      variable.
  password:
    description:
    - Provide a password for authenticating with the API. Can also be specified via K8S_AUTH_PASSWORD environment
      variable.
  cert_file:
    description:
    - Path to a certificate used to authenticate with the API. Can also be specified via K8S_AUTH_CERT_FILE environment
      variable.
  key_file:
    description:
    - Path to a key file used to authenticate with the API. Can also be specified via K8S_AUTH_KEY_FILE environment
      variable.
  ssl_ca_cert:
    description:
    - Path to a CA certificate used to authenticate with the API. Can also be specified via K8S_AUTH_SSL_CA_CERT
      environment variable.
  verify_ssl:
    description:
    - "Whether or not to verify the API server's SSL certificates. Can also be specified via K8S_AUTH_VERIFY_SSL
      environment variable."
    type: bool

requirements:
    - "python >= 2.7"
    - "openshift >= 0.8.1"
    - "PyYAML >= 3.11"
'''

EXAMPLES = '''
- name: Set custom status fields on TestCR
  k8s_status:
    api_version: apps.example.com/v1alpha1
    kind: TestCR
    name: my-test
    namespace: testing
    status:
        hello: world
        custom: entries

- name: Update the standard condition of an Ansible Operator
  k8s_status:
    api_version: apps.example.com/v1alpha1
    kind: TestCR
    name: my-test
    namespace: testing
    conditions:
    - type: Running
      status: "True"
      reason: MigrationStarted
      message: "Migration from v2 to v3 has begun"
      lastTransitionTime: "{{ ansible_date_time.iso8601 }}"

- name: |
    Create custom conditions. WARNING: The default Ansible Operator status management
    will never overwrite custom conditions, so they will persist indefinitely. If you
    want the values to change or be removed, you will need to clean them up manually.
  k8s_status:
    conditions:
    - type: Available
      status: "False"
      reason: PingFailed
      message: "The service did not respond to a ping"

'''

RETURN = '''
result:
  description:
  - If a change was made, will return the patched object, otherwise returns the instance object.
  returned: success
  type: complex
  contains:
     api_version:
       description: The versioned schema of this representation of an object.
       returned: success
       type: str
     kind:
       description: Represents the REST resource this object represents.
       returned: success
       type: str
     metadata:
       description: Standard object metadata. Includes name, namespace, annotations, labels, etc.
       returned: success
       type: complex
     spec:
       description: Specific attributes of the object. Will vary based on the I(api_version) and I(kind).
       returned: success
       type: complex
     status:
       description: Current status details for the object.
       returned: success
       type: complex
'''


def condition_array(conditions):

    VALID_KEYS = ['type', 'status', 'reason', 'message', 'lastHeartbeatTime', 'lastTransitionTime']
    REQUIRED = ['type', 'status']
    CAMEL_CASE = re.compile(r'^(?:[A-Z]*[a-z]*)+$')
    RFC3339_datetime = re.compile(r'^\d{4}-\d\d-\d\dT\d\d:\d\d(:\d\d)?(\.\d+)?(([+-]\d\d:\d\d)|Z)$')

    def validate_condition(condition):
        if not isinstance(condition, dict):
            raise ValueError('` + "`conditions`" + ` must be a list of objects')
        if isinstance(condition.get('status'), bool):
            condition['status'] = 'True' if condition['status'] else 'False'

        for key in condition.keys():
            if key not in VALID_KEYS:
                raise ValueError('{} is not a valid field for a condition, accepted fields are {}'.format(key, VALID_KEYS))
        for key in REQUIRED:
            if not condition.get(key):
                raise ValueError('Condition ` + "`{}`" + ` must be set'.format(key))

        if condition['status'] not in ['True', 'False', 'Unknown']:
            raise ValueError('Condition ` + "`status`" + ` must be one of ["True", "False", "Unknown"], not {}'.format(condition['status']))

        if condition.get('reason') and not re.match(CAMEL_CASE, condition['reason']):
            raise ValueError('Condition ` + "`reason`" + ` must be a single, CamelCase word')

        for key in ['lastHeartBeatTime', 'lastTransitionTime']:
            if condition.get(key) and not re.match(RFC3339_datetime, condition[key]):
                raise ValueError('` + "`{}`" + ` must be a RFC3339 compliant datetime string'.format(key))

        return condition

    return [validate_condition(c) for c in conditions]


STATUS_ARG_SPEC = {
    'status': {
        'type': 'dict',
        'required': False
    },
    'conditions': {
        'type': condition_array,
        'required': False
    }
}


def main():
    KubernetesAnsibleStatusModule().execute_module()


class KubernetesAnsibleStatusModule(KubernetesAnsibleModule):

    def __init__(self, *args, **kwargs):
        KubernetesAnsibleModule.__init__(
            self, *args,
            supports_check_mode=True,
            **kwargs
        )
        self.kind = self.params.get('kind')
        self.api_version = self.params.get('api_version')
        self.name = self.params.get('name')
        self.namespace = self.params.get('namespace')
        self.force = self.params.get('force')

        self.status = self.params.get('status') or {}
        self.conditions = self.params.get('conditions') or []

        if self.conditions and self.status and self.status.get('conditions'):
            raise ValueError("You cannot specify conditions in both the ` + "`status`" + ` and ` + "`conditions`" + ` parameters")

        if self.conditions:
            self.status['conditions'] = self.conditions

    def execute_module(self):
        self.client = self.get_api_client()

        resource = self.find_resource(self.kind, self.api_version, fail=True)
        if 'status' not in resource.subresources:
            self.fail_json(msg='Resource {}.{} does not support the status subresource'.format(resource.api_version, resource.kind))

        try:
            instance = resource.get(name=self.name, namespace=self.namespace).to_dict()
        except DynamicApiError as exc:
            self.fail_json(msg='Failed to retrieve requested object: {0}'.format(exc),
                           error=exc.summary())
        # Make sure status is at least initialized to an empty dict
        instance['status'] = instance.get('status', {})

        if self.force:
            self.exit_json(**self.replace(resource, instance))
        else:
            self.exit_json(**self.patch(resource, instance))

    def replace(self, resource, instance):
        if self.status == instance['status']:
            return {'result': instance, 'changed': False}
        instance['status'] = self.status
        try:
            result = resource.status.replace(body=instance).to_dict(),
        except DynamicApiError as exc:
            self.fail_json(msg='Failed to replace status: {}'.format(exc), error=exc.summary())

        return {
            'result': result,
            'changed': True
        }

    def patch(self, resource, instance):
        if self.object_contains(instance['status'], self.status):
            return {'result': instance, 'changed': False}
        instance['status'] = self.merge_status(instance['status'], self.status)
        try:
            result = resource.status.patch(body=instance, content_type='application/merge-patch+json').to_dict()
        except DynamicApiError as exc:
            self.fail_json(msg='Failed to replace status: {}'.format(exc), error=exc.summary())

        return {
            'result': result,
            'changed': True
        }

    def merge_status(self, old, new):
        old_conditions = old.get('conditions', [])
        new_conditions = new.get('conditions', [])
        if not (old_conditions and new_conditions):
            return new

        merged = copy.deepcopy(old_conditions)

        for condition in new_conditions:
            idx = self.get_condition_idx(merged, condition['type'])
            if idx:
                merged[idx] = condition
            else:
                merged.append(condition)
        new['conditions'] = merged
        return new

    def get_condition_idx(self, conditions, name):
        for i, condition in enumerate(conditions):
            if condition.get('type') == name:
                return i

    def object_contains(self, obj, subset):
        def dict_is_subset(obj, subset):
            return all([mapping.get(type(obj.get(k)), mapping['default'])(obj.get(k), v) for (k, v) in subset.items()])

        def list_is_subset(obj, subset):
            return all(item in obj for item in subset)

        def values_match(obj, subset):
            return obj == subset

        mapping = {
            dict: dict_is_subset,
            list: list_is_subset,
            tuple: list_is_subset,
            'default': values_match
        }

        return dict_is_subset(obj, subset)

    @property
    def argspec(self):
        args = copy.deepcopy(COMMON_ARG_SPEC)
        args.pop('state')
        args.pop('resource_definition')
        args.pop('src')
        args.update(AUTH_ARG_SPEC)
        args.update(STATUS_ARG_SPEC)
        return args


if __name__ == '__main__':
    main()
`

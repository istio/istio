# Copyright Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

.PHONY: operator-proto
operator-proto:
	(cd tools/proto; buf generate --path operator)
	sed -i 's/json\:"istio_egressgateway/json\:"istio-egressgateway/g' operator/pkg/apis/istio/v1alpha1/values_types.pb.go
	sed -i 's/json\:"istio_ingressgateway/json\:"istio-ingressgateway/g' operator/pkg/apis/istio/v1alpha1/values_types.pb.go
	sed -i 's/json\:"proxyInit/json\:"proxy_init/g' operator/pkg/apis/istio/v1alpha1/values_types.pb.go

client-go-gen:
	# generate kube api type wrappers for istio types
	kubetype-gen --input-dirs istio.io/api/operator/v1alpha1 --output-base operator/pkg/apis -h /dev/null
	sed -i 's/metav1alpha1.IstioStatus/*operatorv1alpha1.InstallStatus/g' operator/pkg/apis/install/v1alpha1/types.go
	deepcopy-gen --input-dirs istio.io/istio/operator/pkg/apis/install/v1alpha1 -O zz_generated.deepcopy -h /dev/null
#	@$(move_generated)
#	# generate deepcopy for kube api types
#	@$(deepcopy_gen) --input-dirs $(kube_api_packages) -O zz_generated.deepcopy  -h $(kube_go_header_text)
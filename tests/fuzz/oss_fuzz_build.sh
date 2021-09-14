#!/bin/bash

# Copyright 2021 Istio Authors
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

sed -i 's/out.initJwksResolver()/\/\/out.initJwksResolver()/g' "${SRC}"/istio/pilot/pkg/xds/discovery.go

compile_go_fuzzer istio.io/istio/tests/fuzz FuzzParseInputs fuzz_parse_inputs
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzParseAndBuildSchema fuzz_parse_and_build_schema
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzConfigValidation fuzz_config_validation
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzConfigValidation2 fuzz_config_validation2
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzParseMeshNetworks fuzz_parse_mesh_networks
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzValidateMeshConfig fuzz_validate_mesh_config
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzInitContext fuzz_init_context
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzAnalyzer fuzz_analyzer
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzXds fuzz_xds
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzCompareDiff fuzz_compare_diff
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzHelmReconciler fuzz_helm_reconciler
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzIntoResourceFile fuzz_into_resource_file
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzTranslateFromValueToSpec fuzz_translate_from_value_to_spec
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzBNMUnmarshalJSON fuzz_bnm_unmarshal_json
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzValidateClusters fuzz_validate_clusters
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzCheckIstioOperatorSpec fuzz_check_istio_operator_spec
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzV1Alpha1ValidateConfig fuzz_v1alpha1_validate_config
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzGetEnabledComponents fuzz_get_enabled_components
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzUnmarshalAndValidateIOPS fuzz_unmarshal_and_validate_iops
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzVerify fuzz_verify
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzRenderManifests fuzz_render_manifests
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzOverlayIOP fuzz_overlay_iop
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzNewControlplane fuzz_new_control_plane
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzResolveK8sConflict fuzz_resolve_k8s_conflict
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzYAMLManifestPatch fuzz_yaml_manifest_patch
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzGalleyMeshFs fuzz_galley_mesh_fs
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzGalleyDiag fuzz_galley_diag
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzNewBootstrapServer fuzz_new_bootstrap_server
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzInmemoryKube fuzz_inmemory_kube
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzGenCSR fuzz_gen_csr
compile_go_fuzzer istio.io/istio/tests/fuzz FuzzCreateCertE2EUsingClientCertAuthenticator fuzz_create_cert_e2e_using_client_cert_authenticator

# Create seed corpora:
zip "${OUT}"/fuzz_analyzer_seed_corpus.zip "${SRC}"/istio/galley/pkg/config/analysis/analyzers/testdata/*.yaml
zip "${OUT}"/fuzz_config_validation2_seed_corpus.zip "${SRC}"/istio/tests/fuzz/testdata/FuzzConfigValidation2/seed1
zip "${OUT}"/fuzz_helm_reconciler_seed_corpus.zip "${SRC}"/istio/operator/pkg/helmreconciler/testdata/*
zip "${OUT}"/fuzz_into_resource_file_seed_corpus.zip ./pkg/kube/inject/testdata/inject/*.yaml

# Add dictionaries
cp "${SRC}"/istio/tests/fuzz/testdata/FuzzConfigValidation2/fuzz_config_validation2.dict "${OUT}"/
mv "${SRC}"/istio/tests/fuzz/testdata/inject/fuzz_into_resource_file.dict "${OUT}"/

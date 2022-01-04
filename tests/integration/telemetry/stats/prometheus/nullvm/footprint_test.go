//go:build integ
// +build integ

// Copyright Istio Authors. All Rights Reserved.
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

package nullvm

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/sync/errgroup"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/util/retry"
	common "istio.io/istio/tests/integration/telemetry/stats/prometheus"
)

var canonicalMetrics = map[string]bool{
	"envoy_cluster_assignment_stale":                                               true,
	"envoy_cluster_assignment_timeout_received":                                    true,
	"envoy_cluster_bind_errors":                                                    true,
	"envoy_cluster_circuit_breakers_default_cx_open":                               true,
	"envoy_cluster_circuit_breakers_default_cx_pool_open":                          true,
	"envoy_cluster_circuit_breakers_default_remaining_cx":                          true,
	"envoy_cluster_circuit_breakers_default_remaining_cx_pools":                    true,
	"envoy_cluster_circuit_breakers_default_remaining_pending":                     true,
	"envoy_cluster_circuit_breakers_default_remaining_retries":                     true,
	"envoy_cluster_circuit_breakers_default_remaining_rq":                          true,
	"envoy_cluster_circuit_breakers_default_rq_open":                               true,
	"envoy_cluster_circuit_breakers_default_rq_pending_open":                       true,
	"envoy_cluster_circuit_breakers_default_rq_retry_open":                         true,
	"envoy_cluster_circuit_breakers_high_cx_open":                                  true,
	"envoy_cluster_circuit_breakers_high_cx_pool_open":                             true,
	"envoy_cluster_circuit_breakers_high_rq_open":                                  true,
	"envoy_cluster_circuit_breakers_high_rq_pending_open":                          true,
	"envoy_cluster_circuit_breakers_high_rq_retry_open":                            true,
	"envoy_cluster_client_ssl_socket_factory_downstream_context_secrets_not_ready": true,
	"envoy_cluster_client_ssl_socket_factory_ssl_context_update_by_sds":            true,
	"envoy_cluster_client_ssl_socket_factory_upstream_context_secrets_not_ready":   true,
	"envoy_cluster_default_total_match_count":                                      true,
	"envoy_cluster_external_upstream_rq":                                           true,
	"envoy_cluster_external_upstream_rq_200":                                       true,
	"envoy_cluster_external_upstream_rq_completed":                                 true,
	"envoy_cluster_external_upstream_rq_time_bucket":                               true,
	"envoy_cluster_external_upstream_rq_time_count":                                true,
	"envoy_cluster_external_upstream_rq_time_sum":                                  true,
	"envoy_cluster_http1_dropped_headers_with_underscores":                         true,
	"envoy_cluster_http1_metadata_not_supported_error":                             true,
	"envoy_cluster_http1_requests_rejected_with_underscores_in_headers":            true,
	"envoy_cluster_http1_response_flood":                                           true,
	"envoy_cluster_http2_dropped_headers_with_underscores":                         true,
	"envoy_cluster_http2_header_overflow":                                          true,
	"envoy_cluster_http2_headers_cb_no_stream":                                     true,
	"envoy_cluster_http2_inbound_empty_frames_flood":                               true,
	"envoy_cluster_http2_inbound_priority_frames_flood":                            true,
	"envoy_cluster_http2_inbound_window_update_frames_flood":                       true,
	"envoy_cluster_http2_keepalive_timeout":                                        true,
	"envoy_cluster_http2_metadata_empty_frames":                                    true,
	"envoy_cluster_http2_outbound_control_flood":                                   true,
	"envoy_cluster_http2_outbound_flood":                                           true,
	"envoy_cluster_http2_pending_send_bytes":                                       true,
	"envoy_cluster_http2_requests_rejected_with_underscores_in_headers":            true,
	"envoy_cluster_http2_rx_messaging_error":                                       true,
	"envoy_cluster_http2_rx_reset":                                                 true,
	"envoy_cluster_http2_stream_refused_errors":                                    true,
	"envoy_cluster_http2_streams_active":                                           true,
	"envoy_cluster_http2_trailers":                                                 true,
	"envoy_cluster_http2_tx_flush_timeout":                                         true,
	"envoy_cluster_http2_tx_reset":                                                 true,
	"envoy_cluster_init_fetch_timeout":                                             true,
	"envoy_cluster_internal_upstream_rq":                                           true,
	"envoy_cluster_internal_upstream_rq_200":                                       true,
	"envoy_cluster_internal_upstream_rq_completed":                                 true,
	"envoy_cluster_internal_upstream_rq_time_bucket":                               true,
	"envoy_cluster_internal_upstream_rq_time_count":                                true,
	"envoy_cluster_internal_upstream_rq_time_sum":                                  true,
	"envoy_cluster_lb_healthy_panic":                                               true,
	"envoy_cluster_lb_local_cluster_not_ok":                                        true,
	"envoy_cluster_lb_recalculate_zone_structures":                                 true,
	"envoy_cluster_lb_subsets_active":                                              true,
	"envoy_cluster_lb_subsets_created":                                             true,
	"envoy_cluster_lb_subsets_fallback":                                            true,
	"envoy_cluster_lb_subsets_fallback_panic":                                      true,
	"envoy_cluster_lb_subsets_removed":                                             true,
	"envoy_cluster_lb_subsets_selected":                                            true,
	"envoy_cluster_lb_zone_cluster_too_small":                                      true,
	"envoy_cluster_lb_zone_no_capacity_left":                                       true,
	"envoy_cluster_lb_zone_number_differs":                                         true,
	"envoy_cluster_lb_zone_routing_all_directly":                                   true,
	"envoy_cluster_lb_zone_routing_cross_zone":                                     true,
	"envoy_cluster_lb_zone_routing_sampled":                                        true,
	"envoy_cluster_manager_active_clusters":                                        true,
	"envoy_cluster_manager_cds_init_fetch_timeout":                                 true,
	"envoy_cluster_manager_cds_update_attempt":                                     true,
	"envoy_cluster_manager_cds_update_duration_bucket":                             true,
	"envoy_cluster_manager_cds_update_duration_count":                              true,
	"envoy_cluster_manager_cds_update_duration_sum":                                true,
	"envoy_cluster_manager_cds_update_failure":                                     true,
	"envoy_cluster_manager_cds_update_rejected":                                    true,
	"envoy_cluster_manager_cds_update_success":                                     true,
	"envoy_cluster_manager_cds_update_time":                                        true,
	"envoy_cluster_manager_cds_version":                                            true,
	"envoy_cluster_manager_cluster_added":                                          true,
	"envoy_cluster_manager_cluster_modified":                                       true,
	"envoy_cluster_manager_cluster_removed":                                        true,
	"envoy_cluster_manager_cluster_updated":                                        true,
	"envoy_cluster_manager_cluster_updated_via_merge":                              true,
	"envoy_cluster_manager_update_merge_cancelled":                                 true,
	"envoy_cluster_manager_update_out_of_merge_window":                             true,
	"envoy_cluster_manager_warming_clusters":                                       true,
	"envoy_cluster_max_host_weight":                                                true,
	"envoy_cluster_membership_change":                                              true,
	"envoy_cluster_membership_degraded":                                            true,
	"envoy_cluster_membership_excluded":                                            true,
	"envoy_cluster_membership_healthy":                                             true,
	"envoy_cluster_membership_total":                                               true,
	"envoy_cluster_metadata_exchange_alpn_protocol_found":                          true,
	"envoy_cluster_metadata_exchange_alpn_protocol_not_found":                      true,
	"envoy_cluster_metadata_exchange_header_not_found":                             true,
	"envoy_cluster_metadata_exchange_initial_header_not_found":                     true,
	"envoy_cluster_metadata_exchange_metadata_added":                               true,
	"envoy_cluster_original_dst_host_invalid":                                      true,
	"envoy_cluster_retry_or_shadow_abandoned":                                      true,
	"envoy_cluster_ssl_ciphers":                                                    true,
	"envoy_cluster_ssl_connection_error":                                           true,
	"envoy_cluster_ssl_curves":                                                     true,
	"envoy_cluster_ssl_fail_verify_cert_hash":                                      true,
	"envoy_cluster_ssl_fail_verify_error":                                          true,
	"envoy_cluster_ssl_fail_verify_no_cert":                                        true,
	"envoy_cluster_ssl_fail_verify_san":                                            true,
	"envoy_cluster_ssl_handshake":                                                  true,
	"envoy_cluster_ssl_no_certificate":                                             true,
	"envoy_cluster_ssl_ocsp_staple_failed":                                         true,
	"envoy_cluster_ssl_ocsp_staple_omitted":                                        true,
	"envoy_cluster_ssl_ocsp_staple_requests":                                       true,
	"envoy_cluster_ssl_ocsp_staple_responses":                                      true,
	"envoy_cluster_ssl_session_reused":                                             true,
	"envoy_cluster_ssl_sigalgs_rsa_pss_rsae_sha256":                                true,
	"envoy_cluster_ssl_versions_TLSv1_2":                                           true,
	"envoy_cluster_tlsMode_disabled_total_match_count":                             true,
	"envoy_cluster_tlsMode_istio_total_match_count":                                true,
	"envoy_cluster_update_attempt":                                                 true,
	"envoy_cluster_update_duration_bucket":                                         true,
	"envoy_cluster_update_duration_count":                                          true,
	"envoy_cluster_update_duration_sum":                                            true,
	"envoy_cluster_update_empty":                                                   true,
	"envoy_cluster_update_failure":                                                 true,
	"envoy_cluster_update_no_rebuild":                                              true,
	"envoy_cluster_update_rejected":                                                true,
	"envoy_cluster_update_success":                                                 true,
	"envoy_cluster_update_time":                                                    true,
	"envoy_cluster_upstream_cx_active":                                             true,
	"envoy_cluster_upstream_cx_close_notify":                                       true,
	"envoy_cluster_upstream_cx_connect_attempts_exceeded":                          true,
	"envoy_cluster_upstream_cx_connect_fail":                                       true,
	"envoy_cluster_upstream_cx_connect_ms_bucket":                                  true,
	"envoy_cluster_upstream_cx_connect_ms_count":                                   true,
	"envoy_cluster_upstream_cx_connect_ms_sum":                                     true,
	"envoy_cluster_upstream_cx_connect_timeout":                                    true,
	"envoy_cluster_upstream_cx_destroy":                                            true,
	"envoy_cluster_upstream_cx_destroy_local":                                      true,
	"envoy_cluster_upstream_cx_destroy_local_with_active_rq":                       true,
	"envoy_cluster_upstream_cx_destroy_remote":                                     true,
	"envoy_cluster_upstream_cx_destroy_remote_with_active_rq":                      true,
	"envoy_cluster_upstream_cx_destroy_with_active_rq":                             true,
	"envoy_cluster_upstream_cx_http1_total":                                        true,
	"envoy_cluster_upstream_cx_http2_total":                                        true,
	"envoy_cluster_upstream_cx_http3_total":                                        true,
	"envoy_cluster_upstream_cx_idle_timeout":                                       true,
	"envoy_cluster_upstream_cx_length_ms_bucket":                                   true,
	"envoy_cluster_upstream_cx_length_ms_count":                                    true,
	"envoy_cluster_upstream_cx_length_ms_sum":                                      true,
	"envoy_cluster_upstream_cx_max_duration_reached":                               true,
	"envoy_cluster_upstream_cx_max_requests":                                       true,
	"envoy_cluster_upstream_cx_none_healthy":                                       true,
	"envoy_cluster_upstream_cx_overflow":                                           true,
	"envoy_cluster_upstream_cx_pool_overflow":                                      true,
	"envoy_cluster_upstream_cx_protocol_error":                                     true,
	"envoy_cluster_upstream_cx_rx_bytes_buffered":                                  true,
	"envoy_cluster_upstream_cx_rx_bytes_total":                                     true,
	"envoy_cluster_upstream_cx_total":                                              true,
	"envoy_cluster_upstream_cx_tx_bytes_buffered":                                  true,
	"envoy_cluster_upstream_cx_tx_bytes_total":                                     true,
	"envoy_cluster_upstream_flow_control_backed_up_total":                          true,
	"envoy_cluster_upstream_flow_control_drained_total":                            true,
	"envoy_cluster_upstream_flow_control_paused_reading_total":                     true,
	"envoy_cluster_upstream_flow_control_resumed_reading_total":                    true,
	"envoy_cluster_upstream_internal_redirect_failed_total":                        true,
	"envoy_cluster_upstream_internal_redirect_succeeded_total":                     true,
	"envoy_cluster_upstream_rq":                                                    true,
	"envoy_cluster_upstream_rq_200":                                                true,
	"envoy_cluster_upstream_rq_active":                                             true,
	"envoy_cluster_upstream_rq_cancelled":                                          true,
	"envoy_cluster_upstream_rq_completed":                                          true,
	"envoy_cluster_upstream_rq_maintenance_mode":                                   true,
	"envoy_cluster_upstream_rq_max_duration_reached":                               true,
	"envoy_cluster_upstream_rq_pending_active":                                     true,
	"envoy_cluster_upstream_rq_pending_failure_eject":                              true,
	"envoy_cluster_upstream_rq_pending_overflow":                                   true,
	"envoy_cluster_upstream_rq_pending_total":                                      true,
	"envoy_cluster_upstream_rq_per_try_idle_timeout":                               true,
	"envoy_cluster_upstream_rq_per_try_timeout":                                    true,
	"envoy_cluster_upstream_rq_retry":                                              true,
	"envoy_cluster_upstream_rq_retry_backoff_exponential":                          true,
	"envoy_cluster_upstream_rq_retry_backoff_ratelimited":                          true,
	"envoy_cluster_upstream_rq_retry_limit_exceeded":                               true,
	"envoy_cluster_upstream_rq_retry_overflow":                                     true,
	"envoy_cluster_upstream_rq_retry_success":                                      true,
	"envoy_cluster_upstream_rq_rx_reset":                                           true,
	"envoy_cluster_upstream_rq_time_bucket":                                        true,
	"envoy_cluster_upstream_rq_time_count":                                         true,
	"envoy_cluster_upstream_rq_time_sum":                                           true,
	"envoy_cluster_upstream_rq_timeout":                                            true,
	"envoy_cluster_upstream_rq_total":                                              true,
	"envoy_cluster_upstream_rq_tx_reset":                                           true,
	"envoy_cluster_version":                                                        true,
	"envoy_listener_manager_lds_init_fetch_timeout":                                true,
	"envoy_listener_manager_lds_update_attempt":                                    true,
	"envoy_listener_manager_lds_update_duration_bucket":                            true,
	"envoy_listener_manager_lds_update_duration_count":                             true,
	"envoy_listener_manager_lds_update_duration_sum":                               true,
	"envoy_listener_manager_lds_update_failure":                                    true,
	"envoy_listener_manager_lds_update_rejected":                                   true,
	"envoy_listener_manager_lds_update_success":                                    true,
	"envoy_listener_manager_lds_update_time":                                       true,
	"envoy_listener_manager_lds_version":                                           true,
	"envoy_listener_manager_listener_added":                                        true,
	"envoy_listener_manager_listener_create_failure":                               true,
	"envoy_listener_manager_listener_create_success":                               true,
	"envoy_listener_manager_listener_in_place_updated":                             true,
	"envoy_listener_manager_listener_modified":                                     true,
	"envoy_listener_manager_listener_removed":                                      true,
	"envoy_listener_manager_listener_stopped":                                      true,
	"envoy_listener_manager_total_filter_chains_draining":                          true,
	"envoy_listener_manager_total_listeners_active":                                true,
	"envoy_listener_manager_total_listeners_draining":                              true,
	"envoy_listener_manager_total_listeners_warming":                               true,
	"envoy_listener_manager_workers_started":                                       true,
	"envoy_metric_cache_count":                                                     true,
	"envoy_server_compilation_settings_fips_mode":                                  true,
	"envoy_server_concurrency":                                                     true,
	"envoy_server_days_until_first_cert_expiring":                                  true,
	"envoy_server_debug_assertion_failures":                                        true,
	"envoy_server_dropped_stat_flushes":                                            true,
	"envoy_server_dynamic_unknown_fields":                                          true,
	"envoy_server_envoy_bug_failures":                                              true,
	"envoy_server_hot_restart_epoch":                                               true,
	"envoy_server_initialization_time_ms_bucket":                                   true,
	"envoy_server_initialization_time_ms_count":                                    true,
	"envoy_server_initialization_time_ms_sum":                                      true,
	"envoy_server_live":                                                            true,
	"envoy_server_main_thread_watchdog_mega_miss":                                  true,
	"envoy_server_main_thread_watchdog_miss":                                       true,
	"envoy_server_memory_allocated":                                                true,
	"envoy_server_memory_heap_size":                                                true,
	"envoy_server_memory_physical_size":                                            true,
	"envoy_server_parent_connections":                                              true,
	"envoy_server_seconds_until_first_ocsp_response_expiring":                      true,
	"envoy_server_state":                                                           true,
	"envoy_server_static_unknown_fields":                                           true,
	"envoy_server_stats_recent_lookups":                                            true,
	"envoy_server_total_connections":                                               true,
	"envoy_server_uptime":                                                          true,
	"envoy_server_version":                                                         true,
	"envoy_server_wip_protos":                                                      true,
	"envoy_type_logging_success_false_export_call":                                 true,
	"envoy_type_logging_success_true_export_call":                                  true,
	"envoy_wasm_envoy_wasm_runtime_null_active":                                    true,
	"envoy_wasm_envoy_wasm_runtime_null_created":                                   true,
	"envoy_wasm_remote_load_cache_entries":                                         true,
	"envoy_wasm_remote_load_cache_hits":                                            true,
	"envoy_wasm_remote_load_cache_misses":                                          true,
	"envoy_wasm_remote_load_cache_negative_hits":                                   true,
	"envoy_wasm_remote_load_fetch_failures":                                        true,
	"envoy_wasm_remote_load_fetch_successes":                                       true,
	"istio_agent_dns_requests_total":                                               true,
	"istio_agent_endpoint_no_pod":                                                  true,
	"istio_agent_go_gc_duration_seconds":                                           true,
	"istio_agent_go_gc_duration_seconds_count":                                     true,
	"istio_agent_go_gc_duration_seconds_sum":                                       true,
	"istio_agent_go_goroutines":                                                    true,
	"istio_agent_go_info":                                                          true,
	"istio_agent_go_memstats_alloc_bytes":                                          true,
	"istio_agent_go_memstats_alloc_bytes_total":                                    true,
	"istio_agent_go_memstats_buck_hash_sys_bytes":                                  true,
	"istio_agent_go_memstats_frees_total":                                          true,
	"istio_agent_go_memstats_gc_cpu_fraction":                                      true,
	"istio_agent_go_memstats_gc_sys_bytes":                                         true,
	"istio_agent_go_memstats_heap_alloc_bytes":                                     true,
	"istio_agent_go_memstats_heap_idle_bytes":                                      true,
	"istio_agent_go_memstats_heap_inuse_bytes":                                     true,
	"istio_agent_go_memstats_heap_objects":                                         true,
	"istio_agent_go_memstats_heap_released_bytes":                                  true,
	"istio_agent_go_memstats_heap_sys_bytes":                                       true,
	"istio_agent_go_memstats_last_gc_time_seconds":                                 true,
	"istio_agent_go_memstats_lookups_total":                                        true,
	"istio_agent_go_memstats_mallocs_total":                                        true,
	"istio_agent_go_memstats_mcache_inuse_bytes":                                   true,
	"istio_agent_go_memstats_mcache_sys_bytes":                                     true,
	"istio_agent_go_memstats_mspan_inuse_bytes":                                    true,
	"istio_agent_go_memstats_mspan_sys_bytes":                                      true,
	"istio_agent_go_memstats_next_gc_bytes":                                        true,
	"istio_agent_go_memstats_other_sys_bytes":                                      true,
	"istio_agent_go_memstats_stack_inuse_bytes":                                    true,
	"istio_agent_go_memstats_stack_sys_bytes":                                      true,
	"istio_agent_go_memstats_sys_bytes":                                            true,
	"istio_agent_go_threads":                                                       true,
	"istio_agent_istiod_connection_terminations":                                   true,
	"istio_agent_num_outgoing_requests":                                            true,
	"istio_agent_outgoing_latency":                                                 true,
	"istio_agent_pilot_conflict_inbound_listener":                                  true,
	"istio_agent_pilot_conflict_outbound_listener_http_over_current_tcp":           true,
	"istio_agent_pilot_conflict_outbound_listener_tcp_over_current_http":           true,
	"istio_agent_pilot_conflict_outbound_listener_tcp_over_current_tcp":            true,
	"istio_agent_pilot_destrule_subsets":                                           true,
	"istio_agent_pilot_duplicate_envoy_clusters":                                   true,
	"istio_agent_pilot_eds_no_instances":                                           true,
	"istio_agent_pilot_endpoint_not_ready":                                         true,
	"istio_agent_pilot_no_ip":                                                      true,
	"istio_agent_pilot_proxy_convergence_time_bucket":                              true,
	"istio_agent_pilot_proxy_convergence_time_count":                               true,
	"istio_agent_pilot_proxy_convergence_time_sum":                                 true,
	"istio_agent_pilot_proxy_queue_time_bucket":                                    true,
	"istio_agent_pilot_proxy_queue_time_count":                                     true,
	"istio_agent_pilot_proxy_queue_time_sum":                                       true,
	"istio_agent_pilot_push_triggers":                                              true,
	"istio_agent_pilot_virt_services":                                              true,
	"istio_agent_pilot_vservice_dup_domain":                                        true,
	"istio_agent_pilot_xds":                                                        true,
	"istio_agent_pilot_xds_config_size_bytes_bucket":                               true,
	"istio_agent_pilot_xds_config_size_bytes_count":                                true,
	"istio_agent_pilot_xds_config_size_bytes_sum":                                  true,
	"istio_agent_pilot_xds_expired_nonce":                                          true,
	"istio_agent_pilot_xds_push_time_bucket":                                       true,
	"istio_agent_pilot_xds_push_time_count":                                        true,
	"istio_agent_pilot_xds_push_time_sum":                                          true,
	"istio_agent_pilot_xds_pushes":                                                 true,
	"istio_agent_pilot_xds_send_time_bucket":                                       true,
	"istio_agent_pilot_xds_send_time_count":                                        true,
	"istio_agent_pilot_xds_send_time_sum":                                          true,
	"istio_agent_process_cpu_seconds_total":                                        true,
	"istio_agent_process_max_fds":                                                  true,
	"istio_agent_process_open_fds":                                                 true,
	"istio_agent_process_resident_memory_bytes":                                    true,
	"istio_agent_process_start_time_seconds":                                       true,
	"istio_agent_process_virtual_memory_bytes":                                     true,
	"istio_agent_process_virtual_memory_max_bytes":                                 true,
	"istio_agent_scrapes_total":                                                    true,
	"istio_agent_startup_duration_seconds":                                         true,
	"istio_agent_wasm_cache_entries":                                               true,
	"istio_build":                                                                  true,
	"istio_requests_total":                                                         true,
	"istio_request_bytes_bucket":                                                   true,
	"istio_request_bytes_count":                                                    true,
	"istio_request_bytes_sum":                                                      true,
	"istio_request_duration_milliseconds_bucket":                                   true,
	"istio_request_duration_milliseconds_count":                                    true,
	"istio_request_duration_milliseconds_sum":                                      true,
	"istio_response_bytes_bucket":                                                  true,
	"istio_response_bytes_count":                                                   true,
	"istio_response_bytes_sum":                                                     true,
}

// This test validates that the Prometheus metrics footprint exposed
// by istio-agent does not grow unexpectedly. Growth in the metrics footprint
// can have significant impact on large clusters, due to the increased resources
// required to process and store additional time-series.
//
// Any changes to this test *MUST* be accompanied with release and upgrade notes
// that specifically call out the altered footprint and warn users about the
// new requirements.
func TestExpectedStats(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.stats.prometheus.http.nullvm").
		Run(func(ctx framework.TestContext) {
			g, _ := errgroup.WithContext(context.Background())
			for _, cltInstance := range common.GetClientInstances() {
				cltInstance := cltInstance
				g.Go(func() error {
					err := retry.UntilSuccess(func() error {
						if err := common.SendTraffic(cltInstance); err != nil {
							return err
						}
						return nil
					}, retry.Delay(framework.TelemetryRetryDelay), retry.Timeout(framework.TelemetryRetryTimeout))
					if err != nil {
						return err
					}
					return nil
				})
			}
			if err := g.Wait(); err != nil {
				t.Fatalf("test failed: %v", err)
			}
			retry.UntilSuccessOrFail(t, func() error {
				prom := common.GetPromInstance()
				lv, err := prom.KnownMetrics()
				if err != nil {
					t.Fatalf("could not retrieve list of known metrics: %v", err)
				}
				t.Logf("known metrics retrieved: %#v", lv)
				metricsMap := map[string]bool{}
				for _, lval := range lv {
					str := string(lval)
					if strings.HasPrefix(str, "istio_echo") {
						// ignore the echo app metrics
						continue
					}
					if strings.HasPrefix(str, "envoy_cluster_zone_") {
						// ignore the zone-specific envoy metrics, as the tests could be run in any location
						// todo(douglas-reid): decide how to "fix" the broken zone labels
						continue
					}
					if strings.HasPrefix(str, "istio_") || strings.HasPrefix(str, "envoy_") {
						metricsMap[string(lval)] = true
					}
				}

				if diff := cmp.Diff(canonicalMetrics, metricsMap); diff != "" {
					return fmt.Errorf("retrieved different set of metrics: %v", diff)
				}
				return nil
			}, retry.Delay(10*time.Second), retry.Timeout(2*time.Minute))
		})
}

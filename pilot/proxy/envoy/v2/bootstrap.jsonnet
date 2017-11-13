function(id, xdsPort=15003)
    {
        local ads_cluster = "ads",
        node: {
            id: id,
            cluster: "istio-proxy",
        },
        dynamic_resources: {
            lds_config: { ads: {} },
            cds_config: { ads: {} },
            ads_config: {
                api_type: "GRPC",
                cluster_name: [ads_cluster],
            },
        },
        static_resources: {
            clusters: [{
                name: ads_cluster,
                connect_timeout: "5s",
                type: "LOGICAL_DNS",
                dns_refresh_rate: "5s",
                hosts: [{
                    socket_address: {
                        address: "127.0.0.1",  // TODO
                        port_value: xdsPort,
                    },
                }],
                lb_policy: "ROUND_ROBIN",
                http2_protocol_options: {},
            }],
        },
        admin: {
            access_log_path: "/dev/null",
            address: {
                socket_address: {
                    address: "127.0.0.1",
                    port_value: 15000,
                },
            },
        },
    }

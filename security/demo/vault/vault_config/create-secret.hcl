{
    "name": "istio-cert",
    "path": {
        "secret/data/creds": {
            "capabilities": ["create", "update", "list", "read", "delete"]
        },
        "secret/data/creds/*": {
            "capabilities": ["create", "update", "list", "read", "delete"]
        }
    }
}

#!/bin/bash


[ -f vmlinux.h ] || bpftool btf dump file /sys/kernel/btf/vmlinux format c > vmlinux.h
go run github.com/cilium/ebpf/cmd/bpf2go block_ztunnel_ports block_ztunnel_ports.bpf.c

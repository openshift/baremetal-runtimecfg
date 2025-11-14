# Agents and Components

This document describes the various agents and components in the baremetal-runtimecfg project.

## Overview

The baremetal-runtimecfg project provides utilities for managing dynamic networking configurations in OpenShift baremetal and cloud deployments. It consists of a main CLI tool and several monitoring agents that watch for configuration changes and update networking components accordingly.

## Components

### runtimecfg (Main CLI Tool)

**Location:** `cmd/runtimecfg/`

The primary command-line utility that discovers OpenShift cluster and node configuration and renders Go templates for network configuration.

**Commands:**
- `display`: Shows configuration information
- `render`: Renders Go templates with the runtime configuration
  - Takes `-o/--out-dir` parameter to specify output directory
- `help`: Help about any command

**Flags:**
- `--api-vip`: Virtual IP Address to reach the OpenShift API (deprecated, use `--api-vips`)
- `--api-vips`: Virtual IP Addresses to reach the OpenShift API
- `--dns-vip`: Virtual IP Address to reach an OpenShift node resolving DNS server
- `--ingress-vip`: Virtual IP Address to reach the OpenShift Ingress Routers (deprecated, use `--ingress-vips`)
- `--ingress-vips`: Virtual IP Addresses to reach the OpenShift Ingress Routers

**Note:** At least one VIP must be passed for the VRRP interface to be found.

---

### corednsmonitor

**Location:** `cmd/corednsmonitor/`

Monitors runtime external interface for CoreDNS Corefile changes and updates DNS configuration accordingly.

**Usage:**
```
corednsmonitor path_to_kubeconfig path_to_keepalived_cfg_template path_to_config
```

**Flags:**
- `--api-vip`: Virtual IP Address to reach the OpenShift API (deprecated, use `--api-vips`)
- `--api-vips`: Virtual IP Addresses to reach the OpenShift API
- `--ingress-vip`: Virtual IP Address to reach the OpenShift Ingress Routers (deprecated, use `--ingress-vips`)
- `--ingress-vips`: Virtual IP Addresses to reach the OpenShift Ingress Routers
- `--check-interval`: Time between CoreDNS watch checks (default: 30 seconds)
- `-c, --cluster-config`: Path to cluster-config ConfigMap to retrieve ControlPlane info
- `--cloud-ext-lb-ips`: IP Addresses of Cloud External Load Balancers for OpenShift API
- `--cloud-int-lb-ips`: IP Addresses of Cloud Internal Load Balancers for OpenShift Internal API
- `--cloud-ingress-lb-ips`: IP Addresses of Cloud Ingress Load Balancers
- `-p, --platform`: Cluster Platform

**Functionality:**
- Monitors CoreDNS configuration for changes
- Supports multiple API and Ingress VIPs
- Handles cloud platform configurations
- Automatically updates Corefile when interface changes are detected

---

### dnsmasqmonitor

**Location:** `cmd/dnsmasqmonitor/`

Monitors the dnsmasq host ConfigMap for changes and updates DNS configuration.

**Usage:**
```
dnsmasqmonitor path_to_kubeconfig path_to_host_file_cfg_template path_to_config
```

**Flags:**
- `--api-vip`: Virtual IP Address to reach the OpenShift API (deprecated, use `--api-vips`)
- `--api-vips`: Virtual IP Addresses to reach the OpenShift API
- `--check-interval`: Time between dnsmasq watch checks (default: 30 seconds)
- `-p, --platform`: Cluster Platform

**Functionality:**
- Monitors dnsmasq ConfigMap for host configuration changes
- Platform-agnostic design
- Periodic configuration validation

---

### dynkeepalived

**Location:** `cmd/dynkeepalived/`

Monitors runtime external interface for keepalived configuration and dynamically reloads keepalived when changes are detected.

**Usage:**
```
dynkeepalived path_to_kubeconfig path_to_keepalived_cfg_template path_to_config
```

**Flags:**
- `--api-vip`: Virtual IP Address to reach the OpenShift API (deprecated, use `--api-vips`)
- `--api-vips`: Virtual IP Addresses to reach the OpenShift API
- `--ingress-vip`: Virtual IP Address to reach the OpenShift Ingress Routers (deprecated, use `--ingress-vips`)
- `--ingress-vips`: Virtual IP Addresses to reach the OpenShift Ingress Routers
- `--dns-vip`: Virtual IP Address to reach an OpenShift node resolving DNS server (deprecated)
- `--api-port`: Port where the OpenShift API listens (default: 6443)
- `--lb-port`: Port where the API HAProxy LB will listen (default: 9445)
- `--check-interval`: Time between keepalived watch checks (default: 10 seconds)
- `-c, --cluster-config`: Path to cluster-config ConfigMap to retrieve ControlPlane info
- `-p, --platform`: Cluster Platform
- `--control-plane-topology`: Cluster Control Plane Topology

**Functionality:**
- Monitors network interfaces for changes
- Dynamically updates keepalived configuration
- Manages Virtual IP configurations for API and Ingress
- Supports different control plane topologies
- Handles multiple VIPs

---

### monitor (HAProxy Monitor)

**Location:** `cmd/monitor/`

Monitors master node membership and updates HAProxy configuration accordingly.

**Usage:**
```
monitor path_to_kubeconfig path_to_haproxy_cfg_template path_to_config
```

**Flags:**
- `--api-vip`: Virtual IP Address to reach the OpenShift API (deprecated, use `--api-vips`)
- `--api-vips`: Virtual IP Addresses to reach the OpenShift API
- `--api-port`: Port where the OpenShift API listens (default: 6443)
- `--lb-port`: Port where the API HAProxy LB will listen (default: 9445)
- `--stat-port`: Port where the HAProxy stats API will listen (default: 29445)
- `--check-interval`: Time between monitor checks (default: 6 seconds)

**Functionality:**
- Monitors cluster master/control plane membership
- Automatically updates HAProxy backend configuration
- Maintains load balancer configuration as nodes join/leave the cluster
- Provides statistics endpoint for monitoring

---

## Common Features

All monitoring agents share these characteristics:

- **Template-based configuration:** Use Go templates for flexible configuration rendering
- **Kubeconfig integration:** Connect to the Kubernetes API for cluster information
- **Periodic checking:** Configurable intervals for monitoring changes
- **VIP support:** Handle multiple Virtual IP addresses for high availability
- **Platform awareness:** Support for baremetal and various cloud platforms
- **Automatic updates:** Detect changes and update configurations without manual intervention

## Architecture

The monitoring agents follow a common pattern:

1. **Initialize:** Load kubeconfig and configuration templates
2. **Monitor:** Periodically check for changes in cluster state or network configuration
3. **Detect:** Compare current state with previous state
4. **Update:** Render new configuration from templates when changes are detected
5. **Reload:** Trigger service reloads (keepalived, CoreDNS, dnsmasq, HAProxy) as needed

## Platform Support

The agents support multiple platforms including:
- Baremetal deployments
- Cloud platforms (with external/internal load balancers)
- Hybrid configurations

Cloud platform support includes handling of external and internal load balancer IPs for proper DNS resolution and traffic routing.

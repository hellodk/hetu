# Network Spike Incident Investigation Report

**Incident Dates:** February 22, 2026 and February 28, 2026  
**Investigation Date:** March 1, 2026  
**Investigator:** SRE Team  
**Status:** Root Cause Identified

---

## Executive Summary

A significant network traffic spike was observed on February 22nd (1,668 GB) and February 28th (285 GB), far exceeding normal daily consumption of 10-100 GB.

**Root Cause:** A cascading failure involving:

1. **Tailscale DERP relay fallback** - Lima VM nodes failed to establish direct peer-to-peer connections, causing all inter-node traffic to route through Tailscale's DERP relay server in Bengaluru
2. **Calico VXLAN tunnel resynchronization** - The resulting latency caused node flapping, triggering repeated Calico network resyncs
3. **Bandwidth multiplication** - DERP relay routes traffic through the internet and back, effectively multiplying bandwidth consumption 4x

**Impact:** The two Lima VMs transmitted **17.6 TB** through Tailscale interfaces in 7 days, with peak rates of **140 Mbps** during incidents.

---

## Timeline of Events

### February 22, 2026 - Major Spike (1,668.55 GB)

| Time (UTC) | Event |
|------------|-------|
| ~14:00 | Node status changes detected: lima-mm0-k0s-ubuntu (22 changes), lima-mm1-k0s-ubuntu (14 changes) |
| 17:15-17:30 | Traffic baseline near 0 Gbps |
| 18:00 | Traffic spike begins: 0.253 Gbps |
| 18:45 | Peak traffic: **0.427 Gbps** |
| 19:00-19:15 | Sustained high: ~0.405 Gbps |
| 20:00 | Traffic normalizes: 0.036 Gbps |

### February 28, 2026 - Secondary Spike (284.98 GB)

| Time (UTC) | Event |
|------------|-------|
| 14:00-17:15 | Normal baseline traffic |
| 17:30 | Traffic spike begins: 0.172 Gbps |
| 17:45 | Rapid increase: 0.464 Gbps |
| 18:00-19:00 | Peak sustained: **~0.596 Gbps** |
| 20:00-20:30 | Gradual decline: ~0.465 Gbps |
| 21:00 | Traffic normalizes: 0.123 Gbps |

---

## Investigation Methodology

### 1. Data Collection

Prometheus queries used for investigation:

```promql
# Identify namespace-level traffic
sum(rate(container_network_transmit_bytes_total[1h])) by (namespace)

# Identify pod-level traffic sources
topk(10, sum(rate(container_network_transmit_bytes_total[1h])) by (pod, namespace))

# Track node Ready status changes
changes(kube_node_status_condition{condition="Ready"}[1h])

# Calculate total bandwidth consumption
sum(increase(container_network_transmit_bytes_total{namespace="kube-system"}[24h])) by (pod)
```

### 2. Cluster Topology Analysis

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Kubernetes Cluster                            │
│                         (k0s v1.34.3)                                │
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌──────────────┐  ┌────────────────────┐  ┌────────────────────┐   │
│  │    cylon     │  │ lima-mm0-k0s-ubuntu│  │ lima-mm1-k0s-ubuntu│   │
│  │ (control     │  │    (PROBLEM)       │  │    (PROBLEM)       │   │
│  │  plane)      │  │                    │  │                    │   │
│  │ 100.89.50.27 │  │   100.80.23.82     │  │  100.79.109.104    │   │
│  │   Ready      │  │      Ready         │  │    NotReady ⚠️     │   │
│  └──────────────┘  └────────────────────┘  └────────────────────┘   │
│                                                                      │
│  ┌──────────────┐  ┌──────────────┐                                 │
│  │ raspberrypi  │  │   typhoon    │                                 │
│  │ 100.75.0.32  │  │ 100.82.46.84 │                                 │
│  │    Ready     │  │    Ready     │                                 │
│  └──────────────┘  └──────────────┘                                 │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

---

## Root Cause Analysis

### Primary Cause: Tailscale DERP Relay Fallback

**Critical Finding:** All cluster nodes communicate over Tailscale VPN (100.x.x.x CGNAT addresses). When direct peer-to-peer connections fail, Tailscale falls back to DERP (Designated Encrypted Relay for Packets) servers, routing all traffic through the internet.

**Current Tailscale Status:**

```
Node                    Connection Type    Status
────────────────────────────────────────────────────────────────
cylon (100.89.50.27)    direct            Active, control-plane
lima-mm0-k0s-ubuntu     direct            Active, 192.168.1.64:44340
lima-mm1-k0s-ubuntu     relay "blr"       OFFLINE, last seen 3h ago  ⚠️
raspberrypi             direct            Active, 192.168.1.118:41641
typhoon                 direct            Active, 192.168.1.52:41641
```

**Traffic Through tailscale0 Interface (7-day totals):**

| Node | Tailscale Traffic | Connection |
|------|------------------|------------|
| lima-mm1-k0s-ubuntu | **8.65 TB** | DERP relay |
| lima-mm0-k0s-ubuntu | **8.94 TB** | Direct (now) |
| cylon | 1.48 TB | Direct |
| raspberrypi | 0.01 TB | Direct |
| typhoon | 0.01 TB | Direct |

**The two Lima VMs transmitted 17.6 TB through Tailscale in 7 days.**

### Why DERP Relay Causes Traffic Explosions

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    NORMAL PATH (Direct P2P)                             │
│  ┌────────────┐              LAN               ┌────────────┐          │
│  │  lima-mm0  │ ────────────────────────────── │  lima-mm1  │          │
│  │ 192.168.1.64                               192.168.1.x   │          │
│  └────────────┘         ~1ms latency          └────────────┘          │
│                     Traffic counted ONCE                               │
└─────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────┐
│                    DERP RELAY PATH (Fallback)                           │
│  ┌────────────┐                                ┌────────────┐          │
│  │  lima-mm0  │                                │  lima-mm1  │          │
│  └─────┬──────┘                                └──────┬─────┘          │
│        │ Upload                                       │ Upload         │
│        ▼                                              ▼                │
│  ┌───────────┐        ┌─────────────┐         ┌───────────┐           │
│  │   ISP     │───────►│ DERP "blr"  │◄────────│    ISP    │           │
│  │  Router   │        │ (Bengaluru) │         │  Router   │           │
│  └───────────┘        │   13.9ms    │         └───────────┘           │
│        ▲              └─────────────┘                ▲                │
│        │ Download                                    │ Download       │
│        └─────────────────────────────────────────────┘                │
│                                                                        │
│           Traffic counted 4x: 2x upload + 2x download                 │
│                    Latency: ~28ms minimum                              │
└─────────────────────────────────────────────────────────────────────────┘
```

### Secondary Cause: Calico VXLAN Tunnel Resynchronization

The cluster uses Calico CNI with **VXLAN Always** mode:

```yaml
apiVersion: crd.projectcalico.org/v1
kind: IPPool
spec:
  cidr: 10.244.0.0/16
  vxlanMode: Always      # <-- All pod traffic encapsulated
  ipipMode: Never
  natOutgoing: true
```

When a node transitions between Ready/NotReady states, Calico must:

1. **Tear down VXLAN tunnels** to the affected node
2. **Re-establish tunnel endpoints** when node returns
3. **Synchronize routing tables** across all nodes
4. **Re-encapsulate all pod traffic** destined for the affected node

### Traffic Attribution

**Top traffic sources (24-hour window):**

| Pod | Node | Traffic (GB) |
|-----|------|-------------|
| calico-node-4qf72 | lima-mm0-k0s-ubuntu | 7,757.15 |
| kube-proxy-4khnf | lima-mm0-k0s-ubuntu | 7,756.31 |
| calico-node-lcrw9 | lima-mm1-k0s-ubuntu | 7,635.67 |
| kube-proxy-5dc52 | lima-mm1-k0s-ubuntu | 7,635.06 |

**Key Observation:** 99%+ of traffic originated from Calico and kube-proxy pods on the two Lima VM nodes.

### Contributing Factors

1. **Node Instability (lima-mm1-k0s-ubuntu)**
   - Currently NotReady: "Kubelet stopped posting node status"
   - Has taints: `node.kubernetes.io/unreachable:NoExecute`
   - Multiple Ready status changes during incident windows

2. **High Pod Restart Rate**
   - otel-collector pods: 300+ restarts in 40 hours
   - Each restart triggers network reconfiguration

3. **VM Architecture**
   - Lima VMs running on shared infrastructure
   - Network path between VMs may traverse host networking stack

---

## Evidence Summary

### Tailscale Interface Traffic Correlation

**Feb 22 - tailscale0 interface rates:**

| Time (UTC) | lima-mm0 (Mbps) | lima-mm1 (Mbps) | cylon (Mbps) |
|------------|-----------------|-----------------|--------------|
| 17:00 | 45.5 | - | 35.58 |
| 17:30 | 97.99 | 41.47 | 41.71 |
| 18:00 | 97.82 | 93.89 | 56.11 |
| 18:30 | **106.56** | 43.00 | 60.43 |
| 19:00 | 98.66 | **97.53** | 52.15 |
| 19:30 | 94.62 | 92.92 | 48.71 |
| 20:00 | 50.82 | 37.59 | 33.76 |

**Feb 28 - tailscale0 interface rates:**

| Time (UTC) | lima-mm0 (Mbps) | lima-mm1 (Mbps) |
|------------|-----------------|-----------------|
| 17:30 | 21.62 | 19.58 |
| 18:00 | 92.22 | 90.27 |
| 18:30 | **140.83** | **140.75** |
| 19:00 | 140.53 | 141.02 |
| 19:30 | 134.40 | 134.27 |
| 20:00 | 121.47 | 120.08 |
| 20:30 | 115.40 | 112.72 |
| 21:00 | 72.77 | 71.30 |

**Key Observation:** The traffic spike is almost perfectly mirrored between the two Lima nodes, indicating bidirectional communication (likely Calico VXLAN tunnels or Kubernetes API traffic) being relayed through DERP.

### Network Traffic Graph (Feb 22)

```
Traffic Rate (Gbps)
    0.5 │                    ████
        │                   █████
    0.4 │                  ██████
        │                 ███████
    0.3 │                █████████
        │               ██████████
    0.2 │              ███████████
        │             ████████████
    0.1 │            █████████████
        │           ██████████████
    0.0 │___________████████████████___
        17:00   18:00   19:00   20:00 UTC
```

### Node Ready Status Changes

```
Feb 22, ~14:00 UTC (timestamp 1771783200):
├── lima-mm0-k0s-ubuntu: 22 status transitions
├── lima-mm1-k0s-ubuntu: 14 status transitions
└── Network spike began ~4 hours later
```

---

## Affected Systems

| Namespace | Impact |
|-----------|--------|
| kube-system | Primary source - Calico/kube-proxy traffic |
| monitoring | Secondary - Prometheus node-exporter replication |
| elk | Minor - OTel collector restarts |
| otel-demo | Minor - Workloads on affected nodes |

---

## Recommendations

### Immediate Actions

1. **Fix Tailscale P2P connectivity on Lima VMs**
   ```bash
   # On lima-mm1-k0s-ubuntu (requires SSH/console access)
   tailscale status
   tailscale ping 100.80.23.82  # Test P2P to lima-mm0
   
   # Force reconnection
   tailscale down && tailscale up
   
   # Check for NAT/firewall issues
   tailscale netcheck
   ```

2. **Verify direct connectivity is established**
   ```bash
   # Should show "direct" not "relay"
   tailscale status | grep lima
   
   # Expected output:
   # 100.80.23.82  lima-mm0-k0s-ubuntu  active; direct 192.168.1.64:44340
   # 100.79.109.104 lima-mm1-k0s-ubuntu active; direct 192.168.1.x:xxxxx
   ```

3. **Investigate lima-mm1-k0s-ubuntu node**
   ```bash
   # Check node status
   kubectl describe node lima-mm1-k0s-ubuntu
   
   # Check kubelet logs (on the node)
   journalctl -u kubelet -n 100
   
   # Check if Tailscale service is running
   systemctl status tailscaled
   ```

4. **Stabilize the node or remove from cluster**
   ```bash
   # Option A: Cordon and drain
   kubectl cordon lima-mm1-k0s-ubuntu
   kubectl drain lima-mm1-k0s-ubuntu --ignore-daemonsets --delete-emptydir-data
   
   # Option B: Remove node entirely
   kubectl delete node lima-mm1-k0s-ubuntu
   ```

### Long-term Improvements - Tailscale

1. **Fix VM NAT Layers (Root Cause)**
   
   Lima/VirtualBox VMs default to NAT networking, adding extra NAT layers that break P2P:
   ```yaml
   # Switch Lima VMs to bridged networking
   # In ~/.lima/<vm-name>/lima.yaml
   networks:
     - lima: bridged
       interface: "en0"  # Your host's network interface
   ```
   
   This gives VMs real LAN IPs, eliminating NAT layers.

2. **Enable direct LAN connections**
   
   Ensure firewall allows UDP 41641 (WireGuard) between nodes:
   ```bash
   sudo ufw allow 41641/udp
   ```

3. **Verify NAT Type on All Nodes**
   
   Run on each node to identify problematic NATs:
   ```bash
   tailscale netcheck | grep -E "UDP|Mapping"
   # "MappingVariesByDestIP: true" = symmetric NAT = P2P will fail
   ```

4. **Monitor DERP relay usage**
   ```bash
   # Add to monitoring: check for relay connections
   tailscale status --json | jq '.Peer | to_entries[] | select(.value.Relay != "") | {name: .value.HostName, relay: .value.Relay}'
   ```

5. **Consider disabling DERP entirely (for LAN-only clusters)**
   ```bash
   # WARNING: Nodes will be unreachable if P2P fails
   tailscale up --derp=false
   ```
   
   Only use if all nodes can always reach each other directly.

6. **Consider Tailscale subnet routing**
   
   Instead of using Tailscale IPs for inter-node communication, advertise pod CIDRs via subnet routing to keep Kubernetes traffic on the LAN.

### Long-term Improvements - Kubernetes

1. **Consider VXLAN mode change**
   - Switch to `vxlanMode: CrossSubnet` if nodes share a subnet
   - Reduces encapsulation overhead for same-subnet traffic

2. **Implement node health monitoring alerts**
   ```yaml
   # PrometheusRule for node flapping
   - alert: NodeFlapping
     expr: changes(kube_node_status_condition{condition="Ready",status="true"}[30m]) > 5
     for: 5m
     labels:
       severity: warning
     annotations:
       summary: "Node {{ $labels.node }} is flapping"
   ```

3. **Set resource limits on Lima VMs**
   - Ensure adequate CPU/memory to prevent kubelet timeouts
   - Configure proper liveness probe timeouts

4. **Add network traffic alerts**
   ```yaml
   - alert: HighNetworkTraffic
     expr: sum(rate(container_network_transmit_bytes_total[5m])) by (node) > 100000000
     for: 10m
     labels:
       severity: warning
   ```

---

## Prometheus Queries for Monitoring

Save these queries in Grafana for ongoing monitoring:

### Tailscale-Specific Monitoring

```promql
# Tailscale interface traffic by node (MB/s)
rate(node_network_transmit_bytes_total{device="tailscale0"}[5m]) / 1048576

# Tailscale traffic spike detection (>50 MB/s sustained)
rate(node_network_transmit_bytes_total{device="tailscale0"}[5m]) > 52428800

# Total Tailscale traffic per node (daily)
increase(node_network_transmit_bytes_total{device="tailscale0"}[24h])

# Compare Tailscale vs physical interface traffic (DERP indicator)
# High tailscale0 with low eth0 suggests DERP relay
rate(node_network_transmit_bytes_total{device="tailscale0"}[5m]) 
  / rate(node_network_transmit_bytes_total{device=~"eth0|enp.*"}[5m])
```

### Kubernetes Network Monitoring

```promql
# Network traffic by node (bytes/sec)
sum(rate(container_network_transmit_bytes_total[5m])) by (node)

# Node Ready status (1=Ready, 0=NotReady)
kube_node_status_condition{condition="Ready",status="true"}

# Calico-specific traffic
sum(rate(container_network_transmit_bytes_total{pod=~"calico-node.*"}[5m])) by (pod)

# Node status changes (flapping detection)
changes(kube_node_status_condition{condition="Ready"}[1h])
```

### Recommended Alerts

```yaml
# Alert: Node using DERP relay (requires custom exporter or script)
- alert: TailscaleHighTraffic
  expr: rate(node_network_transmit_bytes_total{device="tailscale0"}[5m]) > 52428800
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: "High Tailscale traffic on {{ $labels.instance }}"
    description: "Tailscale interface transmitting >50MB/s. Check for DERP relay fallback."

- alert: TailscaleTrafficAnomaly
  expr: |
    rate(node_network_transmit_bytes_total{device="tailscale0"}[5m]) 
    > 10 * avg_over_time(rate(node_network_transmit_bytes_total{device="tailscale0"}[5m])[1h:5m])
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: "Tailscale traffic spike on {{ $labels.instance }}"
    description: "Traffic is 10x higher than hourly average. Likely DERP relay fallback."
```

---

## Conclusion

The network spikes on February 22nd and 28th were caused by a **cascading failure involving Tailscale DERP relay fallback and Calico VXLAN tunnel resynchronization**.

### The Feedback Loop

```
┌─────────────────────────────────────────────────────────────────────┐
│                     INCIDENT FEEDBACK LOOP                          │
│                                                                     │
│   ┌──────────────┐                          ┌──────────────┐       │
│   │ Tailscale P2P│                          │    Node      │       │
│   │   Failure    │─────────────────────────►│   Flapping   │       │
│   └──────────────┘                          └──────┬───────┘       │
│          ▲                                         │               │
│          │                                         ▼               │
│   ┌──────┴───────┐                          ┌──────────────┐       │
│   │   Increased  │◄─────────────────────────│    Calico    │       │
│   │   Latency    │                          │ VXLAN Resync │       │
│   └──────────────┘                          └──────────────┘       │
│          │                                         ▲               │
│          ▼                                         │               │
│   ┌──────────────┐                          ┌──────┴───────┐       │
│   │  DERP Relay  │─────────────────────────►│   Massive    │       │
│   │   Fallback   │                          │   Traffic    │       │
│   └──────────────┘                          └──────────────┘       │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

1. **Tailscale P2P connection failed** between Lima VMs
2. Traffic fell back to **DERP relay "blr"** (Bengaluru)
3. DERP relay added **~28ms latency** (vs ~1ms LAN)
4. Increased latency caused **kubelet health check timeouts**
5. Node appeared as NotReady, triggering **Calico VXLAN resync**
6. Resync traffic (already large) was now **routed through DERP**
7. DERP relay **multiplied bandwidth consumption** (traffic goes out and back)
8. This consumed **~140 Mbps sustained** through ISP connection
9. Loop repeated as nodes continued to flap

### Key Takeaways

1. **Tailscale DERP relay is a bandwidth multiplier** - what would be local LAN traffic becomes internet traffic counted multiple times.

2. **Kubernetes + Tailscale + DERP is a dangerous combination** - Kubernetes generates significant inter-node traffic (API, CNI, etcd). When this traffic hits DERP relay, bandwidth explodes.

3. **Monitor Tailscale connection types** - A single node falling back to DERP can trigger cascading failures across the cluster.

4. **The 17.6 TB transmitted by Lima VMs in 7 days** was almost entirely due to DERP relay fallback, not actual workload traffic.

### Prevention

- Ensure all Kubernetes nodes have **direct P2P Tailscale connectivity**
- Alert on **any node using DERP relay** for more than 5 minutes
- Consider using **LAN IPs for Kubernetes node communication** with Tailscale only for external access
- Implement **Calico direct mode** (`vxlanMode: CrossSubnet`) when nodes share a network segment

---

## Appendix: Raw Data

### Feb 22 Traffic Spike (Combined Calico Nodes)

| Time (UTC) | Rate (Gbps) |
|------------|-------------|
| 17:45 | 0.004 |
| 18:00 | 0.253 |
| 18:15 | 0.165 |
| 18:30 | 0.300 |
| 18:45 | 0.427 |
| 19:00 | 0.404 |
| 19:15 | 0.406 |
| 19:30 | 0.317 |
| 19:45 | 0.105 |
| 20:00 | 0.036 |

### Feb 28 Traffic Spike (Combined Calico Nodes)

| Time (UTC) | Rate (Gbps) |
|------------|-------------|
| 17:30 | 0.172 |
| 17:45 | 0.464 |
| 18:00 | 0.596 |
| 18:15 | 0.595 |
| 18:30 | 0.594 |
| 18:45 | 0.599 |
| 19:00 | 0.598 |
| 19:15 | 0.587 |
| 19:30 | 0.531 |
| 19:45 | 0.478 |
| 20:00 | 0.468 |
| 20:30 | 0.470 |
| 21:00 | 0.123 |

---

*Report generated: March 1, 2026*

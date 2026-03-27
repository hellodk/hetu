# Pod Health Management System

## Design Document v1.0

**Author:** K8s Cluster Intelligence Team  
**Date:** February 2026  
**Status:** Proposed  

---

## Table of Contents

1. [Overview](#overview)
2. [Problem Statement](#problem-statement)
3. [Goals & Non-Goals](#goals--non-goals)
4. [Design](#design)
5. [Detection Categories](#detection-categories)
6. [Actions & Remediation](#actions--remediation)
7. [Safety Mechanisms](#safety-mechanisms)
8. [API Specification](#api-specification)
9. [UI Design](#ui-design)
10. [Configuration](#configuration)
11. [Implementation Plan](#implementation-plan)
12. [Security Considerations](#security-considerations)
13. [Testing Strategy](#testing-strategy)
14. [Rollout Plan](#rollout-plan)

---

## Overview

The Pod Health Management System extends the K8s Cluster Intelligence Engine to detect, categorize, and optionally remediate non-running pods in a Kubernetes cluster. This includes evicted pods, failed pods, stuck terminating pods, and pods in various error states.

### Key Capabilities

- **Detection**: Identify all non-running pods with root cause analysis
- **Categorization**: Group pods by issue type for efficient handling
- **Remediation**: Provide manual and automated actions to resolve issues
- **Audit Trail**: Log all actions for compliance and debugging
- **Safety**: Multiple safeguards to prevent accidental damage

---

## Problem Statement

Kubernetes clusters accumulate non-running pods over time:

| Issue | Impact |
|-------|--------|
| **Evicted pods** | Consume etcd storage, clutter monitoring |
| **Failed pods** | Indicate application issues, waste resources |
| **Stuck terminating** | Block namespace deletion, indicate problems |
| **CrashLoopBackOff** | Waste CPU cycles, generate noise |
| **Pending pods** | Resources requested but not available |

### Current State

- Manual cleanup required via `kubectl` commands
- No centralized visibility of pod health issues
- No automated remediation options
- Difficult to identify root causes

### Desired State

- Single dashboard showing all pod health issues
- One-click remediation for safe actions
- Automated cleanup of low-risk items (evicted, old completed)
- Root cause analysis with actionable recommendations

---

## Goals & Non-Goals

### Goals

1. **Visibility**: Surface all non-running pods in a single view
2. **Categorization**: Group by issue type with counts
3. **Root Cause**: Show why each pod is not running
4. **Safe Actions**: Provide remediation with safety guards
5. **Automation**: Optional auto-cleanup for safe categories
6. **Audit**: Log all actions with timestamps and results

### Non-Goals

1. **Auto-scaling**: Not automatically adding nodes for pending pods
2. **Image fixes**: Not automatically updating broken images
3. **Config fixes**: Not automatically fixing ConfigMaps/Secrets
4. **Cost optimization**: Handled by separate cost module
5. **Restart policies**: Not modifying pod restart policies

---

## Design

### Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                     POD HEALTH MANAGEMENT SYSTEM                     │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌────────────────┐                                                  │
│  │   K8s API      │                                                  │
│  │   (Informer)   │                                                  │
│  └───────┬────────┘                                                  │
│          │                                                           │
│          ▼                                                           │
│  ┌────────────────┐    ┌────────────────┐    ┌────────────────┐     │
│  │                │    │                │    │                │     │
│  │   DETECTOR     │───▶│   ANALYZER     │───▶│   ACTIONER     │     │
│  │                │    │                │    │                │     │
│  │ • List pods    │    │ • Categorize   │    │ • Delete       │     │
│  │ • Filter non-  │    │ • Root cause   │    │ • Force delete │     │
│  │   running      │    │ • Recommend    │    │ • Restart      │     │
│  │ • Get events   │    │ • Risk assess  │    │ • Notify       │     │
│  │                │    │                │    │                │     │
│  └────────────────┘    └────────────────┘    └───────┬────────┘     │
│                                                       │              │
│                                                       ▼              │
│                                              ┌────────────────┐      │
│                                              │  ACTION LOG    │      │
│                                              │  (SQLite)      │      │
│                                              └────────────────┘      │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │                         REST API                              │   │
│  │  GET /pods/unhealthy  POST /pods/delete  POST /deploy/restart│   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │                      WEB UI (Pod Health Tab)                  │   │
│  │  [Summary] [Evicted] [Failed] [Pending] [CrashLoop] [Actions] │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Data Flow

```
1. DETECTION
   K8s API ──► List all pods ──► Filter non-Running ──► Get events

2. ANALYSIS
   Non-running pods ──► Categorize by status/reason ──► Root cause analysis
                    ──► Generate recommendations ──► Risk assessment

3. ACTION (on user request or auto)
   Selected pods ──► Validate safety ──► Execute action ──► Log result
                ──► Update UI ──► Send notification (optional)
```

---

## Detection Categories

### Pod Status Categories

| Category | Pod Phase | Container Status | Detection Logic |
|----------|-----------|------------------|-----------------|
| **Evicted** | Failed | - | `status.reason == "Evicted"` |
| **Failed** | Failed | - | `phase == "Failed" && reason != "Evicted"` |
| **Pending** | Pending | - | `phase == "Pending"` |
| **Unknown** | Unknown | - | `phase == "Unknown"` |
| **CrashLoopBackOff** | Running | Waiting | `container.state.waiting.reason == "CrashLoopBackOff"` |
| **ImagePullBackOff** | Pending/Running | Waiting | `reason in ["ImagePullBackOff", "ErrImagePull"]` |
| **OOMKilled** | Running | Terminated | `container.state.terminated.reason == "OOMKilled"` |
| **Error** | Running | Terminated | `container.state.terminated.exitCode != 0` |
| **Completed** | Succeeded | - | `phase == "Succeeded"` |
| **Terminating** | Running | - | `deletionTimestamp != null && age > 5m` |

### Root Cause Analysis

For each category, we perform additional analysis:

#### Pending Pods
```python
def analyze_pending(pod, events):
    reasons = []
    for event in events:
        if event.reason == "FailedScheduling":
            if "Insufficient cpu" in event.message:
                reasons.append({"cause": "insufficient_cpu", "detail": parse_cpu(event)})
            elif "Insufficient memory" in event.message:
                reasons.append({"cause": "insufficient_memory", "detail": parse_mem(event)})
            elif "node(s) had taint" in event.message:
                reasons.append({"cause": "taint_mismatch", "detail": parse_taint(event)})
            elif "node selector" in event.message:
                reasons.append({"cause": "selector_mismatch", "detail": parse_selector(event)})
        elif event.reason == "FailedMount":
            reasons.append({"cause": "volume_mount_failed", "detail": event.message})
    return reasons
```

#### CrashLoopBackOff Pods
```python
def analyze_crashloop(pod):
    analysis = {
        "restart_count": pod.status.restarts,
        "last_exit_code": get_last_exit_code(pod),
        "last_reason": get_last_termination_reason(pod),
        "owner": get_owner_reference(pod),
        "recommendations": []
    }
    
    if analysis["last_exit_code"] == 137:  # SIGKILL
        analysis["recommendations"].append("Check memory limits - pod may be OOMKilled")
    elif analysis["last_exit_code"] == 1:
        analysis["recommendations"].append("Application error - check logs")
    
    return analysis
```

---

## Actions & Remediation

### Action Matrix

| Category | Action | Risk | Auto-Safe | Requires |
|----------|--------|------|-----------|----------|
| Evicted | Delete | Low | ✅ Yes | - |
| Failed (>24h) | Delete | Low | ✅ Yes | Age check |
| Completed (>1h) | Delete | Low | ✅ Yes | Age check, not Job |
| CrashLoopBackOff | Restart Deploy | Medium | ❌ No | Owner check |
| CrashLoopBackOff | Delete Pod | Medium | ❌ No | Confirmation |
| Stuck Terminating | Force Delete | Medium | ❌ No | Confirmation |
| Pending | Diagnose | None | N/A | - |
| ImagePullBackOff | - | N/A | N/A | Manual fix |
| OOMKilled | Increase Limits | Medium | ❌ No | Owner check |

### Action Implementations

#### Delete Pod
```python
def delete_pod(namespace, name, force=False):
    """
    Delete a pod with optional force deletion.
    
    Args:
        namespace: Pod namespace
        name: Pod name
        force: If True, use grace_period=0 and foreground deletion
    
    Returns:
        ActionResult with success status and message
    """
    try:
        if force:
            body = client.V1DeleteOptions(
                grace_period_seconds=0,
                propagation_policy='Foreground'
            )
        else:
            body = client.V1DeleteOptions()
        
        core_api.delete_namespaced_pod(name, namespace, body=body)
        
        return ActionResult(
            success=True,
            action="delete_pod",
            target=f"{namespace}/{name}",
            message=f"Pod deleted successfully"
        )
    except ApiException as e:
        return ActionResult(
            success=False,
            action="delete_pod", 
            target=f"{namespace}/{name}",
            message=f"Failed: {e.reason}"
        )
```

#### Restart Deployment
```python
def restart_deployment(namespace, name):
    """
    Restart a deployment by patching its template annotation.
    
    This triggers a rolling update without changing the spec.
    """
    patch = {
        "spec": {
            "template": {
                "metadata": {
                    "annotations": {
                        "kubectl.kubernetes.io/restartedAt": datetime.utcnow().isoformat()
                    }
                }
            }
        }
    }
    
    try:
        apps_api.patch_namespaced_deployment(name, namespace, patch)
        return ActionResult(
            success=True,
            action="restart_deployment",
            target=f"{namespace}/{name}",
            message="Deployment restart initiated"
        )
    except ApiException as e:
        return ActionResult(success=False, action="restart_deployment",
                          target=f"{namespace}/{name}", message=f"Failed: {e.reason}")
```

#### Bulk Delete Evicted
```python
def delete_evicted_pods(namespaces=None, dry_run=False):
    """
    Delete all evicted pods, optionally filtering by namespace.
    
    Args:
        namespaces: List of namespaces to clean, or None for all
        dry_run: If True, return what would be deleted without deleting
    
    Returns:
        List of ActionResults
    """
    evicted = get_evicted_pods(namespaces)
    results = []
    
    for pod in evicted:
        if is_protected_namespace(pod.namespace):
            continue
            
        if dry_run:
            results.append(ActionResult(
                success=True,
                action="delete_pod_dry_run",
                target=f"{pod.namespace}/{pod.name}",
                message="Would delete"
            ))
        else:
            results.append(delete_pod(pod.namespace, pod.name))
    
    return results
```

---

## Safety Mechanisms

### 1. Protected Namespaces

```python
PROTECTED_NAMESPACES = {
    "kube-system",      # Core K8s components
    "kube-public",      # Public cluster info
    "kube-node-lease",  # Node heartbeats
}

def is_protected_namespace(namespace):
    return namespace in PROTECTED_NAMESPACES
```

### 2. Owner Awareness

Before deleting pods, check if they have owners:

```python
def get_owner_info(pod):
    """
    Get owner reference information for a pod.
    
    Returns info about the controller managing this pod,
    which helps users understand the impact of deletion.
    """
    if not pod.metadata.owner_references:
        return {"type": "standalone", "name": None, "warning": "No controller - pod will not recreate"}
    
    owner = pod.metadata.owner_references[0]
    return {
        "type": owner.kind,  # Deployment, ReplicaSet, Job, DaemonSet, etc.
        "name": owner.name,
        "controller": owner.controller,
        "warning": None if owner.kind in ["ReplicaSet", "Deployment"] else 
                   "Pod may not recreate automatically"
    }
```

### 3. Confirmation Requirements

```python
class ActionRequirements:
    """Define what's required before executing an action."""
    
    ACTIONS = {
        "delete_evicted": {
            "confirmation": False,  # Safe to auto-execute
            "dry_run_first": False,
            "max_batch": 100
        },
        "delete_failed": {
            "confirmation": True,
            "dry_run_first": True,
            "max_batch": 50,
            "min_age_hours": 24
        },
        "force_delete": {
            "confirmation": True,
            "dry_run_first": True,
            "max_batch": 10
        },
        "restart_deployment": {
            "confirmation": True,
            "dry_run_first": False,
            "max_batch": 5
        }
    }
```

### 4. Rate Limiting

```python
class ActionRateLimiter:
    """Prevent too many actions in a short time."""
    
    def __init__(self, max_actions_per_minute=30):
        self.max_actions = max_actions_per_minute
        self.action_times = []
    
    def can_execute(self):
        now = time.time()
        # Remove actions older than 1 minute
        self.action_times = [t for t in self.action_times if now - t < 60]
        return len(self.action_times) < self.max_actions
    
    def record_action(self):
        self.action_times.append(time.time())
```

### 5. Dry Run Mode

All destructive actions support dry-run:

```python
def execute_action(action, targets, dry_run=False):
    """
    Execute an action with optional dry-run mode.
    
    In dry-run mode, no changes are made but the full
    execution path is followed for accurate previews.
    """
    results = []
    for target in targets:
        if dry_run:
            # Validate but don't execute
            validation = validate_action(action, target)
            results.append({
                "target": target,
                "would_execute": validation.valid,
                "reason": validation.reason
            })
        else:
            results.append(execute_single_action(action, target))
    
    return results
```

### 6. Action Logging

```sql
CREATE TABLE action_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT NOT NULL,
    action_type TEXT NOT NULL,      -- delete_pod, restart_deployment, etc.
    target TEXT NOT NULL,           -- namespace/name
    initiated_by TEXT NOT NULL,     -- "auto", "manual", "api"
    dry_run BOOLEAN DEFAULT FALSE,
    success BOOLEAN NOT NULL,
    message TEXT,
    details TEXT                    -- JSON with additional context
);

CREATE INDEX idx_action_log_timestamp ON action_log(timestamp);
CREATE INDEX idx_action_log_action ON action_log(action_type);
```

---

## API Specification

### Endpoints

#### GET /api/v1/pods/unhealthy

List all non-running pods with analysis.

**Response:**
```json
{
  "timestamp": "2026-02-14T15:30:00Z",
  "summary": {
    "total": 24,
    "evicted": 12,
    "failed": 3,
    "pending": 5,
    "crashloop": 4
  },
  "pods": [
    {
      "name": "nginx-abc123",
      "namespace": "prod",
      "category": "evicted",
      "reason": "The node was low on resource: memory",
      "age": "2h15m",
      "owner": {"kind": "ReplicaSet", "name": "nginx-abc"},
      "safe_to_delete": true,
      "actions": ["delete"]
    }
  ]
}
```

#### GET /api/v1/pods/evicted

List evicted pods only.

**Response:**
```json
{
  "count": 12,
  "pods": [
    {
      "name": "nginx-abc123",
      "namespace": "prod",
      "node": "node-1",
      "eviction_time": "2026-02-14T13:15:00Z",
      "reason": "The node was low on resource: memory"
    }
  ]
}
```

#### DELETE /api/v1/pods/evicted

Delete all evicted pods.

**Request:**
```json
{
  "namespaces": ["prod", "staging"],  // Optional filter
  "dry_run": false
}
```

**Response:**
```json
{
  "success": true,
  "deleted": 12,
  "failed": 0,
  "results": [
    {"namespace": "prod", "name": "nginx-abc123", "success": true}
  ]
}
```

#### DELETE /api/v1/pods/{namespace}/{name}

Delete a specific pod.

**Query Parameters:**
- `force=true` - Force delete with grace_period=0

**Response:**
```json
{
  "success": true,
  "message": "Pod deleted successfully"
}
```

#### POST /api/v1/deployments/{namespace}/{name}/restart

Restart a deployment.

**Response:**
```json
{
  "success": true,
  "message": "Deployment restart initiated",
  "affected_pods": 3
}
```

#### GET /api/v1/actions/log

Get action history.

**Query Parameters:**
- `limit=50` - Number of records
- `action=delete_pod` - Filter by action type
- `since=2026-02-14T00:00:00Z` - Filter by time

**Response:**
```json
{
  "actions": [
    {
      "id": 123,
      "timestamp": "2026-02-14T15:30:00Z",
      "action": "delete_pod",
      "target": "prod/nginx-abc123",
      "initiated_by": "manual",
      "success": true,
      "message": "Pod deleted successfully"
    }
  ]
}
```

#### GET /api/v1/actions/dry-run

Preview what would be cleaned up.

**Response:**
```json
{
  "evicted": {
    "count": 12,
    "pods": ["prod/nginx-abc123", "prod/api-def456"]
  },
  "old_failed": {
    "count": 3,
    "pods": ["jobs/worker-xyz789"]
  },
  "old_completed": {
    "count": 8,
    "pods": ["jobs/backup-111", "jobs/backup-222"]
  },
  "total_to_clean": 23
}
```

---

## UI Design

### Pod Health Tab Layout

```
┌─────────────────────────────────────────────────────────────────────┐
│  POD HEALTH                                            [⚙️ Settings]│
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌───────────────────────────────────────────────────────────────┐  │
│  │  SUMMARY                                                       │  │
│  │  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐ │  │
│  │  │   12    │ │    3    │ │    5    │ │    4    │ │    1    │ │  │
│  │  │ Evicted │ │ Failed  │ │ Pending │ │  Crash  │ │  Stuck  │ │  │
│  │  │  [🗑️]   │ │  [👁️]   │ │  [👁️]   │ │  [🔄]   │ │  [⚠️]   │ │  │
│  │  └─────────┘ └─────────┘ └─────────┘ └─────────┘ └─────────┘ │  │
│  └───────────────────────────────────────────────────────────────┘  │
│                                                                      │
│  ┌───────────────────────────────────────────────────────────────┐  │
│  │  QUICK ACTIONS                                                 │  │
│  │                                                                │  │
│  │  [🗑️ Delete All Evicted (12)]  [Preview]                      │  │
│  │  [🗑️ Delete Old Failed (3)]    [Preview]                      │  │
│  │  [🗑️ Delete Old Completed (8)] [Preview]                      │  │
│  │                                                                │  │
│  └───────────────────────────────────────────────────────────────┘  │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────────┐│
│  │ EVICTED PODS (12)                    [☑️ Select All] [🗑️ Delete]││
│  ├─────────────────────────────────────────────────────────────────┤│
│  │ ☑️ │ nginx-abc123   │ prod   │ node-1 │ 2h ago  │ Memory     │  ││
│  │ ☑️ │ api-def456     │ prod   │ node-2 │ 3h ago  │ Memory     │  ││
│  │ ☑️ │ worker-ghi789  │ jobs   │ node-1 │ 1h ago  │ Disk       │  ││
│  │ ...                                                            │  ││
│  └─────────────────────────────────────────────────────────────────┘│
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────────┐│
│  │ CRASHLOOP PODS (4)                                              ││
│  ├─────────────────────────────────────────────────────────────────┤│
│  │ ⚠️ │ worker-xyz789  │ jobs │ 45 restarts │ Exit code: 1       │ ││
│  │    │ Owner: Deployment/worker                                  │ ││
│  │    │ Last log: "Connection refused to database"                │ ││
│  │    │ [📜 View Logs] [🔄 Restart Deploy] [🗑️ Delete Pod]       │ ││
│  │────┼───────────────────────────────────────────────────────────│ ││
│  │ ⚠️ │ api-aaa111     │ prod │ 12 restarts │ OOMKilled          │ ││
│  │    │ Owner: Deployment/api                                     │ ││
│  │    │ Memory limit: 256Mi, Consider increasing                  │ ││
│  │    │ [📜 View Logs] [🔄 Restart Deploy] [📈 Increase Memory]  │ ││
│  └─────────────────────────────────────────────────────────────────┘│
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────────┐│
│  │ PENDING PODS (5)                                                ││
│  ├─────────────────────────────────────────────────────────────────┤│
│  │ ⏳ │ big-job-111    │ batch │ 1h 15m │ Insufficient cpu       │ ││
│  │    │ Requested: 4 CPU │ Available on any node: 2 CPU          │ ││
│  │    │ Recommendation: Reduce CPU request or add nodes           │ ││
│  │    │ [📋 View Events] [📊 Node Resources]                     │ ││
│  └─────────────────────────────────────────────────────────────────┘│
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────────┐│
│  │ ACTION LOG                                           [View All] ││
│  ├─────────────────────────────────────────────────────────────────┤│
│  │ 15:30 │ Deleted 5 evicted pods    │ Auto   │ ✅ Success       │ ││
│  │ 15:25 │ Restarted deployment/api  │ Manual │ ✅ Success       │ ││
│  │ 15:20 │ Force deleted stuck pod   │ Manual │ ✅ Success       │ ││
│  │ 14:30 │ Deleted 3 evicted pods    │ Auto   │ ✅ Success       │ ││
│  └─────────────────────────────────────────────────────────────────┘│
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Confirmation Modal

```
┌─────────────────────────────────────────────────────────────────┐
│  ⚠️  Confirm Action                                       [X]   │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  You are about to delete 12 evicted pods:                       │
│                                                                  │
│  Namespaces affected:                                           │
│    • prod (8 pods)                                              │
│    • staging (3 pods)                                           │
│    • jobs (1 pod)                                               │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  ℹ️  These pods were evicted by Kubernetes due to node    │   │
│  │     resource pressure. They will not be recreated         │   │
│  │     unless their parent controllers are still running.    │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│                          [Cancel]  [Delete 12 Pods]             │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Settings Panel

```
┌─────────────────────────────────────────────────────────────────┐
│  ⚙️  Pod Health Settings                                  [X]   │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  AUTO-CLEANUP                                                    │
│  ─────────────────────────────────────────────────────────────  │
│                                                                  │
│  ☑️  Auto-delete evicted pods                                   │
│      └─ Interval: [Every 30 minutes ▼]                          │
│                                                                  │
│  ☑️  Auto-delete completed pods older than                      │
│      └─ [1 hour ▼]                                              │
│                                                                  │
│  ☐  Auto-delete failed pods older than                          │
│      └─ [24 hours ▼]                                            │
│                                                                  │
│  PROTECTED NAMESPACES                                            │
│  ─────────────────────────────────────────────────────────────  │
│                                                                  │
│  [kube-system] [kube-public] [+ Add namespace]                  │
│                                                                  │
│  NOTIFICATIONS                                                   │
│  ─────────────────────────────────────────────────────────────  │
│                                                                  │
│  ☑️  Notify when auto-cleanup runs                              │
│  ☑️  Notify when new issues detected                            │
│                                                                  │
│                                     [Cancel]  [Save Settings]   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `POD_HEALTH_ENABLED` | `true` | Enable pod health features |
| `AUTO_DELETE_EVICTED` | `true` | Auto-delete evicted pods |
| `AUTO_DELETE_EVICTED_INTERVAL` | `1800` | Seconds between auto-cleanup |
| `AUTO_DELETE_COMPLETED_AFTER` | `3600` | Seconds before deleting completed pods |
| `AUTO_DELETE_FAILED_AFTER` | `86400` | Seconds before deleting failed pods |
| `PROTECTED_NAMESPACES` | `kube-system,kube-public` | Comma-separated list |
| `DRY_RUN_MODE` | `false` | Preview actions without executing |
| `MAX_BATCH_SIZE` | `100` | Maximum pods to delete in one batch |
| `RATE_LIMIT_PER_MINUTE` | `30` | Maximum actions per minute |

### ConfigMap Configuration

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: cluster-intel-config
  namespace: cluster-intel
data:
  pod-health.yaml: |
    enabled: true
    
    auto_cleanup:
      evicted:
        enabled: true
        interval_seconds: 1800
      completed:
        enabled: true
        min_age_seconds: 3600
        exclude_job_pods: false
      failed:
        enabled: false
        min_age_seconds: 86400
    
    safety:
      protected_namespaces:
        - kube-system
        - kube-public
        - kube-node-lease
      dry_run: false
      require_confirmation: true
      max_batch_size: 100
      rate_limit_per_minute: 30
    
    notifications:
      on_auto_cleanup: true
      on_new_issues: true
      webhook_url: ""
```

---

## Implementation Plan

### Phase 1: Detection (Week 1)

**Tasks:**
1. Implement pod health detector
2. Add categorization logic
3. Create root cause analyzer
4. Add `/api/v1/pods/unhealthy` endpoint
5. Add basic UI tab with summary

**Deliverables:**
- Detection of all non-running pod categories
- API endpoint returning categorized pods
- UI showing pod health summary

### Phase 2: Manual Actions (Week 2)

**Tasks:**
1. Implement delete pod action
2. Implement force delete action
3. Implement restart deployment action
4. Add action logging to database
5. Create action confirmation modals
6. Add individual pod action buttons in UI

**Deliverables:**
- Manual delete/restart from UI
- Action logging with history
- Confirmation dialogs

### Phase 3: Bulk Actions (Week 3)

**Tasks:**
1. Implement bulk delete evicted
2. Implement bulk delete old completed
3. Add dry-run preview functionality
4. Create bulk action UI with selection
5. Add action result notifications

**Deliverables:**
- One-click cleanup of evicted pods
- Preview before bulk actions
- Result notifications

### Phase 4: Automation (Week 4)

**Tasks:**
1. Implement auto-cleanup scheduler
2. Add configuration options
3. Create settings UI panel
4. Implement rate limiting
5. Add protected namespace checks
6. Integration testing

**Deliverables:**
- Automated evicted pod cleanup
- Configurable auto-cleanup rules
- Settings panel in UI

### Phase 5: Polish & Documentation (Week 5)

**Tasks:**
1. Performance optimization
2. Error handling improvements
3. UI/UX refinements
4. Documentation updates
5. Security review
6. Load testing

**Deliverables:**
- Production-ready feature
- Updated documentation
- Security sign-off

---

## Security Considerations

### RBAC Requirements

Additional permissions needed for pod deletion:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cluster-intel-pod-manager
rules:
  # Existing read permissions...
  
  # New delete permissions
  - apiGroups: [""]
    resources: [pods]
    verbs: [delete]
  
  # Deployment restart (patch)
  - apiGroups: ["apps"]
    resources: [deployments]
    verbs: [patch]
  
  # Read events for root cause
  - apiGroups: [""]
    resources: [events]
    verbs: [list, watch]
```

### Audit Requirements

All destructive actions must be logged with:
- Timestamp
- Action type
- Target resource
- Initiator (user/auto)
- Result (success/failure)
- Error message if failed

### Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Accidental deletion of important pods | Protected namespaces, confirmation dialogs |
| Runaway automation | Rate limiting, max batch size |
| Deletion of pods that shouldn't be deleted | Owner awareness, dry-run mode |
| Unauthorized access | RBAC, action logging |
| Data loss | Pods managed by controllers will recreate |

---

## Testing Strategy

### Unit Tests

```python
def test_categorize_evicted_pod():
    pod = create_mock_pod(phase="Failed", reason="Evicted")
    result = categorize_pod(pod)
    assert result.category == "evicted"
    assert result.safe_to_delete == True

def test_protected_namespace():
    assert is_protected_namespace("kube-system") == True
    assert is_protected_namespace("prod") == False

def test_rate_limiter():
    limiter = ActionRateLimiter(max_actions_per_minute=2)
    assert limiter.can_execute() == True
    limiter.record_action()
    limiter.record_action()
    assert limiter.can_execute() == False
```

### Integration Tests

```python
def test_delete_evicted_pod(k8s_cluster):
    # Create evicted pod
    pod = create_evicted_pod(k8s_cluster, "test-ns", "test-pod")
    
    # Verify detection
    unhealthy = get_unhealthy_pods(k8s_cluster)
    assert "test-ns/test-pod" in [p.full_name for p in unhealthy.evicted]
    
    # Delete
    result = delete_pod("test-ns", "test-pod")
    assert result.success == True
    
    # Verify deleted
    assert not pod_exists(k8s_cluster, "test-ns", "test-pod")

def test_dry_run_mode(k8s_cluster):
    # Create evicted pods
    create_evicted_pod(k8s_cluster, "test-ns", "pod-1")
    create_evicted_pod(k8s_cluster, "test-ns", "pod-2")
    
    # Dry run
    result = delete_evicted_pods(dry_run=True)
    assert result.would_delete == 2
    
    # Verify not deleted
    assert pod_exists(k8s_cluster, "test-ns", "pod-1")
    assert pod_exists(k8s_cluster, "test-ns", "pod-2")
```

### E2E Tests

1. Create cluster with various unhealthy pods
2. Verify UI displays all categories correctly
3. Test manual delete via UI
4. Test bulk delete with confirmation
5. Verify action log entries
6. Test auto-cleanup feature
7. Verify protected namespaces respected

---

## Rollout Plan

### Stage 1: Internal Testing
- Deploy to dev cluster
- Test all features manually
- Run automated test suite
- Fix any issues found

### Stage 2: Beta Release
- Deploy with `dry_run: true` by default
- Enable for select users
- Gather feedback
- Monitor for issues

### Stage 3: General Availability
- Enable `dry_run: false` 
- Auto-cleanup disabled by default
- Full documentation published
- Support team trained

### Rollback Plan

If issues are discovered:
1. Disable auto-cleanup immediately
2. Assess impact of any unintended deletions
3. Pods managed by controllers should auto-recreate
4. Review action logs to identify affected resources
5. Communicate with affected users

---

## Appendix

### A. Pod Phase Reference

| Phase | Description |
|-------|-------------|
| Pending | Accepted but not running |
| Running | At least one container running |
| Succeeded | All containers terminated successfully |
| Failed | All containers terminated, at least one failed |
| Unknown | State cannot be determined |

### B. Container State Reference

| State | Sub-state | Common Reasons |
|-------|-----------|----------------|
| Waiting | - | CrashLoopBackOff, ImagePullBackOff, CreateContainerConfigError |
| Running | - | Container is executing |
| Terminated | - | Completed, Error, OOMKilled, Evicted |

### C. Common Exit Codes

| Code | Signal | Meaning |
|------|--------|---------|
| 0 | - | Success |
| 1 | - | General error |
| 126 | - | Permission denied |
| 127 | - | Command not found |
| 128+n | n | Terminated by signal n |
| 137 | SIGKILL (9) | Killed (often OOM) |
| 143 | SIGTERM (15) | Graceful termination |

---

*Document version: 1.0*  
*Last updated: February 2026*

# K8s Cluster Intelligence Engine - Local Context

This document is an aggregated context file containing the summary of all documents present in the `docs/` folder for the K8s Cluster Intelligence project.

## 1. Architecture Overview (`ARCHITECTURE.md`)
- The system is composed of multiple layers: Data Collection (K8s API, Prometheus, OTel, Audit Logs), Processing Pipeline (normalization, aggregation), LLM Analysis, and Recommendation Engine.
- End-user interfaces consist of a React/Next.js dashboard and a comprehensive API Server.
- Employs Redis for caching, TimescaleDB for metrics, and PostgreSQL for storage.

## 2. API Contracts (`API_CONTRACTS.md`)
- Outlines the core JSON data structures used by the REST API, such as `ClusterHealthReport`, `TelemetryEvent`, `ResourceMetrics`, and `Recommendation`.
- The API supports operations for namespaces, recommendations, security, cost, analysis, and real-time events (via WebSocket and GraphQL).

## 3. Scoring System (`SCORING_SYSTEM.md`)
- Defines how the unified Health Score is calculated based on four dimensions:
  - **Reliability (35%)**: Penalty for CrashLoopBackOff, Node NotReady, limits missing.
  - **Security (30%)**: Penalty for privileged access, critical CVEs, lacking network policies.
  - **Cost (20%)**: Efficiency of CPU, memory, and storage utilization.
  - **Architecture (15%)**: Best practices like multi-zone deployments and PDBs.
- Priorities for recommendations are assigned based on a matrix of impact vs. effort along with risk multipliers.

## 4. Pod Health Management (`POD_HEALTH_MANAGEMENT.md`)
- Provides the ability to detect, categorize, and remediate non-running pods (e.g., Evicted, CrashLoopBackOff).
- Includes an Action Matrix denoting safe automatic remediations (like deleting old Evicted/Failed pods) vs. manual actions.
- Features safety mechanisms like Protected Namespaces and Dry-Run modes.

## 5. LLM Orchestration (`LLM_ORCHESTRATION.md`)
- The backend leverages Large Language Models to diagnose incidents, propose right-sizing for resources, and explain architectural or security gaps.
- Supports multiple LLM backends (OpenAI, Anthropic, local Ollama, Azure, vLLM).
- Detailed Token Budget Allocation and JSON extraction logic is employed for automated parsing of AI outputs.

## 6. Implementation and Test Status (`IMPLEMENTATION_VERIFICATION.md` & `TEST_OBSERVATIONS.md`)
- The version 6.0 features robust standalone functionality (`src/simple/app.py`) with full API and UI feature coverage.
- Code quality audits identified a few remaining warnings primarily dealing with Node packages (Next.js 14 -> 16 required for full security resolution) and deprecation of `datetime.utcnow()` in K8s deployment manifests requiring updates.
- End-to-end frontend tests and demo modes are largely successful.

## 7. UI Improvements (`UI_IMPROVEMENTS.md`)
- Documented 47 resolved issues spanning Accessibility (ARIA labels, focus states), UX/UI Design, Responsiveness, and Code Quality.
- Brought improvements including skeleton loaders, toast notifications, search/filter bars, and responsive modalities to ensure WCAG AA compliance.

## 8. Incident Report (`incident-investigation-2026-02-22.md`)
- Root cause analysis of massive network usage (17TB+ bandwidth across several days) due to Tailscale DERP fallback relays failing to connect P2P between Lima VMs, thus multiplying the traffic overhead.
- Triggered repeated Calico network resynchronization due to node flapping.

## 9. Roadmap (`ROADMAP.md`)
- **Version 6.0 (Current/WIP)**: Focuses on Resource Right-sizing, Prometheus Metrics integration, LLM foundation, and Advanced Alerting.
- **Future Visions (v7.0/v8.0)**: Multi-cluster federation, GitOps, OPA integration, deeper runtime security (Falco), and secrets management.

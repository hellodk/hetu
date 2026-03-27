# Changelog

All notable changes to K8s Cluster Intelligence Engine will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [6.0.0] - 2026-02-28

### Added
- **Restored UI Tabs**: Re-integrated the Pod Health, Resources, Alerts, Cleanup, and Trends tabs into the main dashboard.
- **API Endpoints**: Re-established and wired up necessary API routes for frontend data fetching.
- **Bug Fix**: Fixed `Invalid Date` parsing issue related to timestamp timezone markers.

## [4.1.0] - 2026-02-15

### Added
- **Cluster Cleanup Detection**: New Cleanup tab to identify and remove stale resources
  - Stale ReplicaSets (0 replicas, >1 day old)
  - Completed/Failed Jobs
  - Evicted Pods
  - Orphaned PVCs
  - Unused ConfigMaps and Secrets (>30 days)
  - Empty Namespaces (>7 days)
- **CrashLoopBackOff Root Cause Analysis**: Detailed crash reason detection
  - Exit code analysis (OOMKilled, SIGKILL, SIGSEGV, etc.)
  - Liveness/Readiness probe failure detection
  - Config/Volume mount error detection
- **Remediation Steps**: Fix button with kubectl commands for each issue type
- **Semantic Versioning**: VERSION file and bump-version.sh script
- **Pre-deploy Validation**: Automated checks before deployment

### Fixed
- Fixed `NoneType` error in CIS benchmark checks for securityContext
- Fixed Resource Optimization "View Fix" button escaping issues
- Fixed capabilities check when securityContext is null

### Changed
- Enhanced Pod Health UI with root cause display
- Added exit code display in pod details
- Improved event message display for troubleshooting

## [4.0.0] - 2026-02-14

### Added
- Full-stack deployment with Trivy integration
- CIS Kubernetes Benchmark compliance checks
- Vulnerability scanning dashboard
- Resource optimization recommendations
- Historical trend analysis
- Alert webhook integration (Slack, Discord)

### Changed
- Complete UI redesign with dark mode support
- Improved scoring algorithm

## [3.0.0] - 2026-02-10

### Added
- Pod Health Management system
- Bulk cleanup operations
- Protected namespace support

## [2.0.0] - 2026-02-01

### Added
- Multi-cluster support
- Prometheus integration
- Grafana dashboards

## [1.0.0] - 2026-01-15

### Added
- Initial release
- Basic cluster health scoring
- Pod status monitoring
- Node health checks

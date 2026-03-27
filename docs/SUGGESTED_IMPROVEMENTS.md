# K8s Cluster Intelligence Engine - Suggested Improvements

Based on a deep analysis of the project documentation, codebase structure, and execution of the application and test suites, the following improvements are strongly recommended:

## 1. Security & Dependency Upgrades
- **Next.js & React Upgrade**: High severity vulnerabilities remain regarding `glob` and Next.js DoS vectors. The dashboard should be refactored and upgraded to Next.js 16.x (from the current 14.x) to resolve these transitive dependency issues.
- **Python Security Scanning**: Incorporate tools like `bandit` and `safety` into the backend for dependency vulnerability scanning.

## 2. Technical Debt & Code Fixes
- **Manifest Deprecations**: While `datetime.utcnow()` was fixed in `app.py`, the `manifests/simple/deployment.yaml` file still references this deprecated function in its probes or annotations (as noted in `IMPLEMENTATION_VERIFICATION.md`). This must be updated to use Python's timezone-aware `datetime.now(timezone.utc)`.
- **Mock Data Elimination**: Both `TimelineChart.tsx` and `ResourceUtilization.tsx` currently render static mock data. Since the backend now provides `/api/v1/history` and `/api/v1/health`, these components must be hooked up to the real API endpoints to accurately reflect cluster state metrics.

## 3. Architecture & Networking (Based on Incident Report)
- **DERP Fallback Loop Mitigation**: The massive 17TB incident caused by Tailscale DERP relays multiplying Calico VXLAN traffic points to networking inefficiencies on the Lima VMs. 
  - **Action**: Switch Lima VMs from NAT to bridged network mode (in `lima.yaml`) so nodes receive direct LAN IPs, allowing strict P2P Tailscale connections. 
  - **Action**: Modify Calico `IPPool` to use `vxlanMode: CrossSubnet` instead of `Always` to reduce encapsulation overhead on the local LAN.

## 4. Testing Infrastructure
- **Missing Backend Tests**: The `tests/` directory referenced in the README for the Python backend is notably missing. A complete pytest suite needs to be established to cover API endpoint functional tests, logic verification, and scoring functions prior to a production environment rollout.
- **Comprehensive E2E Integration**: The Puppeteer UI tests currently live loosely in the root folder (`test-*.js`). These should be integrated directly into a CI/CD pipeline (using GitHub Actions or GitLab CI) or properly structured under an `e2e/` testing directory within the dashboard, managed by a tool like Cypress or Playwright for standard reporting.

## 5. Roadmap Continuation
- **Advanced Alerting & External Integrations**: Proceed with integrating OpsGenie and PagerDuty endpoints alongside the newly created Slack webhooks in the alerting engine.
- **Enhanced LLM Features**: Leverage the local Ollama connection by implementing automated, periodic background scans that generate proactive right-sizing suggestions sent as weekly digest alerts.

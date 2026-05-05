<!-- torque apply plan: release=showcase namespace=showcase -->

## torque apply plan: `showcase`

| Field | Value |
| --- | --- |
| Release | `showcase` |
| Namespace | `showcase` |
| Chart | testdata/charts/verify-findings |
| Chart version | 0.1.0 |
| Cluster | https://127.0.0.1:65535 |
| Generated | 2026-05-05T20:43:28Z |
| Risk | High |
| Rollback | `helm rollback showcase -n showcase` |

### Risk Summary

- 1 planner warning(s) recorded.
- 1 image reference(s) are not pinned by digest.
- 5 critical/high verifier finding(s) present.
- Plan used offline fallback; live-state confidence is reduced.

### Planned Changes

Creates: **1**, updates: **0**, deletes: **0**, unchanged: **0**.

| Change | Resource |
| --- | --- |
| Create | `cluster/verify-findings-showcase Deployment (apps)` |

### Blast Radius

- 1 changed resource(s) across 1 namespace/scope value(s).
- Namespaces: (cluster/default)=1
- Kinds: Deployment=1

### Images

| Resource | Container | Image | Digest pinned |
| --- | --- | --- | --- |
| `cluster/verify-findings-showcase Deployment (apps)` | app | `nginx:stable` | no |

### Build Provenance

No `torque build` capture is attached. Pass `--build-capture build.sqlite` to prove whether the exact built digest is referenced by this plan.

### Secret References

No `secret://` references were resolved for this plan.

### Quota And Headroom

| Namespace | Desired | Headroom | Warnings |
| --- | --- | --- | --- |
| showcase | pods=1, cpuReq=0, memReq=0, services=0, secrets=0, pvcs=0 | unknown | 2 |

### Policy Findings

| Report | Status | Findings | Rendered digest |
| --- | --- | --- | --- |
| docs/showcase/reports/verifier-report.json | passed | total=14 critical=1 high=4 medium=4 low=3 info=2 | match |

Top findings:

| Severity | Rule | Subject | Message |
| --- | --- | --- | --- |
| critical | k8s/pss_restricted_profile | cluster/Deployment/verify-findings-showcase | PSS restricted profile failed: ape, drop_all, run_as_non_root, seccomp |
| high | k8s/container_allow_privilege_escalation_disabled | cluster/Deployment/verify-findings-showcase | Containers should set securityContext.allowPrivilegeEscalation=false to reduce the risk of privilege escalation. |
| high | k8s/container_capabilities_drop_all | cluster/Deployment/verify-findings-showcase | Containers should drop all Linux capabilities (securityContext.capabilities.drop includes 'ALL'). |
| high | k8s/container_run_as_non_root | cluster/Deployment/verify-findings-showcase | Containers should set securityContext.runAsNonRoot to true to avoid running workloads as root. |
| high | k8s/pod_seccomp_profile_runtime_default | cluster/Deployment/verify-findings-showcase | Pods and containers should configure seccompProfile.type (prefer RuntimeDefault) to reduce kernel attack surface. |
| medium | k8s/container_read_only_root_filesystem | cluster/Deployment/verify-findings-showcase | Containers should set securityContext.readOnlyRootFilesystem to true to reduce the writable surface area. |
| medium | k8s/memory_limits_not_defined | cluster/Deployment/verify-findings-showcase | Memory limits should be defined for each container. This prevents potential resource exhaustion by ensuring that containers consume not more than the designated amount of memory |
| medium | k8s/net_raw_capabilities_not_being_dropped | cluster/Deployment/verify-findings-showcase | Containers should drop 'ALL' or at least 'NET_RAW' capabilities |
| medium | k8s/service_account_token_automount_not_disabled | cluster/Deployment/verify-findings-showcase | Service Account Tokens are automatically mounted even if not necessary |
| low | k8s/container_liveness_probe_required | cluster/Deployment/verify-findings-showcase | Containers should define a livenessProbe to help Kubernetes restart unhealthy workloads. |
| low | k8s/container_readiness_probe_required | cluster/Deployment/verify-findings-showcase | Containers should define a readinessProbe to help Kubernetes route traffic only to ready workloads. |
| low | k8s/pod_or_container_without_security_context | cluster/Deployment/verify-findings-showcase | A security context defines privilege and access control settings for a Pod or Container |
| info | k8s/pod_fs_group_recommended | cluster/Deployment/verify-findings-showcase | Recommend setting pod securityContext.fsGroup to a non-zero GID for volume file ownership defaults. |
| info | k8s/pod_run_as_user_non_root_recommended | cluster/Deployment/verify-findings-showcase | Recommend setting securityContext.runAsUser to a non-zero UID (defense in depth). |

### Manifest Diffs

No manifest diff hunks were recorded.

### Warnings

- Live lookup failed (fetch cluster/verify-findings-showcase Deployment (apps): Get "https://127.0.0.1:65535/api?timeout=30s": dial tcp 127.0.0.1:65535: connect: connection refused); falling back to previous release manifest.

### API Timings

- Total: 6ms
- Kubernetes API requests: 2 (avg 1ms, max 2ms)

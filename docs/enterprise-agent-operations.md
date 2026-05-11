# Enterprise Agent Operations

This is the operating baseline for Torque deployments where agents can build,
ship, or inspect Kubernetes releases through MCP and `torque-agent`.

## Policy Baseline

Treat every `torque-agent` as a privileged execution boundary. The agent does
not replace Kubernetes RBAC, registry IAM, BuildKit policy, or MCP write
confirmation; it concentrates those controls behind one auditable endpoint.

| Mode | Required controls | Intended use |
| --- | --- | --- |
| Local dev | loopback bind, short-lived token, local kubecontext | single-developer testing |
| Team remote | TLS, bearer token, scoped kubeconfig, mirror store, retention limits | shared remote builder/cluster |
| Enterprise | mTLS, bearer token as second factor, namespace/context allowlists in MCP config, scoped Kubernetes RBAC, durable mirror store, evidence required for writes | production-like agent automation |

Do not expose plaintext gRPC or the HTTP mirror gateway on a public interface.
If HTTP access is needed, put it behind an authenticated TLS proxy and keep the
same bearer token checks enabled.

## Secure MCP Server Posture

A secure MCP server should stay boring and narrow. Prefer stdio for local agent
hosts. If HTTP is needed, bind it to loopback, require bearer auth, validate
browser origins, cap request sizes, enforce tool timeouts, and put any public
access behind a real TLS proxy. Do not turn MCP into a generic shell or direct
Kubernetes tunnel; keep it as a typed contract with small tools, bounded inputs,
redacted outputs, and clear write gates.

The target posture is MCP over stdio or authenticated loopback HTTP, remote
gRPC over mTLS, writes disabled unless explicitly enabled, secrets never
crossing MCP, and every action replayable from evidence.

For direct CLI-driven agents, use `torque agent` as the proof-backed permission
boundary before a mutating action is invoked:

```bash
torque agent policy check agent-request.json \
  --proof proof.graph.json \
  --allow apply \
  --require-gate

torque agent run agent-request.json \
  --proof proof.graph.json \
  --allow apply \
  --require-gate \
  --out agent-run.json
```

The policy check requires explicit operation permission plus a passing release
gate. `agent run` writes an authorization record and does not mutate the
cluster itself.

## mTLS-First Remote Bridge

Run the remote endpoint with client certificate verification, a scoped
kubeconfig, and durable MirrorService evidence:

```bash
export TORQUE_REMOTE_TOKEN="$(openssl rand -hex 32)"

torque-agent \
  -listen 0.0.0.0:7443 \
  -token "$TORQUE_REMOTE_TOKEN" \
  -kubeconfig /etc/torque/prod-reader-writer.kubeconfig \
  -context prod \
  -tls-cert /etc/torque/tls/agent.crt \
  -tls-key /etc/torque/tls/agent.key \
  -tls-client-ca /etc/torque/tls/client-ca.crt \
  -mirror-store /var/lib/torque/agent/mirror.sqlite \
  -mirror-max-sessions 500 \
  -mirror-max-frames 20000 \
  -mirror-max-bytes 5000000000
```

For a Linux host where loopback MCP is enough, use the systemd installer and
then add TLS/proxying only at the network boundary:

```bash
curl -fsSL https://ingresslabs.github.io/torque/install.sh | sh -s -- --mode systemd-daemon
sudo systemctl status torque-agent.service torque-mcp.service
. /etc/torque/agent.env
curl -fsS -H "authorization: Bearer $TORQUE_MCP_TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' \
  http://127.0.0.1:7331/mcp
```

Connect the MCP bridge with the same TLS material and keep the token out of
argv when launched by an agent host:

```bash
export TORQUE_REMOTE_TOKEN="<from secret manager>"
export TORQUE_REMOTE_TLS=1
export TORQUE_REMOTE_TLS_CA=/etc/torque/tls/ca.crt
export TORQUE_REMOTE_TLS_CLIENT_CERT=/etc/torque/tls/client.crt
export TORQUE_REMOTE_TLS_CLIENT_KEY=/etc/torque/tls/client.key
export TORQUE_REMOTE_TLS_SERVER_NAME=torque-agent.prod.internal

torque-mcp --stdio \
  --remote-agent torque-agent.prod.internal:7443 \
  --enable-write
```

The direct CLI uses the same transport controls:

```bash
torque \
  --remote-agent torque-agent.prod.internal:7443 \
  --remote-token "$TORQUE_REMOTE_TOKEN" \
  --remote-tls \
  --remote-tls-ca /etc/torque/tls/ca.crt \
  --remote-tls-client-cert /etc/torque/tls/client.crt \
  --remote-tls-client-key /etc/torque/tls/client.key \
  --remote-tls-server-name torque-agent.prod.internal \
  stack status --config ./stacks/prod --follow
```

## Evidence Requirements

Every mutating agent action should produce or link evidence before it is
considered complete:

- `torque.build.run`: build capture or MirrorService session, image digest, cache summary, policy/secrets report when enabled.
- `torque.cache.inspect` / `torque.cache.plan`: normalized cache imports/exports, S3 manifest name, changed-path impact classes, and warm target list.
- `torque.cache.warm`: MirrorService session, remote `BuildService.RunBuild` result, and normalized cache export target.
- `torque.ship.run`: evidence directory, build capture, verifier report, plan output, apply capture, explain report.
- `torque.apply.run` / `torque.delete.run`: capture DB, plan digest or delete selection, release/namespace metadata.
- `torque.stack.apply` / `torque.stack.delete`: stack run ID, frozen plan, run status, MirrorService replay.
- CLI agent writes: `agent-policy.json`, `agent-run.json`, release proof graph, release score, and release flight when a proof gate is required.
- S3 cache validation: BuildKit S3 import/export lines, cache object prefix, cleanup record for disposable buckets/builders.

MCP responses should return compact summaries plus resource links. Large logs,
captures, and mirror frames stay behind `torque://...` resources or exported
artifacts so agents do not need to scrape terminal output.

## Scenario Matrix

The published regression matrix is
`testdata/mcp/dag-build-scenarios.yaml`. It contains sixteen scenarios that
cover remote MCP, gRPC, build, ship, stack, S3 cache, microVM, mirror replay,
write confirmation, and token redaction paths.

| Scenario | Shape | Main proof |
| --- | --- | --- |
| `remote-ship-core-fanout` | fanout | cache inspect/plan/warm, MCP build, and ship through remote Build/Deploy/Stack services |
| `compose-diamond-promotion` | diamond | Compose build fan-in with verifier and mirror frames |
| `stack-progressive-large-dag` | layered | remote StackService plan/apply/status over a larger graph |
| `multi-namespace-delete-threshold` | multi-root | delete confirmation and threshold policy |
| `secret-provider-build-and-ship` | chain | secret references stay redacted through build and ship |
| `mcp-http-origin-remote-stack` | ingress | HTTP MCP origin and token checks |
| `mirror-session-replay-build-to-apply` | event-bus | replayable Build/Deploy/Stack sessions |
| `microvm-mcp-smoke-before-deploy` | preflight | static MCP binary boots and advertises tools in microVMs |
| `fanin-verifier-gate` | fanin | verifier gate blocks stack apply until all digests are present |
| `rollback-delete-after-failed-ship` | rollback | failed ship evidence drives selective stack delete |

`TestMCPDAGBuildScenarioCatalog` keeps the matrix executable enough to be
useful: it checks scenario count, unique IDs, acyclic edges, required MCP calls,
remote gRPC coverage, write confirmation coverage, and remote token redaction.

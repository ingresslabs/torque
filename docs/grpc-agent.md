# gRPC Agent API (torque-agent)

`torque-agent` exposes `torque` capabilities over gRPC for automation and AI agents:

- Logs: `LogService.StreamLogs`
- Builds: `BuildService.RunBuild`
- Deploy: `DeployService.Apply` / `DeployService.Destroy`
- Verify: `VerifyService.Verify`
- Mirror bus: `MirrorService.Publish` / `MirrorService.Subscribe`
- Agent metadata: `AgentInfoService.GetInfo`

The API definitions live in `proto/torque/api/v1/agent.proto`.

## Running torque-agent

```bash
go install ./cmd/torque-agent

# Insecure gRPC by default (plaintext). Prefer SSH tunnels or a private network.
torque-agent -listen :7443 -kubeconfig ~/.kube/config -context <ctx>

# Optional auth token (required for all RPCs when set).
torque-agent -listen :7443 -token "$TORQUE_REMOTE_TOKEN" -kubeconfig ~/.kube/config -context <ctx>

# Optional MirrorService flight recorder (durable sessions + ListSessions/Export).
torque-agent -listen :7443 -mirror-store ~/.torque/agent/mirror.sqlite -kubeconfig ~/.kube/config -context <ctx>

# Optional retention knobs for the flight recorder.
torque-agent -listen :7443 -mirror-store ~/.torque/agent/mirror.sqlite \
  -mirror-max-sessions 200 -mirror-max-frames 5000 -mirror-max-bytes 1000000000

# Optional HTTP gateway for browser UIs (same auth token as gRPC).
torque-agent -listen :7443 -http-listen :8081 -mirror-store ~/.torque/agent/mirror.sqlite

# Optional TLS (and mTLS).
torque-agent -listen :7443 -tls-cert ./server.crt -tls-key ./server.key
torque-agent -listen :7443 -tls-cert ./server.crt -tls-key ./server.key -tls-client-ca ./client-ca.crt
```

## Introspection (reflection)

The agent enables gRPC reflection so dynamic clients can discover the API at runtime.

If you have `grpcurl` installed:

```bash
grpcurl -plaintext -H "authorization: Bearer $TORQUE_REMOTE_TOKEN" 127.0.0.1:7443 list
grpcurl -plaintext -H "authorization: Bearer $TORQUE_REMOTE_TOKEN" 127.0.0.1:7443 list torque.api.v1
```

If the agent is running with TLS, omit `-plaintext` and pass a CA bundle instead:

```bash
grpcurl -cacert ./ca.crt -H "authorization: Bearer $TORQUE_REMOTE_TOKEN" 127.0.0.1:7443 list
```

If the agent requires mTLS (`-tls-client-ca`), also pass a client cert/key:

```bash
grpcurl -cacert ./ca.crt -cert ./client.crt -key ./client.key \
  -H "authorization: Bearer $TORQUE_REMOTE_TOKEN" 127.0.0.1:7443 list
```

## Health checks

```bash
grpcurl -plaintext -H "authorization: Bearer $TORQUE_REMOTE_TOKEN" \
  127.0.0.1:7443 grpc.health.v1.Health/Check
```

## Agent info

```bash
grpcurl -plaintext -H "authorization: Bearer $TORQUE_REMOTE_TOKEN" \
  127.0.0.1:7443 torque.api.v1.AgentInfoService/GetInfo
```

## Mirror Flight Recorder (sessions)

When `-mirror-store` is set, `torque-agent` persists `MirrorService` frames to SQLite and exposes session metadata:

```bash
grpcurl -plaintext -H "authorization: Bearer $TORQUE_REMOTE_TOKEN" \
  127.0.0.1:7443 torque.api.v1.MirrorService/ListSessions
```

List sessions also supports query filters (meta/tags/state/last-seen window), for example:

```bash
grpcurl -plaintext -H "authorization: Bearer $TORQUE_REMOTE_TOKEN" \
  -d '{"limit":200,"meta":{"namespace":"prod","release":"checkout"},"state":"MIRROR_SESSION_STATE_RUNNING"}' \
  127.0.0.1:7443 torque.api.v1.MirrorService/ListSessions
```

Get a single session (metadata + latest cursor):

```bash
grpcurl -plaintext -H "authorization: Bearer $TORQUE_REMOTE_TOKEN" \
  -d '{"session_id":"<session-id>"}' \
  127.0.0.1:7443 torque.api.v1.MirrorService/GetSession
```

Set session metadata/tags (useful for IDEs/UIs):

```bash
grpcurl -plaintext -H "authorization: Bearer $TORQUE_REMOTE_TOKEN" \
  -d '{"session_id":"<session-id>","meta":{"command":"torque logs","args":["checkout-.*","--namespace","prod"],"requester":"me@host"},"tags":{"team":"infra"}}' \
  127.0.0.1:7443 torque.api.v1.MirrorService/SetSessionMeta
```

Set session lifecycle status (optional; `torque-agent` also sets this automatically for built-in streaming RPCs when `session_id` is provided):

```bash
grpcurl -plaintext -H "authorization: Bearer $TORQUE_REMOTE_TOKEN" \
  -d '{"session_id":"<session-id>","status":{"state":"MIRROR_SESSION_STATE_DONE","exit_code":0,"completed_unix_nano":123}}' \
  127.0.0.1:7443 torque.api.v1.MirrorService/SetSessionStatus
```

Delete a session:

```bash
grpcurl -plaintext -H "authorization: Bearer $TORQUE_REMOTE_TOKEN" \
  -d '{"session_id":"<session-id>"}' \
  127.0.0.1:7443 torque.api.v1.MirrorService/DeleteSession
```

You can export a session as JSONL (one `MirrorFrame` per line, with `sequence` and `received_unix_nano` set):

```bash
grpcurl -plaintext -H "authorization: Bearer $TORQUE_REMOTE_TOKEN" \
  -d '{"session_id":"<session-id>","format":"jsonl"}' \
  127.0.0.1:7443 torque.api.v1.MirrorService/Export
```

## HTTP Gateway (Browser UIs)

When `-http-listen` is set, `torque-agent` exposes a tiny HTTP API that mirrors the MirrorService session surface:

- `POST /api/v1/auth/cookie` (sets an HttpOnly `torque_token` cookie; useful for native browser `EventSource`)
- `DELETE /api/v1/auth/cookie` (clears the cookie)
- `GET /api/v1/mirror/sessions?limit=200`
- `GET /api/v1/mirror/sessions?limit=200&namespace=prod&release=checkout&state=running`
- `GET /api/v1/mirror/sessions/<session-id>`
- `GET /api/v1/mirror/sessions/<session-id>/export?from_sequence=1` (JSONL)
- `GET /api/v1/mirror/sessions/<session-id>/tail?from_sequence=1&replay=1` (SSE: `event: frame` per `MirrorFrame`)
  - Resume: send `Last-Event-ID: <sequence>` (or `?last_event_id=<sequence>`)
  - Tuning: `?heartbeat=15s` (or `heartbeat_ms=15000`), `?retry_ms=1000`
  - Backpressure: if frames cannot be replayed (retention, slow consumer, etc.), the stream emits `event: dropped` with a JSON payload describing the missing sequence range.

Authentication uses the same headers as gRPC (`authorization: Bearer ...` or `x-torque-token: ...`), or the `torque_token` cookie set by `POST /api/v1/auth/cookie`.

## Session IDs

For agent/IDE integrations, treat `session_id` as the cross-RPC correlation key:

- Send `session_id` on `BuildService.RunBuild`, `LogService.StreamLogs`, `DeployService.Apply`/`Destroy`, and `VerifyService.Verify` to have the agent mirror those streams into `MirrorService` (so multiple subscribers can replay/tail the same session).
- `MirrorService.Publish` also records inbound frames with the same `session_id` and a server-assigned `sequence`.

## Client auth header

When `torque-agent -token ...` is set, clients must send one of:

- `authorization: Bearer <token>`
- `x-torque-token: <token>`

## torque Client TLS Flags

When the agent runs with TLS (`-tls-cert/-tls-key`), `torque` can be pointed at it with:

```bash
torque --remote-agent <host:port> --remote-tls --remote-tls-ca ./ca.crt --remote-token "$TORQUE_REMOTE_TOKEN" logs ...
torque --remote-agent <host:port> --remote-tls --remote-tls-insecure-skip-verify logs ...
torque --remote-agent <host:port> --remote-tls --remote-tls-server-name <name> logs ...
torque --remote-agent <host:port> --remote-tls --remote-tls-ca ./ca.crt \
  --remote-tls-client-cert ./client.crt --remote-tls-client-key ./client.key \
  --remote-token "$TORQUE_REMOTE_TOKEN" logs ...
```

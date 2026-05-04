Output ktl-showcase.gif

Set Shell "bash"
Set FontSize 18
Set Width 1200
Set Height 620
Set Theme "Dracula"
Set WindowBar Colorful
Set Padding 18
Set TypingSpeed 14ms
Set PlaybackSpeed 1.12

Hide
Type@0ms "clear"
Enter
Type@0ms "ktl() {"
Enter
Type@0ms '  case "$1:$2" in'
Enter
Type@0ms "    build:*) cat <<'OUT'"
Enter
Type@0ms "Serving ktl websocket build stream on :9085"
Enter
Type@0ms "BuildKit: cached 8/10 steps"
Enter
Type@0ms "Built ghcr.io/acme/api@sha256:12ab...9f31"
Enter
Type@0ms "Capture: build.sqlite"
Enter
Type@0ms "OUT"
Enter
Type@0ms "      ;;"
Enter
Type@0ms "    apply:plan) cat <<'OUT'"
Enter
Type@0ms "Planning release api [done]"
Enter
Type@0ms "Plan written to plan.html"
Enter
Type@0ms "Review: +3 new, ~1 changed, -0 deleted"
Enter
Type@0ms "Evidence: verify.json + build.sqlite attached to the PR summary"
Enter
Type@0ms "OUT"
Enter
Type@0ms "      ;;"
Enter
Type@0ms "    apply:*) cat <<'OUT'"
Enter
Type@0ms "Applying release api in namespace prod"
Enter
Type@0ms "Ready: deployment/api 3/3, service/api, hpa/api"
Enter
Type@0ms "Capture: apply.sqlite"
Enter
Type@0ms "OUT"
Enter
Type@0ms "      ;;"
Enter
Type@0ms "    stack:plan) cat <<'OUT'"
Enter
Type@0ms "STACK   prod"
Enter
Type@0ms "GROUP  WAVE  RELEASE     NEEDS"
Enter
Type@0ms "0      0     namespace   []"
Enter
Type@0ms "0      1     api         [namespace config]"
Enter
Type@0ms "0      1     worker      [namespace config]"
Enter
Type@0ms "1      2     frontend    [api worker]"
Enter
Type@0ms "OUT"
Enter
Type@0ms "      ;;"
Enter
Type@0ms "    logs:*) cat <<'OUT'"
Enter
Type@0ms "pod/api-7f9c7d9b55-2m8tf api listening on :8080"
Enter
Type@0ms "pod/api-7f9c7d9b55-2m8tf migrated 4 tables"
Enter
Type@0ms "pod/api-7f9c7d9b55-k6zq1 health check passed"
Enter
Type@0ms "Capture: logs.sqlite"
Enter
Type@0ms "OUT"
Enter
Type@0ms "      ;;"
Enter
Type@0ms "  esac"
Enter
Type@0ms "}"
Enter
Type@0ms "clear"
Enter
Show

Type "# ktl fast path: build, plan, apply, evidence, and logs"
Enter
Sleep 400ms
Type "ktl build . --tag ghcr.io/acme/api:9f31 --ws-listen :9085 --capture build.sqlite"
Enter
Sleep 3s

Ctrl+L
Sleep 300ms
Type "# PR review: render the deploy plan with build and verify evidence attached"
Enter
Type "ktl apply plan --visualize --chart ./chart --release api -n prod --output plan.html"
Enter
Sleep 3s

Ctrl+L
Sleep 300ms
Type "# Apply with a live rollout capture"
Enter
Type "ktl apply --chart ./chart --release api -n prod --capture apply.sqlite --yes"
Enter
Sleep 3s

Ctrl+L
Sleep 300ms
Type "# Many releases: compile a dependency graph before touching the cluster"
Enter
Type "ktl stack plan --config ./stacks/prod"
Enter
Sleep 3s

Ctrl+L
Sleep 300ms
Type "# After rollout: capture matching logs for review"
Enter
Type "ktl logs 'api-.*' -n prod --capture logs.sqlite --tail 100"
Enter
Sleep 4s

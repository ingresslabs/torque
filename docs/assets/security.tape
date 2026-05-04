Output ktl-security.gif

Set Shell "bash"
Set FontSize 18
Set Width 1200
Set Height 620
Set Theme "Dracula"
Set WindowBar Colorful
Set Padding 18
Set TypingSpeed 14ms
Set PlaybackSpeed 0.95

Hide
Type@0ms "clear"
Enter
Type@0ms "verifier() {"
Enter
Type@0ms "  cat <<'OUT'"
Enter
Type@0ms "Verifier policy report: verify.json"
Enter
Type@0ms "Rules: 42 evaluated, 0 critical, 0 high, 2 medium"
Enter
Type@0ms "Gate: pass for failOn=high"
Enter
Type@0ms "OUT"
Enter
Type@0ms "}"
Enter
Type@0ms "ktl() {"
Enter
Type@0ms '  case "$1:$2" in'
Enter
Type@0ms "    build:*) cat <<'OUT'"
Enter
Type@0ms "[sandbox] profile=sandbox/linux-strict.cfg namespaces=user,pid,cgroup"
Enter
Type@0ms "[sandbox] writable paths limited to /workspace and /tmp"
Enter
Type@0ms "SBOM: attest/api.spdx.json"
Enter
Type@0ms "Provenance: attest/api.intoto.jsonl"
Enter
Type@0ms "Built ghcr.io/acme/api:secure"
Enter
Type@0ms "OUT"
Enter
Type@0ms "      ;;"
Enter
Type@0ms "    secrets:discover) cat <<'OUT'"
Enter
Type@0ms "Secret references discovered"
Enter
Type@0ms "  values/prod.yaml db.password -> secret://vault/prod/api#db_password"
Enter
Type@0ms "  chart/templates/secret.yaml api.key -> secret://vault/prod/api#api_key"
Enter
Type@0ms "Plaintext findings: 0"
Enter
Type@0ms "OUT"
Enter
Type@0ms "      ;;"
Enter
Type@0ms "    apply:plan) cat <<'OUT'"
Enter
Type@0ms "Security evidence attached to plan.md"
Enter
Type@0ms "Verifier: verify.json [gate pass]"
Enter
Type@0ms "Build: SBOM + provenance linked for image ghcr.io/acme/api:secure"
Enter
Type@0ms "OUT"
Enter
Type@0ms "      ;;"
Enter
Type@0ms "    apply:*) cat <<'OUT'"
Enter
Type@0ms "Verified deploy gate: require-verified verify.json [pass]"
Enter
Type@0ms "Applying release api in namespace prod"
Enter
Type@0ms "Capture: secure-apply.sqlite"
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

Type "# Security gate: verify chart and policy before deploy"
Enter
Sleep 400ms
Type "verifier --chart ./chart --release api -n prod --format json --report verify.json"
Enter
Sleep 3s

Ctrl+L
Sleep 300ms
Type "# Strict sandbox build with SBOM and provenance"
Enter
Type "KTL_SANDBOX_CONFIG=sandbox/linux-strict.cfg ktl build . --tag ghcr.io/acme/api:secure --sbom --provenance --attest-dir attest --sandbox-logs"
Enter
Sleep 3s

Ctrl+L
Sleep 300ms
Type "# Secret references stay external to manifests and values"
Enter
Type "ktl secrets discover --scope chart --chart ./chart --values values/prod.yaml"
Enter
Sleep 3s

Ctrl+L
Sleep 300ms
Type "# PR plan carries verifier, SBOM, and provenance evidence"
Enter
Type "ktl apply plan --chart ./chart --release api -n prod --verify-report verify.json --github-comment --output plan.md"
Enter
Sleep 3s

Ctrl+L
Sleep 300ms
Type "# Apply only after the verifier gate passes"
Enter
Type "ktl apply --chart ./chart --release api -n prod --require-verified verify.json --capture secure-apply.sqlite --yes"
Enter
Sleep 4s

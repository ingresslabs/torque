# Sandbox demo fixtures

These fixtures are used by `scripts/sandbox-demo.sh` to demonstrate the difference between:

- running `torque build` *without* the `torque` sandbox (`TORQUE_SANDBOX_DISABLE=1`), and
- running `torque build` *with* the `torque` sandbox (default when a sandbox runtime is present on Linux).

They are intentionally non-destructive and do not read sensitive host paths.


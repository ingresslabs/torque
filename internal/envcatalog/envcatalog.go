package envcatalog

type VarInfo struct {
	Category    string
	Name        string
	Description string
	Dynamic     bool
	Internal    bool
}

func Catalog() []VarInfo {
	return []VarInfo{
		{
			Category:    "Config",
			Name:        "TORQUE_CONFIG",
			Description: "Path to the torque config file.",
		},
		{
			Category:    "Config",
			Name:        "TORQUE_<FLAG>",
			Dynamic:     true,
			Description: "Set any torque CLI flag via environment (hyphens become underscores). Example: TORQUE_NAMESPACE=default.",
		},
		{
			Category:    "Secrets",
			Name:        "TORQUE_SECRET_PROVIDER",
			Description: "Default secret provider name for resolving secret:// references in torque apply/plan.",
		},
		{
			Category:    "Secrets",
			Name:        "TORQUE_SECRET_CONFIG",
			Description: "Path to a secrets provider config file for resolving secret:// references.",
		},
		{
			Category:    "Output",
			Name:        "NO_COLOR",
			Description: "Disable ANSI color output (any non-empty value).",
		},
		{
			Category:    "CLI",
			Name:        "TORQUE_YES",
			Description: "Auto-approve confirmations (equivalent to passing --yes).",
		},
		{
			Category:    "Logging",
			Name:        "TORQUE_KUBE_LOG_LEVEL",
			Description: "Kubernetes client-go verbosity (klog -v). At >=6 enables HTTP request/response tracing.",
		},
		{
			Category:    "Kubernetes",
			Name:        "KUBERNETES_MASTER",
			Description: "Address of the Kubernetes API server (overrides kubeconfig).",
		},
		{
			Category:    "Kubernetes",
			Name:        "KUBECONFIG",
			Description: "Path to the kubeconfig file (defaults to ~/.kube/config).",
		},
		{
			Category:    "Profiling",
			Name:        "TORQUE_PROFILE",
			Description: "Enable profiling modes for torque itself (e.g. startup writes CPU/heap profiles to the working directory).",
		},
		{
			Category:    "Build",
			Name:        "TORQUE_BUILDKIT_HOST",
			Description: "Override the BuildKit address used by `torque build`.",
		},
		{
			Category:    "Build",
			Name:        "TORQUE_BUILDKIT_CACHE",
			Description: "Configure BuildKit cache import/export for `torque build`.",
		},
		{
			Category:    "Build",
			Name:        "TORQUE_DOCKER_CONTEXT",
			Description: "Docker context to use for Buildx fallback (when provisioning a Docker-backed BuildKit builder).",
		},
		{
			Category:    "Build",
			Name:        "TORQUE_DOCKER_CONFIG",
			Description: "Override Docker config directory for Buildx fallback (equivalent to DOCKER_CONFIG).",
		},
		{
			Category:    "Registry",
			Name:        "TORQUE_AUTHFILE",
			Description: "Path to a container registry auth file for `torque build` (containers-auth.json).",
		},
		{
			Category:    "Registry",
			Name:        "TORQUE_REGISTRY_AUTH_FILE",
			Description: "Alternate registry auth file path for `torque build`.",
		},
		{
			Category:    "Sandbox",
			Name:        "TORQUE_SANDBOX_DISABLE",
			Description: "Disable sandbox execution where supported (set to 1).",
		},
		{
			Category:    "Sandbox",
			Name:        "TORQUE_SANDBOX_CONFIG",
			Description: "Path to the sandbox policy configuration file.",
		},
		{
			Category:    "Sandbox",
			Name:        "TORQUE_SANDBOX_ACTIVE",
			Internal:    true,
			Description: "Internal marker set inside the sandbox runtime.",
		},
		{
			Category:    "Sandbox",
			Name:        "TORQUE_SANDBOX_LOG_PATH",
			Internal:    true,
			Description: "Internal path used by the sandbox to mirror diagnostics/logs.",
		},
		{
			Category:    "Sandbox",
			Name:        "TORQUE_SANDBOX_CONTEXT",
			Internal:    true,
			Description: "Internal sandbox context marker.",
		},
		{
			Category:    "Sandbox",
			Name:        "TORQUE_SANDBOX_CACHE",
			Internal:    true,
			Description: "Internal sandbox cache marker.",
		},
		{
			Category:    "Sandbox",
			Name:        "TORQUE_SANDBOX_BUILDER",
			Internal:    true,
			Description: "Internal sandbox builder marker.",
		},
		{
			Category:    "Sandbox (Legacy)",
			Name:        "TORQUE_NSJAIL_DISABLE",
			Internal:    true,
			Description: "Legacy alias for TORQUE_SANDBOX_DISABLE.",
		},
		{
			Category:    "Sandbox (Legacy)",
			Name:        "TORQUE_NSJAIL_ACTIVE",
			Internal:    true,
			Description: "Legacy alias for TORQUE_SANDBOX_ACTIVE.",
		},
		{
			Category:    "Sandbox (Legacy)",
			Name:        "TORQUE_NSJAIL_LOG_PATH",
			Internal:    true,
			Description: "Legacy alias for TORQUE_SANDBOX_LOG_PATH.",
		},
		{
			Category:    "Sandbox (Legacy)",
			Name:        "TORQUE_NSJAIL_CONTEXT",
			Internal:    true,
			Description: "Legacy alias for TORQUE_SANDBOX_CONTEXT.",
		},
		{
			Category:    "Sandbox (Legacy)",
			Name:        "TORQUE_NSJAIL_CACHE",
			Internal:    true,
			Description: "Legacy alias for TORQUE_SANDBOX_CACHE.",
		},
		{
			Category:    "Sandbox (Legacy)",
			Name:        "TORQUE_NSJAIL_BUILDER",
			Internal:    true,
			Description: "Legacy alias for TORQUE_SANDBOX_BUILDER.",
		},
		{
			Category:    "Capture",
			Name:        "TORQUE_CAPTURE_QUEUE_SIZE",
			Description: "Capture recorder in-memory queue size.",
		},
		{
			Category:    "Capture",
			Name:        "TORQUE_CAPTURE_BATCH_SIZE",
			Description: "Capture recorder flush batch size.",
		},
		{
			Category:    "Capture",
			Name:        "TORQUE_CAPTURE_FLUSH_MS",
			Description: "Capture recorder flush interval in milliseconds.",
		},
		{
			Category:    "Stack",
			Name:        "TORQUE_STACK_ROOT",
			Description: "Default stack root for `torque stack ...` when --root is not provided.",
		},
		{
			Category:    "Stack",
			Name:        "TORQUE_STACK_PROFILE",
			Description: "Default stack profile overlay for `torque stack ...` when --profile is not provided.",
		},
		{
			Category:    "Stack",
			Name:        "TORQUE_STACK_OUTPUT",
			Description: "Default output format for `torque stack` commands when --output is not provided (table|json).",
		},
		{
			Category:    "Stack",
			Name:        "TORQUE_STACK_CLUSTER",
			Description: "Default cluster filter for `torque stack` selection (comma-separated).",
		},
		{
			Category:    "Stack",
			Name:        "TORQUE_STACK_TAG",
			Description: "Default tag selector for `torque stack` selection (comma-separated).",
		},
		{
			Category:    "Stack",
			Name:        "TORQUE_STACK_FROM_PATH",
			Description: "Default from-path selector for `torque stack` selection (comma-separated).",
		},
		{
			Category:    "Stack",
			Name:        "TORQUE_STACK_RELEASE",
			Description: "Default release selector for `torque stack` selection (comma-separated).",
		},
		{
			Category:    "Stack",
			Name:        "TORQUE_STACK_GIT_RANGE",
			Description: "Default git diff range selector for `torque stack` selection (example: origin/main...HEAD).",
		},
		{
			Category:    "Stack",
			Name:        "TORQUE_STACK_GIT_INCLUDE_DEPS",
			Description: "When using TORQUE_STACK_GIT_RANGE, include dependencies (set to 1/true).",
		},
		{
			Category:    "Stack",
			Name:        "TORQUE_STACK_GIT_INCLUDE_DEPENDENTS",
			Description: "When using TORQUE_STACK_GIT_RANGE, include dependents (set to 1/true).",
		},
		{
			Category:    "Stack",
			Name:        "TORQUE_STACK_INCLUDE_DEPS",
			Description: "Include dependencies in selection expansion (set to 1/true).",
		},
		{
			Category:    "Stack",
			Name:        "TORQUE_STACK_INCLUDE_DEPENDENTS",
			Description: "Include dependents in selection expansion (set to 1/true).",
		},
		{
			Category:    "Stack",
			Name:        "TORQUE_STACK_ALLOW_MISSING_DEPS",
			Description: "Allow missing dependencies when selecting a subset (set to 1/true).",
		},
		{
			Category:    "Stack",
			Name:        "TORQUE_STACK_INFER_DEPS",
			Description: "Enable inferred dependencies when not explicitly set via flags (set to 1/true).",
		},
		{
			Category:    "Stack",
			Name:        "TORQUE_STACK_INFER_CONFIG_REFS",
			Description: "Enable inferred ConfigMap/Secret reference edges when inferring deps (set to 1/true).",
		},
		{
			Category:    "Stack",
			Name:        "TORQUE_STACK_APPLY_DRY_RUN",
			Description: "Default `torque stack apply --dry-run` value when the flag is not provided (set to 1/true).",
		},
		{
			Category:    "Stack",
			Name:        "TORQUE_STACK_APPLY_DIFF",
			Description: "Default `torque stack apply --diff` value when the flag is not provided (set to 1/true).",
		},
		{
			Category:    "Stack",
			Name:        "TORQUE_STACK_DELETE_CONFIRM_THRESHOLD",
			Description: "Default delete confirmation threshold for `torque stack delete` when the flag is not provided.",
		},
		{
			Category:    "Stack",
			Name:        "TORQUE_STACK_RESUME_ALLOW_DRIFT",
			Description: "Default `torque stack --allow-drift` value when the flag is not provided (set to 1/true).",
		},
		{
			Category:    "Stack",
			Name:        "TORQUE_STACK_RESUME_RERUN_FAILED",
			Description: "Default `torque stack --rerun-failed` value when the flag is not provided (set to 1/true).",
		},
	}
}

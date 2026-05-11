//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ingresslabs/torque/internal/verify"
)

const (
	securityE2EKubeconfigEnv = "TORQUE_SECURITY_E2E_KUBECONFIG"
	securityE2EConfirmEnv    = "TORQUE_SECURITY_E2E_CONFIRM"
	securityE2EContextEnv    = "TORQUE_SECURITY_E2E_CONTEXT"
	securityE2ENamespaceEnv  = "TORQUE_SECURITY_E2E_NAMESPACE"
)

func TestVerifierSecurityProfile_LiveNamespaceSecretsBoundaries(t *testing.T) {
	chdirSecurityE2ERepoRoot(t)
	kubeconfig := resolveSecurityE2EKubeconfig(t)
	if strings.TrimSpace(os.Getenv(securityE2EConfirmEnv)) != "1" {
		t.Skipf("%s=1 not set", securityE2EConfirmEnv)
	}
	ctxName := strings.TrimSpace(os.Getenv(securityE2EContextEnv))
	namespace := strings.TrimSpace(os.Getenv(securityE2ENamespaceEnv))
	providedNamespace := namespace != ""
	if namespace == "" {
		namespace = fmt.Sprintf("torque-security-e2e-%d", time.Now().UTC().UnixNano())
	}
	rawSecret := strings.Join([]string{"AKIA", "1234567890", "ABCDEF"}, "")
	tmp := t.TempDir()
	secretsReport := filepath.Join(tmp, "secrets.json")
	verifyReport := filepath.Join(tmp, "verify.json")
	evidenceDir := filepath.Join(tmp, "evidence")

	if providedNamespace {
		kubectl(t, kubeconfig, ctxName, "get", "namespace", namespace)
	} else {
		applySecurityE2ENamespace(t, kubeconfig, ctxName, namespace)
		t.Cleanup(func() {
			cleanupSecurityE2E(t, kubeconfig, ctxName, "delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true", "--timeout=60s")
		})
	}
	t.Cleanup(func() {
		cleanupSecurityE2EResources(t, kubeconfig, ctxName, namespace)
	})
	cleanupSecurityE2EResources(t, kubeconfig, ctxName, namespace)
	applyLiveSecurityFixture(t, kubeconfig, ctxName, namespace, rawSecret)

	cmd := newRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	args := []string{
		"--kubeconfig", kubeconfig,
		"--namespace", namespace,
		"--security-profile", "enterprise",
		"--security-boundary-matrix",
		"--secret-flow-graph",
		"--secrets-report", secretsReport,
		"--security-evidence", evidenceDir,
		"--format", "json",
		"--report", verifyReport,
	}
	if ctxName != "" {
		args = append([]string{"--context", ctxName}, args...)
	}
	cmd.SetArgs(args)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "verify blocked") {
		t.Fatalf("expected live namespace security scan to block, err=%v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}

	report := readSecurityE2EVerifyReport(t, verifyReport)
	secretScan := readSecurityE2ESecretsReport(t, secretsReport)
	assertSecurityE2EFindings(t, report.Findings, secretScan.Findings)
	assertSecurityE2EBoundaryMatrix(t, secretScan.BoundaryMatrix)
	assertSecurityE2EFlowGraph(t, secretScan.FlowGraph)
	for _, path := range []string{
		verifyReport,
		secretsReport,
		filepath.Join(evidenceDir, "manifest.json"),
		filepath.Join(evidenceDir, "boundary.matrix.json"),
		filepath.Join(evidenceDir, "secret.flow.graph.json"),
		filepath.Join(evidenceDir, "secrets.report.json"),
		filepath.Join(evidenceDir, "verifier.report.json"),
		filepath.Join(evidenceDir, "redaction.proof.json"),
		filepath.Join(evidenceDir, "reports", "security.md"),
	} {
		assertSecurityE2ENoRawSecret(t, path, rawSecret)
	}
}

func chdirSecurityE2ERepoRoot(t *testing.T) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(cwd, "..", ".."))
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("chdir repo root: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
}

func resolveSecurityE2EKubeconfig(t *testing.T) string {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(securityE2EKubeconfigEnv))
	if raw == "" {
		t.Skipf("%s not set", securityE2EKubeconfigEnv)
	}
	if strings.HasPrefix(raw, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("resolve home: %v", err)
		}
		raw = filepath.Join(home, strings.TrimPrefix(raw, "~/"))
	}
	path := filepath.Clean(raw)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat kubeconfig %s: %v", path, err)
	}
	return path
}

func applyLiveSecurityFixture(t *testing.T, kubeconfig, ctxName, namespace, rawSecret string) {
	t.Helper()
	manifest := fmt.Sprintf(`
apiVersion: v1
kind: Secret
metadata:
  name: allowed-secret
  namespace: %[1]s
stringData:
  apiKey: %[2]s
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: blocked-config
  namespace: %[1]s
data:
  apiKey: %[2]s
---
apiVersion: v1
kind: Pod
metadata:
  name: env-leak
  namespace: %[1]s
  labels:
    torque.dev/leak: %[2]s
  annotations:
    torque.dev/leak: %[2]s
spec:
  restartPolicy: Never
  containers:
    - name: env-leak
      image: registry.k8s.io/pause:3.9
      command: ["/pause", "%[2]s"]
      args: ["%[2]s"]
      env:
        - name: API_KEY
          value: %[2]s
      livenessProbe:
        exec:
          command: ["/pause", "%[2]s"]
---
apiVersion: v1
kind: Pod
metadata:
  name: ref-ok
  namespace: %[1]s
spec:
  restartPolicy: Never
  containers:
    - name: ref-ok
      image: registry.k8s.io/pause:3.9
      env:
        - name: API_KEY
          valueFrom:
            secretKeyRef:
              name: allowed-secret
              key: apiKey
      volumeMounts:
        - name: allowed-secret
          mountPath: /var/run/allowed-secret
          readOnly: true
  volumes:
    - name: allowed-secret
      secret:
        secretName: allowed-secret
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: deploy-leak
  namespace: %[1]s
spec:
  replicas: 1
  selector:
    matchLabels:
      app: deploy-leak
  template:
    metadata:
      labels:
        app: deploy-leak
    spec:
      containers:
        - name: deploy-leak
          image: registry.k8s.io/pause:3.9
          env:
            - name: API_KEY
              value: %[2]s
---
apiVersion: batch/v1
kind: Job
metadata:
  name: job-leak
  namespace: %[1]s
spec:
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: job-leak
          image: registry.k8s.io/pause:3.9
          env:
            - name: API_KEY
              value: %[2]s
---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: cron-leak
  namespace: %[1]s
spec:
  schedule: "*/5 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: Never
          containers:
            - name: cron-leak
              image: registry.k8s.io/pause:3.9
              env:
                - name: API_KEY
                  value: %[2]s
`, namespace, rawSecret)
	cmd := exec.Command("kubectl", kubectlArgs(kubeconfig, ctxName, "create", "-f", "-")...)
	cmd.Stdin = strings.NewReader(manifest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kubectl create security fixture: %v\n%s", err, out)
	}
}

func applySecurityE2ENamespace(t *testing.T, kubeconfig, ctxName, namespace string) {
	t.Helper()
	manifest := fmt.Sprintf(`
apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    torque.dev/e2e: security
`, namespace)
	cmd := exec.Command("kubectl", kubectlArgs(kubeconfig, ctxName, "apply", "-f", "-")...)
	cmd.Stdin = strings.NewReader(manifest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kubectl apply namespace: %v\n%s", err, out)
	}
}

func kubectl(t *testing.T, kubeconfig, ctxName string, args ...string) {
	t.Helper()
	cmd := exec.Command("kubectl", kubectlArgs(kubeconfig, ctxName, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kubectl %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func cleanupSecurityE2E(t *testing.T, kubeconfig, ctxName string, args ...string) {
	t.Helper()
	cmd := exec.Command("kubectl", kubectlArgs(kubeconfig, ctxName, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("kubectl cleanup %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func cleanupSecurityE2EResources(t *testing.T, kubeconfig, ctxName, namespace string) {
	t.Helper()
	cleanupSecurityE2E(t, kubeconfig, ctxName, "-n", namespace, "delete",
		"secret/allowed-secret",
		"configmap/blocked-config",
		"pod/env-leak",
		"pod/ref-ok",
		"deployment/deploy-leak",
		"job/job-leak",
		"cronjob/cron-leak",
		"--ignore-not-found=true", "--wait=true", "--timeout=60s")
}

func kubectlArgs(kubeconfig, ctxName string, args ...string) []string {
	out := []string{"--kubeconfig", kubeconfig}
	if strings.TrimSpace(ctxName) != "" {
		out = append(out, "--context", strings.TrimSpace(ctxName))
	}
	return append(out, args...)
}

func readSecurityE2EVerifyReport(t *testing.T, path string) *verify.Report {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read verifier report: %v", err)
	}
	var report verify.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("decode verifier report: %v\n%s", err, raw)
	}
	return &report
}

func readSecurityE2ESecretsReport(t *testing.T, path string) *verify.SecretScanReport {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read secrets report: %v", err)
	}
	var report verify.SecretScanReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("decode secrets report: %v\n%s", err, raw)
	}
	return &report
}

func assertSecurityE2EFindings(t *testing.T, verifierFindings, secretFindings []verify.Finding) {
	t.Helper()
	all := append(append([]verify.Finding{}, verifierFindings...), secretFindings...)
	var configMapLeak, envLeak, secretLeak bool
	for _, finding := range all {
		if finding.RuleID != "secret/value_aws_access_key" {
			continue
		}
		switch finding.Subject.Kind {
		case "ConfigMap":
			if finding.Subject.Name == "blocked-config" {
				configMapLeak = true
			}
		case "Pod":
			if finding.Subject.Name == "env-leak" && strings.Contains(finding.FieldPath, "env") {
				envLeak = true
			}
		case "Secret":
			secretLeak = true
		}
	}
	if !configMapLeak {
		t.Fatalf("missing ConfigMap secret-flow finding: %#v", all)
	}
	if !envLeak {
		t.Fatalf("missing env secret-flow finding: %#v", all)
	}
	if secretLeak {
		t.Fatalf("Secret object should be an allowed materialization, got finding: %#v", all)
	}
}

func assertSecurityE2EBoundaryMatrix(t *testing.T, matrix *verify.SecurityBoundaryMatrix) {
	t.Helper()
	if matrix == nil {
		t.Fatalf("missing security boundary matrix")
	}
	if !matrix.Passed {
		t.Fatalf("security boundary matrix should pass allowed/blocked expectations: %#v", matrix)
	}
	assertSecurityE2EMatrixRow(t, matrix, "Secret.data", "allowed", "allowed", 0)
	assertSecurityE2EMatrixRow(t, matrix, "ConfigMap.data", "blocked", "blocked", 1)
	assertSecurityE2EMatrixRow(t, matrix, "metadata.annotations", "blocked", "blocked", 1)
	assertSecurityE2EMatrixRow(t, matrix, "metadata.labels", "blocked", "blocked", 1)
	assertSecurityE2EMatrixRow(t, matrix, "env.value", "blocked", "blocked", 4)
	assertSecurityE2EMatrixRow(t, matrix, "command", "blocked", "blocked", 1)
	assertSecurityE2EMatrixRow(t, matrix, "args", "blocked", "blocked", 1)
	assertSecurityE2EMatrixRow(t, matrix, "probes", "blocked", "blocked", 1)
	assertSecurityE2EMatrixRow(t, matrix, "secretKeyRef", "allowed", "allowed", 0)
	assertSecurityE2EMatrixRow(t, matrix, "secret volume", "allowed", "allowed", 0)
}

func assertSecurityE2EFlowGraph(t *testing.T, graph *verify.SecretFlowGraph) {
	t.Helper()
	if graph == nil {
		t.Fatalf("missing secret flow graph")
	}
	if graph.Summary.ForbiddenFlows < 1 {
		t.Fatalf("expected forbidden flows in graph: %#v", graph)
	}
	if graph.Summary.AllowedMaterializations < 1 {
		t.Fatalf("expected allowed materializations in graph: %#v", graph)
	}
	if graph.Summary.SecretReferences < 1 {
		t.Fatalf("expected secret references in graph: %#v", graph)
	}
	if graph.Summary.RawSecretStored {
		t.Fatalf("flow graph marked raw secret stored: %#v", graph)
	}
	if graph.Summary.LiveObjects < 1 {
		t.Fatalf("expected live object provenance in graph: %#v", graph)
	}
}

func assertSecurityE2EMatrixRow(t *testing.T, matrix *verify.SecurityBoundaryMatrix, surface, boundary, status string, minFindings int) {
	t.Helper()
	for _, row := range matrix.Rows {
		if row.Surface != surface {
			continue
		}
		if !row.Present {
			t.Fatalf("%s present=false row=%#v", surface, row)
		}
		if row.Boundary != boundary {
			t.Fatalf("%s boundary=%q, want %q row=%#v", surface, row.Boundary, boundary, row)
		}
		if row.Status != status {
			t.Fatalf("%s status=%q, want %q row=%#v", surface, row.Status, status, row)
		}
		if row.FindingCount < minFindings {
			t.Fatalf("%s findingCount=%d, want >=%d row=%#v", surface, row.FindingCount, minFindings, row)
		}
		return
	}
	t.Fatalf("missing matrix row %s in %#v", surface, matrix.Rows)
}

func assertSecurityE2ENoRawSecret(t *testing.T, path, rawSecret string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact %s: %v", path, err)
	}
	if strings.Contains(string(raw), rawSecret) {
		t.Fatalf("artifact %s leaked raw secret:\n%s", path, raw)
	}
}

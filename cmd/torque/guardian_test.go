package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestGuardianInstallManifestIsObserveOnly(t *testing.T) {
	manifest := renderGuardianInstallManifest("torque-system", "observe")
	for _, want := range []string{
		"kind: ClusterRole",
		"kind: ClusterRoleBinding",
		"mode: observe",
		"mutation: disabled",
		`verbs: ["get", "list", "watch"]`,
	} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("expected manifest to contain %q:\n%s", want, manifest)
		}
	}
	for _, forbidden := range []string{`"create"`, `"update"`, `"patch"`, `"delete"`} {
		if strings.Contains(manifest, forbidden) {
			t.Fatalf("observe manifest should not include write verb %s:\n%s", forbidden, manifest)
		}
	}
}

func TestGuardianDiffProofDetectsDriftAndRedacts(t *testing.T) {
	rawSecret := "AKIA1234567890ABCDEF"
	source := guardianSimulationSource{
		Dir:                    "/tmp/torque-sim-proof",
		Release:                "api",
		Namespace:              "prod",
		Chart:                  "./chart",
		RenderedManifestSHA256: "sha256:test",
		Resources: map[string]string{
			"prod|configmap|api-config": `apiVersion: v1
kind: ConfigMap
metadata:
  name: api-config
  namespace: prod
data:
  value: expected
`,
		},
	}
	now := metav1.NewTime(time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC))
	live := mustGuardianObject(t, `apiVersion: v1
kind: ConfigMap
metadata:
  name: api-config
  namespace: prod
data:
  value: changed
  password: AKIA1234567890ABCDEF
`)
	live.SetManagedFields([]metav1.ManagedFieldsEntry{{
		Manager:    "kubectl-edit",
		Operation:  metav1.ManagedFieldsOperationUpdate,
		APIVersion: "v1",
		Time:       &now,
	}})
	proof, err := buildGuardianDiffProof(context.Background(), source, func(_ context.Context, desired *unstructured.Unstructured, _ string) (*unstructured.Unstructured, error) {
		return live, nil
	}, []guardianEventRow{{
		Time:    now.UTC().Format(time.RFC3339Nano),
		Type:    "Warning",
		Reason:  "BackOff",
		Message: "token=" + rawSecret,
		Resource: guardianResourceRef{
			Kind:      "Pod",
			Namespace: "prod",
			Name:      "api-123",
		},
	}}, guardianDiffBuildOptions{Namespace: "prod", ClusterHost: "https://cluster", Since: "24h"})
	if err != nil {
		t.Fatalf("build diff proof: %v", err)
	}
	if !proof.Blocked || proof.Status != "drifted" {
		t.Fatalf("expected drifted proof, got blocked=%t status=%q", proof.Blocked, proof.Status)
	}
	if proof.Summary.Changed != 1 || proof.Summary.RuntimeBoundary != 1 || proof.Summary.WarningEvents != 1 {
		t.Fatalf("unexpected summary: %#v", proof.Summary)
	}
	if len(proof.ManagedFields.Owners) != 1 || !proof.ManagedFields.Owners[0].Suspicious {
		t.Fatalf("expected suspicious managed field owner: %#v", proof.ManagedFields.Owners)
	}
	raw, err := json.Marshal(proof)
	if err != nil {
		t.Fatalf("marshal proof: %v", err)
	}
	if strings.Contains(string(raw), rawSecret) {
		t.Fatalf("proof leaked raw secret: %s", raw)
	}
}

func TestGuardianDiffIgnoresHelmMetadataAndAPIDefaults(t *testing.T) {
	source := guardianSimulationSource{
		Release:   "api",
		Namespace: "prod",
		Resources: map[string]string{
			"prod|deployment|api": `apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: prod
spec:
  replicas: 2
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      serviceAccountName: api
      containers:
        - name: api
          image: ghcr.io/acme/api:v1
`,
			"prod|service|api": `apiVersion: v1
kind: Service
metadata:
  name: api
  namespace: prod
spec:
  selector:
    app: api
  ports:
    - name: http
      port: 80
      targetPort: 8080
`,
		},
	}
	liveDeployment := mustGuardianObject(t, `apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: prod
  annotations:
    deployment.kubernetes.io/revision: "1"
    meta.helm.sh/release-name: api
    meta.helm.sh/release-namespace: prod
  labels:
    app.kubernetes.io/managed-by: Helm
spec:
  progressDeadlineSeconds: 600
  replicas: 2
  revisionHistoryLimit: 10
  selector:
    matchLabels:
      app: api
  strategy:
    rollingUpdate:
      maxSurge: 25%
      maxUnavailable: 25%
    type: RollingUpdate
  template:
    metadata:
      labels:
        app: api
    spec:
      containers:
        - name: api
          image: ghcr.io/acme/api:v1
          imagePullPolicy: IfNotPresent
          resources: {}
          terminationMessagePath: /dev/termination-log
          terminationMessagePolicy: File
      dnsPolicy: ClusterFirst
      restartPolicy: Always
      schedulerName: default-scheduler
      securityContext: {}
      serviceAccount: api
      serviceAccountName: api
      terminationGracePeriodSeconds: 30
`)
	liveService := mustGuardianObject(t, `apiVersion: v1
kind: Service
metadata:
  name: api
  namespace: prod
spec:
  clusterIP: 10.43.1.2
  clusterIPs:
    - 10.43.1.2
  internalTrafficPolicy: Cluster
  ipFamilies:
    - IPv4
  ipFamilyPolicy: SingleStack
  ports:
    - name: http
      port: 80
      protocol: TCP
      targetPort: 8080
  selector:
    app: api
  sessionAffinity: None
  type: ClusterIP
`)
	proof, err := buildGuardianDiffProof(context.Background(), source, func(_ context.Context, desired *unstructured.Unstructured, _ string) (*unstructured.Unstructured, error) {
		if strings.EqualFold(desired.GetKind(), "Service") {
			return liveService, nil
		}
		return liveDeployment, nil
	}, nil, guardianDiffBuildOptions{Namespace: "prod"})
	if err != nil {
		t.Fatalf("build diff proof: %v", err)
	}
	if proof.Blocked || proof.Status != "passed" || proof.Summary.Changed != 0 {
		t.Fatalf("expected default-only live object to pass, got status=%s summary=%#v changes=%#v", proof.Status, proof.Summary, proof.PredictedVsLive.Changes)
	}
}

func TestGuardianComplexRuntimeBoundaryAndAftercare(t *testing.T) {
	rawSecret := "ghp_abcdefghijklmnopqrstuvwxyz123456"
	source := guardianSimulationSource{
		Release:   "api",
		Namespace: "prod",
		Resources: map[string]string{
			"prod|deployment|api": `apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: prod
spec:
  replicas: 2
  template:
    spec:
      containers:
        - name: api
          image: ghcr.io/acme/api:v1
          env:
            - name: API_TOKEN
              valueFrom:
                secretKeyRef:
                  name: api
                  key: token
`,
		},
	}
	now := metav1.NewTime(time.Date(2026, 5, 11, 13, 0, 0, 0, time.UTC))
	live := mustGuardianObject(t, `apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: prod
spec:
  replicas: 2
  template:
    spec:
      containers:
        - name: api
          image: ghcr.io/acme/api:v1
          env:
            - name: API_TOKEN
              value: ghp_abcdefghijklmnopqrstuvwxyz123456
status:
  replicas: 2
  availableReplicas: 1
  unavailableReplicas: 1
`)
	live.SetManagedFields([]metav1.ManagedFieldsEntry{{
		Manager:    "mutating-webhook",
		Operation:  metav1.ManagedFieldsOperationUpdate,
		APIVersion: "apps/v1",
		Time:       &now,
	}})
	proof, err := buildGuardianDiffProof(context.Background(), source, func(context.Context, *unstructured.Unstructured, string) (*unstructured.Unstructured, error) {
		return live, nil
	}, []guardianEventRow{{
		Time:     now.UTC().Format(time.RFC3339Nano),
		Type:     "Warning",
		Reason:   "Unhealthy",
		Message:  "readiness failed token: " + rawSecret,
		Resource: guardianResourceRef{Kind: "Pod", Namespace: "prod", Name: "api-123"},
	}}, guardianDiffBuildOptions{Namespace: "prod"})
	if err != nil {
		t.Fatalf("build diff proof: %v", err)
	}
	if !proof.Blocked || proof.Summary.Changed != 1 || proof.Summary.RuntimeBoundary != 1 || proof.Summary.WarningEvents != 1 || proof.Summary.AftercareIssues == 0 {
		t.Fatalf("unexpected complex proof: blocked=%t summary=%#v", proof.Blocked, proof.Summary)
	}
	if len(proof.ManagedFields.Owners) != 1 || !proof.ManagedFields.Owners[0].Suspicious {
		t.Fatalf("expected suspicious webhook owner: %#v", proof.ManagedFields.Owners)
	}
	raw, err := json.Marshal(proof)
	if err != nil {
		t.Fatalf("marshal proof: %v", err)
	}
	if strings.Contains(string(raw), rawSecret) {
		t.Fatalf("proof leaked raw secret: %s", raw)
	}
}

func TestGuardianPRArtifacts(t *testing.T) {
	dir := t.TempDir()
	proof := guardianDiffProof{
		Version:     "v1",
		Tool:        guardianTool,
		GeneratedAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
		Release:     "api",
		Namespace:   "prod",
		Status:      "drifted",
		Blocked:     true,
		Summary:     guardianDiffSummary{Changed: 1},
		PredictedVsLive: guardianPredictedVsLiveDiff{Version: "v1", Passed: false, Changes: []guardianDriftItem{{
			Resource: guardianResourceRef{Version: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "api-config"},
			Reason:   "changed",
		}}},
	}
	paths, err := writeGuardianPRArtifacts(proof, guardianPROptions{From: filepath.Join(dir, "drift-proof.json"), Branch: "fix/runtime-drift"})
	if err != nil {
		t.Fatalf("write PR artifacts: %v", err)
	}
	for _, key := range []string{"patch", "pr"} {
		if _, err := os.Stat(paths[key]); err != nil {
			t.Fatalf("expected %s artifact: %v", key, err)
		}
	}
	pr, err := os.ReadFile(paths["pr"])
	if err != nil {
		t.Fatalf("read pr: %v", err)
	}
	if !strings.Contains(string(pr), "fix/runtime-drift") || !strings.Contains(string(pr), "ConfigMap") {
		t.Fatalf("unexpected PR body:\n%s", pr)
	}
}

func TestRootExposesGuardianCommands(t *testing.T) {
	root := newRootCommand()
	for _, args := range [][]string{
		{"guardian", "install"},
		{"guardian", "report"},
		{"guardian", "diff"},
		{"guardian", "pr"},
	} {
		cmd, _, err := root.Find(args)
		if err != nil || cmd == nil || cmd.Name() != args[len(args)-1] {
			t.Fatalf("expected %v command, got cmd=%v err=%v", args, cmd, err)
		}
	}
}

func mustGuardianObject(t *testing.T, body string) *unstructured.Unstructured {
	t.Helper()
	obj, err := guardianObjectFromYAML(body)
	if err != nil {
		t.Fatalf("parse object: %v", err)
	}
	return obj
}

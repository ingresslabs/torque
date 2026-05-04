package deployplan

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestComputeDesiredQuotaTotals(t *testing.T) {
	manifest := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: demo
spec:
  replicas: 2
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
        - name: web
          image: example.com/web:1
          resources:
            requests:
              cpu: 100m
              memory: 64Mi
            limits:
              cpu: 200m
              memory: 128Mi
---
apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: demo
spec:
  selector:
    app: web
  ports:
    - port: 80
      targetPort: 8080
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: data
  namespace: demo
spec:
  accessModes: ["ReadWriteOnce"]
  resources:
    requests:
      storage: 10Gi
`
	docs := DocsToMap(ParseManifestDocs(manifest))
	totals, warnings := ComputeDesiredQuotaTotals(docs, "demo")
	if totals == nil {
		t.Fatalf("expected totals")
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
	if totals.Pods != 2 || totals.Services != 1 || totals.PVCs != 1 {
		t.Fatalf("unexpected object totals: %+v", totals)
	}
	if totals.CPURequests.Value != "200m" || totals.CPULimits.Value != "400m" {
		t.Fatalf("unexpected cpu totals: %+v", totals)
	}
	if totals.MemoryRequests.Value != "128Mi" || totals.MemoryLimits.Value != "256Mi" {
		t.Fatalf("unexpected memory totals: %+v", totals)
	}
	if totals.Storage.Value != "10Gi" {
		t.Fatalf("unexpected storage total: %+v", totals)
	}
}

func TestPopulateQuotaLive(t *testing.T) {
	client := fake.NewClientset(&corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rq",
			Namespace: "demo",
		},
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("1"),
				corev1.ResourcePods:        resource.MustParse("10"),
			},
			Used: corev1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("250m"),
				corev1.ResourcePods:        resource.MustParse("3"),
			},
		},
	})

	report := &QuotaReport{
		Namespace: "demo",
		Desired: QuotaUsageTotals{
			CPURequests: QuotaQuantity{Value: "200m"},
			Pods:        2,
		},
	}

	if err := PopulateQuotaLive(context.Background(), client, report); err != nil {
		t.Fatalf("populate: %v", err)
	}
	if len(report.Live) != 1 {
		t.Fatalf("expected 1 quota snapshot, got %d", len(report.Live))
	}

	var cpuRow *QuotaHeadroom
	for i := range report.Headroom {
		if report.Headroom[i].Resource == string(corev1.ResourceRequestsCPU) {
			cpuRow = &report.Headroom[i]
			break
		}
	}
	if cpuRow == nil {
		t.Fatalf("missing cpu headroom row")
	}
	if cpuRow.After != "450m" {
		t.Fatalf("expected after=450m, got %q", cpuRow.After)
	}
	if cpuRow.Headroom == "" || cpuRow.Status == "" {
		t.Fatalf("expected headroom and status, got %+v", *cpuRow)
	}
}

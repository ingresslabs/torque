package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/ingresslabs/torque/internal/analyze"
	"github.com/ingresslabs/torque/internal/kube"
	"github.com/ingresslabs/torque/internal/ui"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newAnalyzeCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	var targetPod string
	var namespace string
	var drift bool
	var cost bool
	var cluster bool
	var profile bool
	var rbac bool
	var duration time.Duration

	cmd := &cobra.Command{
		Use:        "analyze [POD_NAME]",
		Short:      "Analyze a Kubernetes pod for failures using local diagnostics",
		Hidden:     true,
		Deprecated: "use `torque logs`, `torque apply plan`, verifier, or stack health gates while this diagnostic surface is rebuilt",
		Long: `Analyze a pod to determine why it is failing.
It fetches the pod status, recent events, and logs, then runs them through a diagnostic engine.

Examples:
  # Analyze a specific pod in the current namespace
  torque analyze my-app-pod-123

  # Analyze a pod in a different namespace
  torque analyze my-app-pod-123 -n prod
  
  # Check for configuration drift (manual changes)
  torque analyze my-app-pod-123 --drift
  
  # Estimate monthly cost
  torque analyze my-app-pod-123 --cost
  
  # Profile resource usage vs requests
  torque analyze my-app-pod-123 --profile
  
  # Audit RBAC permissions for the pod
  torque analyze my-app-pod-123 --rbac
  
  # Analyze Cluster Health (Nodes, Global Events)
  torque analyze --cluster`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				targetPod = args[0]
			}
			return runAnalyze(cmd.Context(), kubeconfig, kubeContext, targetPod, namespace, drift, cost, cluster, profile, rbac, duration)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace (defaults to context)")
	cmd.Flags().BoolVar(&drift, "drift", false, "Check for configuration drift against 'kubectl.kubernetes.io/last-applied-configuration'")
	cmd.Flags().BoolVar(&cost, "cost", false, "Estimate monthly cost of the pod based on resource requests")
	cmd.Flags().BoolVar(&cluster, "cluster", false, "Run cluster-wide health checks (nodes, system pods)")
	cmd.Flags().BoolVar(&profile, "profile", false, "Profile resource usage (requires metrics-server)")
	cmd.Flags().BoolVar(&rbac, "rbac", false, "Audit RBAC permissions for the pod's ServiceAccount")
	cmd.Flags().DurationVar(&duration, "duration", 30*time.Second, "Timeout for analysis")

	return cmd
}

func runAnalyze(ctx context.Context, kubeconfig, kubeContext *string, podName, namespace string, drift bool, cost bool, cluster bool, profile bool, rbac bool, duration time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	// 1. Setup Kube Client
	var kc, kctx string
	if kubeconfig != nil {
		kc = *kubeconfig
	}
	if kubeContext != nil {
		kctx = *kubeContext
	}
	kClient, err := kube.New(ctx, kc, kctx)
	if err != nil {
		return fmt.Errorf("failed to init kube client: %w", err)
	}

	// 2. Resolve Namespace/Pod
	if namespace == "" {
		namespace = kClient.Namespace
		if namespace == "" {
			namespace = "default"
		}
	}

	// Cluster Analysis Mode
	if cluster {
		fmt.Println("Analyzing Cluster Health...")
		nodes, err := kClient.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}

		fmt.Printf("Checked %d nodes.\n", len(nodes.Items))
		for _, n := range nodes.Items {
			ready := false
			for _, c := range n.Status.Conditions {
				if c.Type == "Ready" && c.Status == "True" {
					ready = true
					break
				}
			}
			if !ready {
				color.New(color.FgRed).Printf("Node %s is NOT READY\n", n.Name)
			}

			// Check Pressure
			for _, c := range n.Status.Conditions {
				if c.Status == "True" && c.Type != "Ready" {
					color.New(color.FgYellow).Printf("Node %s has %s\n", n.Name, c.Type)
				}
			}
		}
		return nil
	}

	if podName == "" {
		return fmt.Errorf("pod name required (or use --cluster)")
	}

	fmt.Printf("Analyzing pod %s/%s...\n", namespace, podName)

	// 3. Gather Evidence
	evidence, err := analyze.GatherEvidence(ctx, kClient.Clientset, namespace, podName)
	if err != nil {
		// Mock evidence if we can't connect to cluster (for demo/dev purposes)
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "i/o timeout") || strings.Contains(err.Error(), "no such host") || strings.Contains(err.Error(), "invalid configuration") {
			fmt.Println("Warning: Could not connect to cluster. Using simulated evidence.")
			evidence = &analyze.Evidence{
				Logs: map[string]string{
					"broken-container": "Error: failed to create cgroup: openat2 /sys/fs/cgroup/kubepods.slice...: no such file or directory\npanic: runtime error: invalid memory address or nil pointer dereference",
				},
			}
		} else {
			return fmt.Errorf("failed to gather evidence: %w", err)
		}
	}

	// RBAC Audit
	if rbac {
		if evidence.Pod == nil {
			fmt.Println("Error: Cannot audit RBAC without pod details.")
			return nil
		}
		saName := evidence.Pod.Spec.ServiceAccountName
		fmt.Printf("Auditing RBAC for ServiceAccount %s/%s...\n", namespace, saName)

		// List RoleBindings
		rbs, err := kClient.Clientset.RbacV1().RoleBindings(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}

		var roles []string
		for _, rb := range rbs.Items {
			for _, s := range rb.Subjects {
				if s.Kind == "ServiceAccount" && s.Name == saName && (s.Namespace == "" || s.Namespace == namespace) {
					roles = append(roles, fmt.Sprintf("Role/%s", rb.RoleRef.Name))

					// Fetch Role to show rules
					role, err := kClient.Clientset.RbacV1().Roles(namespace).Get(ctx, rb.RoleRef.Name, metav1.GetOptions{})
					if err == nil {
						fmt.Printf("  Role: %s\n", rb.RoleRef.Name)
						for _, rule := range role.Rules {
							fmt.Printf("    - %v %v\n", rule.Verbs, rule.Resources)
						}
					}
				}
			}
		}

		// List ClusterRoleBindings
		crbs, err := kClient.Clientset.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for _, crb := range crbs.Items {
			for _, s := range crb.Subjects {
				if s.Kind == "ServiceAccount" && s.Name == saName && (s.Namespace == "" || s.Namespace == namespace) {
					roles = append(roles, fmt.Sprintf("ClusterRole/%s", crb.RoleRef.Name))

					// Fetch ClusterRole
					role, err := kClient.Clientset.RbacV1().ClusterRoles().Get(ctx, crb.RoleRef.Name, metav1.GetOptions{})
					if err == nil {
						fmt.Printf("  ClusterRole: %s\n", crb.RoleRef.Name)
						for _, rule := range role.Rules {
							fmt.Printf("    - %v %v\n", rule.Verbs, rule.Resources)
						}
					}
				}
			}
		}

		if len(roles) == 0 {
			fmt.Println("No explicit roles found (ServiceAccount might have no permissions).")
		}
		return nil
	}

	// Profiler
	if profile {
		if evidence.Pod == nil {
			fmt.Println("Error: Cannot profile without pod details.")
			return nil
		}
		fmt.Println("Profiling resource usage (fetching metrics)...")
		path := fmt.Sprintf("/apis/metrics.k8s.io/v1beta1/namespaces/%s/pods/%s", namespace, podName)
		data, err := kClient.Clientset.CoreV1().RESTClient().Get().AbsPath(path).DoRaw(ctx)
		if err != nil {
			if strings.Contains(err.Error(), "the server could not find the requested resource") || strings.Contains(err.Error(), "NotFound") {
				fmt.Println("Warning: No metrics available for this pod (it might be crashing, too new, or metrics-server is lagging).")
				return nil
			}
			return fmt.Errorf("failed to fetch metrics (is metrics-server installed?): %w", err)
		}

		type PodMetrics struct {
			Containers []struct {
				Name  string `json:"name"`
				Usage struct {
					CPU    string `json:"cpu"`
					Memory string `json:"memory"`
				} `json:"usage"`
			} `json:"containers"`
		}

		var metrics PodMetrics
		if err := json.Unmarshal(data, &metrics); err != nil {
			return fmt.Errorf("failed to parse metrics: %w", err)
		}

		for _, c := range metrics.Containers {
			fmt.Printf("Container: %s\n", c.Name)

			// Parse Usage
			cpuUsage, _ := resource.ParseQuantity(c.Usage.CPU)
			memUsage, _ := resource.ParseQuantity(c.Usage.Memory)

			fmt.Printf("  Usage:    CPU: %s, Mem: %s\n", c.Usage.CPU, c.Usage.Memory)

			// Find Spec
			for _, specC := range evidence.Pod.Spec.Containers {
				if specC.Name == c.Name {
					// Requests
					if req, ok := specC.Resources.Requests[corev1.ResourceCPU]; ok {
						fmt.Printf("  Request:  CPU: %s (Usage: %.0f%%)\n", req.String(), float64(cpuUsage.MilliValue())/float64(req.MilliValue())*100)
					}
					if req, ok := specC.Resources.Requests[corev1.ResourceMemory]; ok {
						fmt.Printf("  Request:  Mem: %s (Usage: %.0f%%)\n", req.String(), float64(memUsage.Value())/float64(req.Value())*100)
					}
					// Limits
					if lim, ok := specC.Resources.Limits[corev1.ResourceCPU]; ok {
						fmt.Printf("  Limit:    CPU: %s (Usage: %.0f%%)\n", lim.String(), float64(cpuUsage.MilliValue())/float64(lim.MilliValue())*100)
					}
					if lim, ok := specC.Resources.Limits[corev1.ResourceMemory]; ok {
						fmt.Printf("  Limit:    Mem: %s (Usage: %.0f%%)\n", lim.String(), float64(memUsage.Value())/float64(lim.Value())*100)
					}
				}
			}
		}
		return nil
	}

	// 4. Run Analysis
	analyzer := analyze.NewHeuristicAnalyzer()

	stop := ui.StartSpinner(os.Stdout, "Running analysis...")
	diagnosis, err := analyzer.Analyze(ctx, evidence)
	if err != nil {
		stop(false)
		return fmt.Errorf("analysis failed: %w", err)
	} else {
		stop(true)
	}

	// 5. Present Results
	printDiagnosis(diagnosis)

	if drift {
		driftReport := analyze.CheckDrift(evidence.Pod)
		if len(driftReport) > 0 {
			color.New(color.FgRed, color.Bold).Println("\n DRIFT DETECTED ")
			for _, line := range driftReport {
				fmt.Println(line)
			}
		} else {
			color.New(color.FgGreen).Println("\nNo configuration drift detected.")
		}
	}

	if cost {
		monthlyCost := analyze.EstimateCost(evidence.Pod)
		color.New(color.FgCyan, color.Bold).Println("\n COST ESTIMATION ")
		fmt.Printf("Estimated Monthly Cost: $%.2f\n", monthlyCost)
		fmt.Println("(Based on generic cloud pricing: $0.04/vCPU/hr, $0.004/GB/hr)")
	}

	return nil
}

func printDiagnosis(d *analyze.Diagnosis) {
	fmt.Println()
	color.New(color.FgCyan, color.Bold).Println(" ANALYSIS REPORT ")
	fmt.Println(strings.Repeat("=", 40))

	color.New(color.FgYellow).Printf("Root Cause: ")
	fmt.Printf("%s\n\n", d.RootCause)

	color.New(color.FgGreen).Printf("Suggestion: ")
	fmt.Printf("%s\n\n", d.Suggestion)

	if d.ConfidenceScore > 0 {
		fmt.Printf("Confidence: %.0f%%\n", d.ConfidenceScore*100)
	}

	if d.Explanation != "" {
		fmt.Println("\nExplanation:")
		fmt.Println(d.Explanation)
	}
}

package main

import (
	"context"

	"github.com/ingresslabs/torque/internal/deployplan"
	"k8s.io/client-go/kubernetes"
)

type quotaHeadroom = deployplan.QuotaHeadroom

func populateQuotaLive(ctx context.Context, client kubernetes.Interface, report *quotaReport) error {
	return deployplan.PopulateQuotaLive(ctx, client, report)
}

package main

import "github.com/ingresslabs/ktl/internal/deployplan"

type quotaQuantity = deployplan.QuotaQuantity
type quotaUsageTotals = deployplan.QuotaUsageTotals
type quotaReport = deployplan.QuotaReport

func buildDesiredQuotaReport(desired map[resourceKey]manifestDoc, targetNamespace string) *quotaReport {
	return deployplan.BuildDesiredQuotaReport(desired, targetNamespace)
}

func computeDesiredQuotaTotals(desired map[resourceKey]manifestDoc, targetNamespace string) (*quotaUsageTotals, []string) {
	return deployplan.ComputeDesiredQuotaTotals(desired, targetNamespace)
}

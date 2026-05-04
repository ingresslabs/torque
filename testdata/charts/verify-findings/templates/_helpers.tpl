{{- define "verify-findings.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "verify-findings.fullname" -}}
{{- printf "%s-%s" (include "verify-findings.name" .) .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

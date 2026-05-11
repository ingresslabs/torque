{{- define "torque-guardian-incident-e2e.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "torque-guardian-incident-e2e.fullname" -}}
{{- $name := include "torque-guardian-incident-e2e.name" . -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "torque-guardian-incident-e2e.labels" -}}
app.kubernetes.io/name: {{ include "torque-guardian-incident-e2e.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Expand the name of the chart.
*/}}
{{- define "mysql-provisioning-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "mysql-provisioning-operator.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "mysql-provisioning-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "mysql-provisioning-operator.labels" -}}
helm.sh/chart: {{ include "mysql-provisioning-operator.chart" . }}
{{ include "mysql-provisioning-operator.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: mysql-provisioning-operator
{{- end }}

{{- define "mysql-provisioning-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "mysql-provisioning-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "mysql-provisioning-operator.serviceAccountName" -}}
{{ include "mysql-provisioning-operator.fullname" . }}
{{- end }}

{{- define "mysql-provisioning-operator.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end }}

{{- define "mysql-provisioning-operator.webhookServiceName" -}}
{{ include "mysql-provisioning-operator.fullname" . }}-webhook
{{- end }}

{{- define "mysql-provisioning-operator.certificateName" -}}
{{ include "mysql-provisioning-operator.fullname" . }}-webhook-cert
{{- end }}

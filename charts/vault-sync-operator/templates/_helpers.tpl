{{/*
Expand the name of the chart.
*/}}
{{- define "vault-sync-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "vault-sync-operator.fullname" -}}
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

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "vault-sync-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "vault-sync-operator.labels" -}}
helm.sh/chart: {{ include "vault-sync-operator.chart" . }}
{{ include "vault-sync-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "vault-sync-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "vault-sync-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: controller-manager
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "vault-sync-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (printf "%s-controller-manager" (include "vault-sync-operator.fullname" .)) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the name of the namespace to use
*/}}
{{- define "vault-sync-operator.namespace" -}}
{{- if .Values.namespace.create }}
{{- default .Values.namespace.name .Release.Namespace }}
{{- else }}
{{- .Release.Namespace }}
{{- end }}
{{- end }}

{{/*
Create the name of the controller manager deployment
*/}}
{{- define "vault-sync-operator.deploymentName" -}}
{{- printf "%s-controller-manager" (include "vault-sync-operator.fullname" .) }}
{{- end }}

{{/*
Create the name of the metrics service
*/}}
{{- define "vault-sync-operator.serviceName" -}}
{{- printf "%s-controller-manager-metrics-service" (include "vault-sync-operator.fullname" .) }}
{{- end }}

{{/*
Create the name of the auth proxy service
*/}}
{{- define "vault-sync-operator.authProxyServiceName" -}}
{{- printf "%s-auth-proxy-service" (include "vault-sync-operator.fullname" .) }}
{{- end }}

{{/*
Create the name of the manager role
*/}}
{{- define "vault-sync-operator.managerRoleName" -}}
{{- printf "%s-manager-role" (include "vault-sync-operator.fullname" .) }}
{{- end }}

{{/*
Create the name of the auth proxy role
*/}}
{{- define "vault-sync-operator.authProxyRoleName" -}}
{{- printf "%s-auth-proxy-role" (include "vault-sync-operator.fullname" .) }}
{{- end }}

{{/*
Create the name of the kube rbac proxy cluster role
*/}}
{{- define "vault-sync-operator.kubeRbacProxyClusterRoleName" -}}
{{- printf "%s-kube-rbac-proxy" (include "vault-sync-operator.fullname" .) }}
{{- end }}
{{- define "hermes-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "hermes-operator.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}{{ .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else -}}{{ printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" }}{{- end -}}
{{- end -}}

{{- define "hermes-operator.labels" -}}
app.kubernetes.io/name: {{ include "hermes-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{- define "hermes-operator.selector" -}}
app.kubernetes.io/name: {{ include "hermes-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "hermes-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}{{ default (include "hermes-operator.fullname" .) .Values.serviceAccount.name }}
{{- else -}}{{ default "default" .Values.serviceAccount.name }}{{- end -}}
{{- end -}}

{{- define "hermes-operator.allNamespaces" -}}{{ eq .Values.watchNamespace "*" }}{{- end -}}

{{- /* namespace passed to --watch-namespace ("" = all) */ -}}
{{- define "hermes-operator.watchNamespace" -}}
{{- if eq .Values.watchNamespace "*" -}}{{- else if .Values.watchNamespace -}}{{ .Values.watchNamespace }}{{- else -}}{{ .Release.Namespace }}{{- end -}}
{{- end -}}

{{- define "hermes-operator.agentNamespace" -}}
{{- if .Values.agentNamespace -}}{{ .Values.agentNamespace }}
{{- else if and .Values.watchNamespace (ne .Values.watchNamespace "*") -}}{{ .Values.watchNamespace }}
{{- else -}}{{ .Release.Namespace }}{{- end -}}
{{- end -}}

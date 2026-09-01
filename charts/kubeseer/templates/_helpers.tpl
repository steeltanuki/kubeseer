{{/*
Copyright 2026 Alessandro Rontani

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/}}

{{- define "kubeseer.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kubeseer.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "kubeseer.name" . -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "kubeseer.namespace" -}}
{{- .Release.Namespace -}}
{{- end -}}

{{- define "kubeseer.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kubeseer.labels" -}}
helm.sh/chart: {{ include "kubeseer.chart" . }}
app.kubernetes.io/name: {{ include "kubeseer.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/part-of: kubeseer
app.kubernetes.io/managed-by: {{ .Release.Service }}
kubeseer.io/release: {{ .Release.Name }}
kubeseer.io/release-namespace: {{ .Release.Namespace }}
{{- end -}}

{{- define "kubeseer.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kubeseer.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "kubeseer.ownershipAnnotations" -}}
meta.helm.sh/release-name: {{ .Release.Name | quote }}
meta.helm.sh/release-namespace: {{ .Release.Namespace | quote }}
kubeseer.io/ownership: {{ printf "%s/%s" .Release.Namespace .Release.Name | quote }}
{{- end -}}

{{- define "kubeseer.serviceAccountName" -}}
{{- printf "%s-manager" (include "kubeseer.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kubeseer.webhookServiceName" -}}
{{- printf "%s-webhook" (include "kubeseer.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kubeseer.metricsServiceName" -}}
{{- printf "%s-metrics" (include "kubeseer.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kubeseer.webhookSecretName" -}}
{{- if eq .Values.certificate.mode "externalSecret" -}}
{{- .Values.certificate.externalSecret.secretName -}}
{{- else -}}
{{- printf "%s-webhook-tls" (include "kubeseer.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "kubeseer.webhookIssuerName" -}}
{{- printf "%s-webhook-issuer" (include "kubeseer.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kubeseer.webhookCAIssuerName" -}}
{{- printf "%s-ca-issuer" (include "kubeseer.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kubeseer.webhookCACertificateName" -}}
{{- printf "%s-webhook-ca" (include "kubeseer.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kubeseer.webhookCertificateName" -}}
{{- printf "%s-webhook-serving" (include "kubeseer.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kubeseer.webhookDNSNames" -}}
{{- $service := include "kubeseer.webhookServiceName" . -}}
{{- list $service (printf "%s.%s" $service (include "kubeseer.namespace" .)) (printf "%s.%s.svc" $service (include "kubeseer.namespace" .)) (printf "%s.%s.svc.cluster.local" $service (include "kubeseer.namespace" .)) | join "," -}}
{{- end -}}

{{- define "kubeseer.image" -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) -}}
{{- end -}}

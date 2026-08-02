{{/*
Expand the name of the chart.
*/}}
{{- define "latr.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "latr.fullname" -}}
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
{{- define "latr.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "latr.labels" -}}
helm.sh/chart: {{ include "latr.chart" . }}
{{ include "latr.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "latr.selectorLabels" -}}
app.kubernetes.io/name: {{ include "latr.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "latr.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "latr.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the name of the secret to use (single-instance mode)
*/}}
{{- define "latr.secretName" -}}
{{- if .Values.secrets.existingSecret }}
{{- .Values.secrets.existingSecret }}
{{- else }}
{{- include "latr.fullname" . }}
{{- end }}
{{- end }}

{{/*
Image name
*/}}
{{- define "latr.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.image.repository $tag }}
{{- end }}

{{/*
Multi-account mode: true when accounts list is non-empty.
*/}}
{{- define "latr.multiAccount" -}}
{{- if and .Values.accounts (gt (len .Values.accounts) 0) }}true{{- else }}false{{- end }}
{{- end }}

{{/*
Account workload name. Context: dict "root" $ "account" $account

Prefer accounts[].name (e.g. personal-account, work-account). Optional
accounts[].id is only used when name is omitted (resource name latr-<id>).

Names are used as Kubernetes resource names (Deployment/ConfigMap/Secret) and
must be valid DNS-1123 labels: lowercase alphanumeric, '-' allowed, must start
and end with alphanumeric, max 63 characters.
*/}}
{{- define "latr.accountFullname" -}}
{{- $account := .account -}}
{{- $name := "" -}}
{{- if $account.name -}}
{{- $name = $account.name | trunc 63 | trimSuffix "-" -}}
{{- else if $account.id -}}
{{- $name = printf "latr-%s" ($account.id | toString) | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- fail "accounts[] entries require .name (recommended) or .id" -}}
{{- end -}}
{{- if not (regexMatch "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" $name) -}}
{{- fail (printf "accounts[] name %q is not a valid DNS-1123 label (lowercase alphanumeric and '-', max 63, start/end alphanumeric)" $name) -}}
{{- end -}}
{{- $name -}}
{{- end }}

{{/*
Checksum material for one account's config only (avoids rolling all pods when
another account's config changes). Context: dict "root" $ "account" $account
*/}}
{{- define "latr.accountConfigChecksum" -}}
{{- $account := .account -}}
{{- if $account.configFiles -}}
{{- toYaml $account.configFiles -}}
{{- else if $account.config -}}
{{- toYaml $account.config -}}
{{- end -}}
{{- end }}

{{/*
Checksum material for one account's secrets only.
Context: dict "root" $ "account" $account
*/}}
{{- define "latr.accountSecretChecksum" -}}
{{- toYaml (.account.secrets | default dict) -}}
{{- end }}

{{/*
Account selector labels. Context: dict "root" $ "account" $account
*/}}
{{- define "latr.accountSelectorLabels" -}}
app.kubernetes.io/name: {{ include "latr.name" .root }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ include "latr.accountFullname" . }}
{{- end }}

{{/*
Account common labels. Context: dict "root" $ "account" $account
*/}}
{{- define "latr.accountLabels" -}}
helm.sh/chart: {{ include "latr.chart" .root }}
{{ include "latr.accountSelectorLabels" . }}
{{- if .root.Chart.AppVersion }}
app.kubernetes.io/version: {{ .root.Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .root.Release.Service }}
{{- end }}

{{/*
Account secret name. Context: dict "root" $ "account" $account
*/}}
{{- define "latr.accountSecretName" -}}
{{- $account := .account -}}
{{- $secrets := $account.secrets | default dict -}}
{{- if $secrets.existingSecret -}}
{{- $secrets.existingSecret -}}
{{- else -}}
{{- include "latr.accountFullname" . -}}
{{- end -}}
{{- end }}

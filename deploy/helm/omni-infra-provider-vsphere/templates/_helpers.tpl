{{/*
Expand the name of the chart.
*/}}
{{- define "omni-infra-provider-vsphere.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "omni-infra-provider-vsphere.fullname" -}}
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
{{- define "omni-infra-provider-vsphere.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Expand the namespace of the release.
Allows overriding it for multi-namespace deployments in combined charts.
*/}}
{{- define "omni-infra-provider-vsphere.namespace" -}}
{{- default .Release.Namespace .Values.namespaceOverride | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{/*
Common labels
*/}}
{{- define "omni-infra-provider-vsphere.labels" -}}
helm.sh/chart: {{ include "omni-infra-provider-vsphere.chart" . }}
{{ include "omni-infra-provider-vsphere.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "omni-infra-provider-vsphere.selectorLabels" -}}
app.kubernetes.io/name: {{ include "omni-infra-provider-vsphere.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "omni-infra-provider-vsphere.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "omni-infra-provider-vsphere.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Return the image tag to use.
*/}}
{{- define "omni-infra-provider-vsphere.imageTag" -}}
{{- .Values.image.tag | default .Chart.AppVersion -}}
{{- end -}}

{{/*
Return the name of the Secret holding the provider config file.
If 'existingConfigSecret' is set, use it.
Otherwise, use the chart's fullname suffix.
*/}}
{{- define "omni-infra-provider-vsphere.configSecretName" -}}
{{- if .Values.existingConfigSecret -}}
    {{- .Values.existingConfigSecret -}}
{{- else -}}
    {{- include "omni-infra-provider-vsphere.fullname" . -}}-config
{{- end -}}
{{- end -}}

{{/*
Return the key inside the config Secret that holds the provider config file.
The chart-managed Secret always uses 'config.yaml'.
*/}}
{{- define "omni-infra-provider-vsphere.configSecretKey" -}}
{{- if .Values.existingConfigSecret -}}
    {{- .Values.existingConfigSecretKey | default "config.yaml" -}}
{{- else -}}
    config.yaml
{{- end -}}
{{- end -}}

{{/*
Return the name of the Secret holding the Omni service account key.
If 'omni.serviceAccountKey.existingSecret' is set, use it.
Otherwise, use the chart's fullname suffix.
*/}}
{{- define "omni-infra-provider-vsphere.serviceAccountKeySecretName" -}}
{{- if .Values.omni.serviceAccountKey.existingSecret -}}
    {{- .Values.omni.serviceAccountKey.existingSecret -}}
{{- else -}}
    {{- include "omni-infra-provider-vsphere.fullname" . -}}-service-account-key
{{- end -}}
{{- end -}}

{{/*
Return the key inside the service account key Secret.
The chart-managed Secret always uses 'OMNI_SERVICE_ACCOUNT_KEY'.
*/}}
{{- define "omni-infra-provider-vsphere.serviceAccountKeySecretKey" -}}
{{- if .Values.omni.serviceAccountKey.existingSecret -}}
    {{- .Values.omni.serviceAccountKey.existingSecretKey | default "OMNI_SERVICE_ACCOUNT_KEY" -}}
{{- else -}}
    OMNI_SERVICE_ACCOUNT_KEY
{{- end -}}
{{- end -}}

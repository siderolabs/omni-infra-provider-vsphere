{{/*
Compile all validation checks here
*/}}
{{- define "omni-infra-provider-vsphere.validateValues" -}}

{{/* Provider Identity Check */}}
  {{- if empty .Values.provider.id -}}
    {{- fail "You must provide the infrastructure provider ID in provider.id (it must match the ID used in `omnictl infraprovider create <id>`)" -}}
  {{- end -}}

{{/* Omni Endpoint Check */}}
  {{- if empty .Values.omni.endpoint -}}
    {{- fail "You must provide the URL of the Omni API in omni.endpoint" -}}
  {{- end -}}

  {{/* Example.com Check - prevent users from deploying with placeholder values */}}
  {{- if contains "example.com" .Values.omni.endpoint -}}
    {{- fail "omni.endpoint contains 'example.com'. Please set the URL of your Omni instance." -}}
  {{- end -}}

{{/* Service Account Key Check */}}
  {{- if not .Values.omni.serviceAccountKey.existingSecret -}}
    {{- if empty .Values.omni.serviceAccountKey.value -}}
      {{- fail "You must provide the Omni service account key in omni.serviceAccountKey.value or use an existing secret via omni.serviceAccountKey.existingSecret" -}}
    {{- end -}}
  {{- end -}}

{{/* vSphere Config Check */}}
  {{- if not .Values.existingConfigSecret -}}
    {{- $vsphere := .Values.config.vsphere | default dict -}}

    {{- if empty $vsphere.uri -}}
      {{- fail "You must provide the vCenter SDK URL in config.vsphere.uri (or provide the config file via existingConfigSecret)" -}}
    {{- end -}}

    {{- if contains "example.com" $vsphere.uri -}}
      {{- fail "config.vsphere.uri contains 'example.com'. Please set the URL of your vCenter (or provide the config file via existingConfigSecret)." -}}
    {{- end -}}

    {{- if empty $vsphere.user -}}
      {{- fail "You must provide the vSphere user in config.vsphere.user (or provide the config file via existingConfigSecret)" -}}
    {{- end -}}

    {{- if empty $vsphere.password -}}
      {{- fail "You must provide the vSphere password in config.vsphere.password (or provide the config file via existingConfigSecret)" -}}
    {{- end -}}
  {{- end -}}

{{- end -}}

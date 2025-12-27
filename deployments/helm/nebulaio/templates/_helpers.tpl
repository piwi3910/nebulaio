{{/*
Expand the name of the chart.
*/}}
{{- define "nebulaio.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "nebulaio.fullname" -}}
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
{{- define "nebulaio.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "nebulaio.labels" -}}
helm.sh/chart: {{ include "nebulaio.chart" . }}
{{ include "nebulaio.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "nebulaio.selectorLabels" -}}
app.kubernetes.io/name: {{ include "nebulaio.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "nebulaio.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "nebulaio.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Return the secret name containing auth credentials
*/}}
{{- define "nebulaio.secretName" -}}
{{- if .Values.auth.existingSecret }}
{{- .Values.auth.existingSecret }}
{{- else }}
{{- include "nebulaio.fullname" . }}
{{- end }}
{{- end }}

{{/*
Return the image name
*/}}
{{- define "nebulaio.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.image.repository $tag }}
{{- end }}

{{/*
Return the headless service name
*/}}
{{- define "nebulaio.headlessServiceName" -}}
{{- printf "%s-headless" (include "nebulaio.fullname" .) }}
{{- end }}

{{/*
Return the cluster expect nodes value
*/}}
{{- define "nebulaio.expectNodes" -}}
{{- if .Values.cluster.expectNodes }}
{{- .Values.cluster.expectNodes }}
{{- else }}
{{- .Values.replicaCount }}
{{- end }}
{{- end }}

{{/*
Pod anti-affinity configuration
*/}}
{{- define "nebulaio.podAntiAffinity" -}}
{{- if eq .Values.podAntiAffinityPreset "hard" }}
podAntiAffinity:
  requiredDuringSchedulingIgnoredDuringExecution:
    - labelSelector:
        matchLabels:
          {{- include "nebulaio.selectorLabels" . | nindent 10 }}
      topologyKey: kubernetes.io/hostname
{{- else if eq .Values.podAntiAffinityPreset "soft" }}
podAntiAffinity:
  preferredDuringSchedulingIgnoredDuringExecution:
    - weight: 100
      podAffinityTerm:
        labelSelector:
          matchLabels:
            {{- include "nebulaio.selectorLabels" . | nindent 12 }}
        topologyKey: kubernetes.io/hostname
{{- end }}
{{- end }}

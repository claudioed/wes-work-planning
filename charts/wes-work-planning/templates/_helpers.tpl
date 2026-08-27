{{/*
Expand the name of the chart.
*/}}
{{- define "wes-work-planning.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "wes-work-planning.fullname" -}}
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
Chart name and version as used by the chart label.
*/}}
{{- define "wes-work-planning.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "wes-work-planning.labels" -}}
helm.sh/chart: {{ include "wes-work-planning.chart" . }}
{{ include "wes-work-planning.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "wes-work-planning.selectorLabels" -}}
app.kubernetes.io/name: {{ include "wes-work-planning.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "wes-work-planning.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "wes-work-planning.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Name of the Secret holding DATABASE_URL, when the chart creates its own.
*/}}
{{- define "wes-work-planning.databaseSecretName" -}}
{{- if .Values.database.existingSecret }}
{{- .Values.database.existingSecret }}
{{- else }}
{{- include "wes-work-planning.fullname" . }}-database
{{- end }}
{{- end }}

{{/*
Fully qualified name of the analytics projector deployment (ADR-0011).
*/}}
{{- define "wes-work-planning.projectorFullname" -}}
{{- include "wes-work-planning.fullname" . }}-projector
{{- end }}

{{/*
Fully qualified name of the analytics reports deployment/service (ADR-0011).
*/}}
{{- define "wes-work-planning.reportsFullname" -}}
{{- include "wes-work-planning.fullname" . }}-reports
{{- end }}

{{/*
Name of the Secret holding the analytics DSNs, when the chart creates its own.
*/}}
{{- define "wes-work-planning.analyticsSecretName" -}}
{{- if .Values.analytics.database.existingSecret }}
{{- .Values.analytics.database.existingSecret }}
{{- else }}
{{- include "wes-work-planning.fullname" . }}-analytics
{{- end }}
{{- end }}

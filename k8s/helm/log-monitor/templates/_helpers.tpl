{{- define "log-monitor.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "log-monitor.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "log-monitor.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "log-monitor.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" -}}
{{- end -}}

{{- define "log-monitor.labels" -}}
helm.sh/chart: {{ include "log-monitor.chart" . }}
app.kubernetes.io/name: {{ include "log-monitor.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "log-monitor.selectorLabels" -}}
app.kubernetes.io/name: {{ include "log-monitor.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "log-monitor.redisFullname" -}}
{{- printf "%s-redis" (include "log-monitor.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "log-monitor.streamerFullname" -}}
{{- printf "%s-k8s-log-streamer" (include "log-monitor.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "log-monitor.testLoggerFullname" -}}
{{- printf "%s-test-logger" (include "log-monitor.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "log-monitor.postgresFullname" -}}
{{- printf "%s-postgres" (include "log-monitor.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "log-monitor.logApiFullname" -}}
{{- printf "%s-log-api-service" (include "log-monitor.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

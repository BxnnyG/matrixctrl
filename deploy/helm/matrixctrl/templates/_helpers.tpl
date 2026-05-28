{{/*
Expand the name of the chart.
*/}}
{{- define "matrixctrl.name" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels applied to every resource.
*/}}
{{- define "matrixctrl.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
app.kubernetes.io/name: {{ include "matrixctrl.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels — used by Service and Deployment.
*/}}
{{- define "matrixctrl.selectorLabels" -}}
app.kubernetes.io/name: {{ include "matrixctrl.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Name of the Secret holding credentials.
*/}}
{{- define "matrixctrl.secretName" -}}
{{ include "matrixctrl.name" . }}-secret
{{- end }}

{{/*
Full database URL for the matrixctrl container.
*/}}
{{- define "matrixctrl.dbURL" -}}
postgres://matrixctrl:$(MATRIXCTRL_DB_PASSWORD)@127.0.0.1:5432/matrixctrl?sslmode=disable
{{- end }}

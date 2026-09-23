{{- define "hatchery.fullname" -}}
{{- if contains "hatchery" .Release.Name -}}
{{- .Release.Name | trunc 40 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-hatchery" .Release.Name | trunc 40 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "hatchery.labels" -}}
app.kubernetes.io/part-of: hatchery
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{/* Selector labels for one component: include with (dict "ctx" . "component" "operator"). */}}
{{- define "hatchery.selector" -}}
app.kubernetes.io/name: {{ .component }}
app.kubernetes.io/instance: {{ .ctx.Release.Name }}
{{- end -}}

{{- define "hatchery.componentLabels" -}}
{{ include "hatchery.labels" .ctx }}
{{ include "hatchery.selector" . }}
{{- end -}}

{{/* Full image reference: include with (dict "ctx" . "image" .Values.operator.image). */}}
{{- define "hatchery.image" -}}
{{- $repo := .image.repository -}}
{{- if .ctx.Values.imageRegistry -}}{{- $repo = printf "%s/%s" (trimSuffix "/" .ctx.Values.imageRegistry) $repo -}}{{- end -}}
{{- printf "%s:%s" $repo (default .ctx.Chart.AppVersion .image.tag) -}}
{{- end -}}

{{- define "hatchery.operatorName" -}}{{ include "hatchery.fullname" . }}-operator{{- end -}}
{{- define "hatchery.panelName" -}}{{ include "hatchery.fullname" . }}-panel{{- end -}}
{{- define "hatchery.webhookName" -}}{{ include "hatchery.fullname" . }}-webhook{{- end -}}
{{- define "hatchery.postgresName" -}}{{ include "hatchery.fullname" . }}-postgres{{- end -}}
{{- define "hatchery.valkeyName" -}}{{ include "hatchery.fullname" . }}-valkey{{- end -}}

{{/*
A generated password that survives upgrades: reuses the key from the live Secret when there is one.
include with (dict "ctx" . "secret" <name> "key" <key>).
*/}}
{{- define "hatchery.persistentPassword" -}}
{{- $existing := lookup "v1" "Secret" .ctx.Release.Namespace .secret -}}
{{- if and $existing (index $existing.data .key) -}}
{{- index $existing.data .key -}}
{{- else -}}
{{- randAlphaNum 32 | b64enc -}}
{{- end -}}
{{- end -}}

{{- define "hatchery.imagePullSecrets" -}}
{{- with .Values.imagePullSecrets }}
imagePullSecrets:
{{- toYaml . | nindent 2 }}
{{- end }}
{{- end -}}

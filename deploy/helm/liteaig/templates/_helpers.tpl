{{- define "liteaig.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "liteaig.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "liteaig.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "liteaig.labels" -}}
app.kubernetes.io/name: {{ include "liteaig.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "liteaig.workloadName" -}}
{{- printf "%s-%s" (include "liteaig.fullname" .root) .name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "liteaig.selectorLabels" -}}
app.kubernetes.io/name: {{ include "liteaig.name" .root }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .name }}
{{- end -}}

{{- define "liteaig.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "liteaig.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "liteaig.validateWorkload" -}}
{{- $replicas := int .workload.replicas -}}
{{- $minAvailable := int .workload.pdb.minAvailable -}}
{{- $maxUnavailable := int .workload.rollingUpdate.maxUnavailable -}}
{{- $db := default dict .workload.database -}}
{{- if ge $minAvailable $replicas -}}
{{- fail (printf "invalid HA values for %s: pdb.minAvailable must be lower than replicas" .name) -}}
{{- end -}}
{{- if ge $maxUnavailable $replicas -}}
{{- fail (printf "invalid HA values for %s: rollingUpdate.maxUnavailable must be lower than replicas" .name) -}}
{{- end -}}
{{- if lt (int .root.Values.drain.terminationGracePeriodSeconds) (int .root.Values.drain.forceShutdownTimeoutSeconds) -}}
{{- fail "invalid HA values: terminationGracePeriodSeconds must cover forceShutdownTimeoutSeconds" -}}
{{- end -}}
{{- if and .workload.autoscaling.enabled (gt (int .workload.autoscaling.minReplicas) (int .workload.autoscaling.maxReplicas)) -}}
{{- fail (printf "invalid HA values for %s: autoscaling.minReplicas must not exceed maxReplicas" .name) -}}
{{- end -}}
{{- if and $db.enabled (ne .workload.mode "all") -}}
{{- fail (printf "invalid database values for %s: SQLite persistence requires mode=all" .name) -}}
{{- end -}}
{{- if and $db.enabled (ne $replicas 1) -}}
{{- fail (printf "invalid database values for %s: SQLite persistence requires replicas=1" .name) -}}
{{- end -}}
{{- if and $db.enabled .workload.autoscaling.enabled -}}
{{- fail (printf "invalid database values for %s: SQLite persistence requires autoscaling.enabled=false" .name) -}}
{{- end -}}
{{- end -}}

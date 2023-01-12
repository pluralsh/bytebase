{{- define "pg.env" }}
- name: DATABASE_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{.Values.database.existingSecret}}
      key: password
-  { name: DB_URL, value: "{{ printf "%s://%s:$(DATABASE_PASSWORD)@%s:%s/%s" .Values.database.type .Values.database.user  .Values.database.host .Values.database.port .Values.database.name }}"}
{{- end -}}
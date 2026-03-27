import os

with open('src/simple/app.py', 'r') as f:
    app_py_code = f.read()

# 1. Manifests Version
manifests_header = """---
# ConfigMap for main application code
apiVersion: v1
kind: ConfigMap
metadata:
  name: cluster-intel-app
  namespace: utilities
data:
  app.py: |-
"""
manifests_body_lines = ['    ' + line for line in app_py_code.split('\n')]
manifests_content = manifests_header + '\n'.join(manifests_body_lines) + '\n'

with open('manifests/backend/app-configmap.yaml', 'w') as f:
    f.write(manifests_content)

# 2. Charts Version
charts_header = """---
# ConfigMap - Application Code
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ include "cluster-intel.fullname" . }}-app
  labels:
    {{- include "cluster-intel.labels" . | nindent 4 }}
data:
  app.py: |-
"""
charts_body_lines = []
for line in app_py_code.split('\n'):
    new_line = line.replace('{{', '{{ "{{" }}').replace('}}', '{{ "}}" }}')
    charts_body_lines.append('    ' + new_line)
charts_content = charts_header + '\n'.join(charts_body_lines) + '\n'

with open('charts/cluster-intel/templates/app-configmap.yaml', 'w') as f:
    f.write(charts_content)

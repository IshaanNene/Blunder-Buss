apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: ${certificate_authority}
    server: https://${endpoint}
  name: ${cluster_name}
contexts:
- context:
    cluster: ${cluster_name}
    user: ${cluster_name}
  name: ${cluster_name}
current-context: ${cluster_name}
kind: Config
preferences: {}
users:
- name: ${cluster_name}
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      command: gcloud
      args:
        - container
        - clusters
        - get-credentials
        - ${cluster_name}
        - --region
        - ${region}
        - --project
        - ${project_id}
      env:
        - name: USE_GKE_GCLOUD_AUTH_PLUGIN
          value: "True"
[![Docker](https://github.com/knowshan/simplecopier-controller/actions/workflows/docker-publish.yml/badge.svg?branch=main)](https://github.com/knowshan/simplecopier-controller/actions/workflows/docker-publish.yml)'


# Simple Copier for K8s Deployments

![copier-controller-demo-gif](./copier-controller-demo.gif)

**Deployment copy with different image version and labels.**

Example:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
  labels:
    app: nginx
  annotations:
    knowshan.github.io/SimpleCopier-image-version: "latest"
    "knowshan.github.io/SimpleCopier-image-replace": |
      "{\"nginx:1.14.2\" : \"nginx:latest\"}"
spec:
  replicas: 3
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:1.14.2
        ports:
        - containerPort: 80
```

In above case of, SimpleCopier controller will create a new deployment `nginx-latest` with image `nginx:latest` (replacing `nginx:1.14.2`) and labels updated with suffix `-latest`.

Use cases for having a deployment copy with different image:
 * Upgrade tests
 * Compare different versions
 * Security risk assessment - Check if different image version will pass security gate  
 * Any other tests and verifications

This will be useful if you're managing Kubernetes resources using tools like Terraform or Helm. Instead of additional provisioning code, you can have a copy of your deployment with separate name, labels and image by simply adding annotations.

## ToDo
 * Add tests - Ginkgo and kuttl
 * Add copier for other Kubernetes resources such as service and ingress
 * Explore creating a custom resource for patching - Override database configuration or credentials that shouldn't be shared with the copied deployment  

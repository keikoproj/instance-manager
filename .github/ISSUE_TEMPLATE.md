**Is this a BUG REPORT or FEATURE REQUEST?**:

**What happened**:

**What you expected to happen**:

**How to reproduce it (as minimally and precisely as possible)**:

**Anything else we need to know?**:

**Environment**:

- Kubernetes version:

```bash
kubectl version -o yaml
```

**Other debugging information (if applicable)**:

- InstanceGroup status:

```bash
kubectl describe instancegroup <ig-name>
```

- controller logs:

```bash
kubectl logs <instance-manager pod>
```

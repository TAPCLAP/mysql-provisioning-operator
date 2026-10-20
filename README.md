# mysql-provisioning-operator

[Russian](README_ru.md)

The operator connects to MySQL servers that are already running and creates databases, accounts, grants, and Kubernetes Secrets from custom resources. It does not install MySQL, choose a new primary, or take backups.

Tenants create `MySQLDatabase` and `MySQLUser` in their own namespaces. Admin credentials and tenant policy stay in the operator namespace. The operator never runs `DROP DATABASE`.

## Example

The platform points the operator at a MySQL pair that is already running and allows the `payments` namespace to create names that start with `payments_`. The tenant then asks for a schema and an account. The operator creates both on the detected master and writes a Secret next to the application. The password is generated and is not stored in the custom resource.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: db1-admin
  namespace: mysql-provisioning-system
stringData:
  username: operator
  password: replace-me
---
apiVersion: mysql-provisioning.tapclap.com/v1alpha1
kind: MySQLCluster
metadata:
  name: db1
  namespace: mysql-provisioning-system
spec:
  members:
    - name: db1-1
      host: db1-1.internal
    - name: db1-2
      host: db1-2.internal
  credentialsSecret:
    name: db1-admin
  privilegeReplication: Replicated
  defaultTarget: Master
  master:
    mode: Detected
---
apiVersion: mysql-provisioning.tapclap.com/v1alpha1
kind: MySQLTenant
metadata:
  name: payments
  namespace: mysql-provisioning-system
spec:
  namespaces: ["payments"]
  access:
    - cluster: db1
      databases:
        patterns: ["payments_*"]
      users:
        patterns: ["payments_*"]
      hosts: ["%"]
      targets: ["Master"]
      privileges: ["SELECT", "INSERT", "UPDATE", "DELETE"]
---
apiVersion: mysql-provisioning.tapclap.com/v1alpha1
kind: MySQLDatabase
metadata:
  name: payments-main
  namespace: payments
spec:
  cluster: db1
  name: payments_main
---
apiVersion: mysql-provisioning.tapclap.com/v1alpha1
kind: MySQLUser
metadata:
  name: payments-app
  namespace: payments
spec:
  cluster: db1
  username: payments_app
  authPlugin: caching_sha2_password
  requireTLS: true
  grants:
    - database: payments_main
      privileges: ["SELECT", "INSERT", "UPDATE", "DELETE"]
  secret:
    name: payments-app
    data:
      USERNAME: "{{ .Username }}"
      PASSWORD: "{{ .Password }}"
      PRIMARY_HOST: "{{ .Master.Host }}"
      REPLICA_HOST: "{{ .Replica.Host }}"
```

The operator picks the member that is writable and has no replication source. The other member receives the schema and the account through the binlog. The Secret `payments-app` in the `payments` namespace receives `USERNAME`, `PASSWORD`, `PRIMARY_HOST`, and `REPLICA_HOST`. Deleting the database object leaves the schema in MySQL. Deleting the user drops the account.

The same objects, with every field explained, are in [Using the operator](docs/usage.md).

## Documentation

- [Using the operator](docs/usage.md)

## Install

cert-manager must already be installed. The validating webhook rejects invalid resources (`failurePolicy: Fail`).

`mysql-provisioning-operator` includes the CRDs, the controller, and the validating webhook. Installing that chart is enough. `mysql-provisioning-operator-crds` is the same CRDs without the operator; install it first only when CRDs are managed on their own. Helm does not upgrade CRDs from that chart's `crds/` directory on a later upgrade.

```bash
helm install mysql-provisioning-operator-crds \
  oci://ghcr.io/tapclap/charts/mysql-provisioning-operator-crds \
  --version 0.0.12

helm install mysql-provisioning-operator \
  oci://ghcr.io/tapclap/charts/mysql-provisioning-operator \
  --version 0.0.12 \
  --namespace mysql-provisioning-system \
  --create-namespace
```

Create `MySQLCluster` and `MySQLTenant` in `mysql-provisioning-system`. A published release note contains the same commands for the registry that built that version.

## Development

```bash
make test                 # unit tests and envtest, no Docker
make test-integration     # controllers against a real MySQL container
make lint
```

Go 1.25. `make help` lists the other targets.

## License

Apache License 2.0.

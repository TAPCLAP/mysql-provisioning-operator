# mysql-provisioning-operator

[Russian](README_ru.md)

The operator connects to MySQL servers that are already running and creates databases, accounts, grants, and Kubernetes Secrets from custom resources. It does not install MySQL, choose a new primary, or take backups.

Tenants create `MySQLDatabase` and `MySQLUser` in their own namespaces. Admin credentials and tenant policy stay in the operator namespace. The operator never runs `DROP DATABASE`.

## Documentation

- [Using the operator](docs/usage.md)

## Install

cert-manager must already be installed. The validating webhook rejects invalid resources (`failurePolicy: Fail`).

```bash
helm install mysql-provisioning-operator \
  oci://ghcr.io/tapclap/charts/mysql-provisioning-operator \
  --version 0.0.1 \
  --namespace mysql-provisioning-system \
  --create-namespace
```

Create `MySQLCluster` and `MySQLTenant` in `mysql-provisioning-system`. A published release note contains the same command for the registry that built that version.

## Development

```bash
make test                 # unit tests and envtest, no Docker
make test-integration     # controllers against a real MySQL container
make lint
```

Go 1.25. `make help` lists the other targets.

## License

Apache License 2.0.

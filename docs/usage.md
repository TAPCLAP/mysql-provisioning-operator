# Using the operator

[Русская версия](usage.ru.md)

API group: `mysql-provisioning.tapclap.com/v1alpha1`.

The operator reconciles four resources. Platform objects live in the operator namespace (the Helm release namespace, usually `mysql-provisioning-system`). Tenant objects live next to the application.

| Resource | Who writes it | What it does |
|---|---|---|
| `MySQLCluster` | platform | Addresses, admin Secret, TLS, and how the master is chosen |
| `MySQLTenant` | platform | Which namespaces may create which names, hosts, targets, and privileges |
| `MySQLDatabase` | tenant | `CREATE DATABASE` on the selected members |
| `MySQLUser` | tenant | Account, grants, password, and a Secret in the same namespace |

A tenant refers to a cluster by name only and never sees the admin Secret.

## Install

cert-manager must be installed before the chart. The webhook `failurePolicy` is `Fail`, so a missing certificate blocks creates and updates of these resources.

`mysql-provisioning-operator` includes the CRDs, the controller, and the validating webhook. Installing that chart is enough. `mysql-provisioning-operator-crds` contains the same CRDs and nothing else; install it first only when CRDs are managed separately. A later upgrade of the CRD chart does not change CRDs that Helm already applied from its `crds/` directory.

```bash
helm install mysql-provisioning-operator-crds \
  oci://ghcr.io/tapclap/charts/mysql-provisioning-operator-crds \
  --version 0.0.3

helm install mysql-provisioning-operator \
  oci://ghcr.io/tapclap/charts/mysql-provisioning-operator \
  --version 0.0.3 \
  --namespace mysql-provisioning-system \
  --create-namespace
```

`spec.tls` on `MySQLCluster` is a separate setting: it is how the operator itself dials MySQL.

## Register a cluster

Create the admin Secret in the operator namespace. Keys are `username` and `password`.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: db1-admin
  namespace: mysql-provisioning-system
stringData:
  username: operator
  password: replace-me
```

To connect with TLS, add the CA as a PEM bundle. `caSecret` reads a Secret and `caConfigMap` reads a ConfigMap. Set `key` when the data is not under `ca.crt`. Only one of the two sources may be set. The bundle is added to the system trust store. Optional mutual TLS uses `tls.crt` and `tls.key` in `spec.tls.clientSecret`.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: db1-ca
  namespace: mysql-provisioning-system
data:
  ca.crt: BASE64_PEM
```

```yaml
apiVersion: mysql-provisioning.tapclap.com/v1alpha1
kind: MySQLCluster
metadata:
  name: db1
  namespace: mysql-provisioning-system
spec:
  members:
    - name: db1-1
      host: db1-1.internal
      port: 3306
  credentialsSecret:
    name: db1-admin
  tls:
    mode: Required          # Disabled | Required | SkipVerify
    caSecret:
      name: db1-ca
    serverName: db1.internal
  privilegeReplication: Replicated
  defaultTarget: Master
  master:
    mode: Detected
```

`tls.mode` defaults to `Disabled` (plain TCP). `Required` verifies the server certificate. `SkipVerify` encrypts and does not verify it. `caSecret` and `caConfigMap` are allowed only with `Required`. `serverName` is the name used for verification and SNI; when it is empty, each member host is used.

```yaml
tls:
  mode: Required
  caConfigMap:
    name: db1-ca
    key: ca.pem
```

`privilegeReplication` says whether `CREATE DATABASE` and `CREATE USER` travel through the binlog:

- `Replicated` (default): run the statement on the master and let replicas receive it. This is the normal mode. `target: All` is rejected.
- `Local`: each member is updated on its own. The operator sets `sql_log_bin=0` for that session so the statement is not written to the binlog again.

`defaultTarget` is used when a database or user omits `spec.target`. The default is `Master`.

Watch the cluster until it is ready:

```bash
kubectl get mysqlcluster -n mysql-provisioning-system
```

`status.master` is the member that will receive `target: Master` statements. `status.members` records reachability, `server_id`, and which criteria each member passed.

### How the master is chosen

`master.mode: Detected` probes every member on each reconcile. Enabled criteria are combined with AND. At least one criterion must be enabled. Omitted criteria default to on:

- `readOnlyOff`: `@@global.read_only = 0`
- `superReadOnlyOff`: `@@global.super_read_only = 0`
- `noReplicationConfigured`: no replication source is configured. A stopped replica thread still counts as configured when `SOURCE_HOST` or `MASTER_HOST` is set. A Group Replication primary has channels, so turn this criterion off or use `Explicit`.

Zero matches sets condition `NoMaster`. More than one match sets `SplitBrain`. In both cases the operator skips DDL for that cycle.

`master.mode: Explicit` uses the single member with `master: true` and does not use the criteria to choose. Exactly one member must be marked. `master: true` is forbidden in `Detected` mode. An unreachable explicit master sets `MasterUnavailable` and DDL is skipped. Status still records whether that member would have passed the criteria.

```yaml
spec:
  master:
    mode: Explicit
  members:
    - name: db1-1
      host: db1-1.internal
      master: true
    - name: db1-2
      host: db1-2.internal
```

Changing the explicit master is a change to `MySQLCluster`. Nothing is deleted from the previous member.

### Where a database and a user are created

`spec.target` is `Master` or `All`. An empty target uses the cluster `defaultTarget`. A tenant policy can restrict the allowed values.

- `Master`: execute on the selected master with the binlog enabled, so replicas receive the statement when `privilegeReplication` is `Replicated`. After a master change the next reconcile runs `CREATE ... IF NOT EXISTS` on the new master. Existing copies are left in place.
- `All`: execute on every member with `sql_log_bin=0`. A member that cannot be reached leaves the object `Degraded`; the next reconcile creates the object only where it is still missing. A new member receives existing `All` objects. `Master` objects are created there only if that member becomes the master.

Switching `Master` to `All` creates the missing copies. Switching back does not delete them.

## Allow a tenant

`MySQLTenant` is also only in the operator namespace. A namespace may match several policies; permissions are the union. No matching policy means deny. The webhook checks this on `kubectl apply`, and the controller checks again before any SQL.

```yaml
apiVersion: mysql-provisioning.tapclap.com/v1alpha1
kind: MySQLTenant
metadata:
  name: payments
  namespace: mysql-provisioning-system
spec:
  namespaces: ["payments", "payments-staging"]
  quotas:
    maxDatabases: 20
    maxUsers: 50
  access:
    - cluster: db1
      databases:
        type: Glob
        patterns: ["payments_*"]
      users:
        type: Glob
        patterns: ["payments_*"]
      hosts: ["%"]
      targets: ["Master"]
      requireTLS: Required
      privileges: ["SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "INDEX", "ALTER"]
```

The checks on a database or user:

- the object namespace is listed in `spec.namespaces`;
- `spec.cluster` appears in `access`;
- the database name matches `databases.patterns`, and the username matches `users.patterns`;
- every `grants[].database` matches those database patterns, so a grant cannot point at another tenant's schema;
- privileges are a subset of the allow-list;
- hosts are a subset of `hosts`;
- `target` is one of `targets`;
- the Secret is created only in the object's own namespace.

`type` is `Glob` (default, `*` and `?`) or `Regex` (Go RE2, matched against the whole name). A pattern is at most 256 characters. An empty `hosts` or `targets` list on an access entry allows nothing for that entry: write the values you intend to permit.

`requireTLS: Required` rejects a user with `spec.requireTLS: false`. `Optional` allows both.

Quotas count live objects in the tenant namespace. A nil quota is unlimited. If any matching policy leaves the quota unset, the result is unlimited. Otherwise the highest set limit is used.

These privileges and schemas are refused even when the allow-list is wide:

- schemas `mysql`, `sys`, `information_schema`, `performance_schema`;
- privileges `SUPER`, `FILE`, `SHUTDOWN`, `RELOAD`, `BINLOG_ADMIN`, `SYSTEM_USER`, `CONNECTION_ADMIN`, `GRANT OPTION`, `CREATE USER`.

`ALL` is allowed only on a named database that matches the tenant patterns, and only when that tenant's `privileges` list includes `ALL`. It cannot be granted on `*.*` or on `mysql`, `sys`, `information_schema`, or `performance_schema`.

`status.orphanDatabases` lists schemas that match the tenant patterns and have no `MySQLDatabase` object. The list is informational. The operator does not delete those schemas.

## Create a database

```yaml
apiVersion: mysql-provisioning.tapclap.com/v1alpha1
kind: MySQLDatabase
metadata:
  name: payments-main
  namespace: payments
spec:
  cluster: db1
  name: payments_main
  characterSet: utf8mb3
  collation: utf8mb3_general_ci
  target: Master
```

`name` is a MySQL identifier of at most 64 characters and is immutable. `characterSet` and `collation` are immutable and are applied only in `CREATE DATABASE`. The defaults are `utf8mb3` and `utf8mb3_general_ci`. If the schema already exists with a different charset or collation, the operator leaves it unchanged and sets `CharacterSetDrift`. There is no `ALTER DATABASE`.

`status.appliedOn` lists members where the schema is present.

Deleting the object, the namespace, or the operator does not drop the schema. There is no finalizer and no flag that deletes it. The object can be deleted immediately, and the operator emits `DatabaseRetained`. Creating the object again adopts the existing schema.

## Create a user

The password is not stored in the custom resource. If the Secret has no `PASSWORD` key, the operator generates one and writes it there. Status stores a hash and the retention deadline, not the password.

```yaml
apiVersion: mysql-provisioning.tapclap.com/v1alpha1
kind: MySQLUser
metadata:
  name: payments-app
  namespace: payments
spec:
  cluster: db1
  username: payments_app
  hosts: ["%"]
  authPlugin: caching_sha2_password
  requireTLS: true
  target: Master
  maxUserConnections: 20
  password:
    retention: 24h
    generation: 1
  grants:
    - database: payments_main
      privileges: ["SELECT", "INSERT", "UPDATE", "DELETE"]
      tables: ["*"]
  secret:
    name: payments-app
    data:
      USERNAME: "{{ .Username }}"
      PASSWORD: "{{ .Password }}"
      PASSWORD_PREVIOUS: "{{ .PreviousPassword }}"
      HOSTS: "{{ .Hosts | join \",\" }}"
      PRIMARY_HOST: "{{ .Master.Host }}"
      DSN: "{{ .Username }}:{{ .Password }}@tcp({{ .Master.Host }}:{{ .Master.Port }})/payments_main"
    entries:
      - forEach: members
        key: "{{ .Item.Name | upper }}_HOST"
        value: "{{ .Item.Host }}"
```

`username` is immutable and at most 32 characters. Each host is a separate account. An empty `hosts` list means `%`. `authPlugin` is `caching_sha2_password` or `mysql_native_password`. If the server rejects the plugin, status records the error. The operator does not switch plugins on its own.

`requireTLS: true` means `REQUIRE SSL`. `false` means `REQUIRE NONE`. Changing the flag runs `ALTER USER` and does not rotate the password.

Grants are reconciled from `SHOW GRANTS`: missing privileges are granted and extra ones are revoked. `USAGE` on `*.*` is ignored. A grant waits until that database exists.

`maxUserConnections` and `maxQueriesPerHour` are optional limits on the account.

### Secret template

Templates use Go `text/template` and a reduced Sprig set. `env`, `expandenv`, and `getHostByName` are not available. The template can see only this user: `Username`, `Password`, `PreviousPassword`, `Hosts`, `Plugin`, `Cluster`, `Master`, `Members`, `Databases`. It cannot read other Secrets.

`PASSWORD` and `PASSWORD_PREVIOUS` are reserved. The operator sets them after rendering, so a template cannot replace the password with something else. `entries` repeats a key and value for each cluster member. `forEach` supports only `members`. Rendered keys must match `[A-Za-z0-9_.-]+`.

The Secret is created in the same namespace as the `MySQLUser`, with an owner reference. The operator does not watch Secrets. A hand edit of `PASSWORD`, and the moment a retained password should be discarded, are picked up on the next reconcile: the sooner of one minute, the retention deadline, and `rotateEvery`. A change to the `MySQLUser` spec is reconciled immediately.

### Password rotation

MySQL keeps at most two passwords. The operator uses `ALTER USER ... RETAIN CURRENT PASSWORD` and later `DISCARD OLD PASSWORD`. There is no path that replaces a password instantly.

`spec.password.retention` is how long the previous password stays valid. The default is `24h`. The minimum is `5m`. During that window the Secret contains `PASSWORD` (new) and `PASSWORD_PREVIOUS` (old). The discard time is stored on the Secret as `mysql-provisioning.tapclap.com/previous-discard-after` and copied to `status.password.retainedUntil`. The annotation survives a lost status.

Rotation starts for one of two reasons:

- `spec.password.generation` increases. The operator rotates once for that value and records it in `status.password.generation`. This is the GitOps trigger.
- `spec.password.rotateEvery` elapses. An empty value means manual rotation only. The interval must be longer than `retention`, because a new rotation cannot start while the previous password is still retained (`RotationInProgress`).

Editing the `PASSWORD` key by hand is also a rotation and retains the current password when the server supports it. Writing back the value the operator already stored does not rotate. If the server is older than MySQL 8.0.14, rotation does not start (`PasswordRetentionUnsupported`) and the current password stays.

### Deleting a user

The object has finalizer `mysql-provisioning.tapclap.com/user-cleanup`. Deleting it runs `DROP USER` for each managed `user@host` on the same targets used to create the account, then deletes the Secret. While MySQL cannot be reached, the finalizer keeps the object. If the `MySQLCluster` itself is gone, the operator drops the finalizer, emits `ClusterMissing`, and leaves the accounts in place so the namespace can be deleted.

## Status

`Ready` is `True` when the object is fully applied.

| Condition | Meaning |
|---|---|
| `Applied` | The required statements have been executed |
| `Authorized` | A tenant policy allows the object |
| `Degraded` | At least one required member failed |
| `CharacterSetDrift` | An existing schema charset or collation differs from spec |
| `GrantsDrift` | Grants could not be converged |
| `SecretSynced` | The application Secret matches the template |
| `NoMaster` | No member passed the master criteria |
| `SplitBrain` | More than one member looks like the master |
| `MasterUnavailable` | The explicit master cannot be dialed |
| `RotationInProgress` | A previous password is still retained |
| `PasswordRetentionUnsupported` | The server cannot keep two passwords |

Passwords are not written to status, events, or logs.

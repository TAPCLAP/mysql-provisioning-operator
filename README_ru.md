# mysql-provisioning-operator

[English](README.md)

Оператор подключается к уже работающим серверам MySQL и создаёт базы, учётки, права и Kubernetes Secret из custom resource. Он не устанавливает MySQL, не выбирает новый primary и не делает бэкапы.

Тенант создаёт `MySQLDatabase` и `MySQLUser` в своём namespace. Админский пароль и политика тенанта остаются в namespace оператора. `DROP DATABASE` оператор не выполняет.

## Пример

Платформа указывает оператору уже работающую пару MySQL и разрешает namespace `payments` создавать имена с префиксом `payments_`. Тенант просит схему и учётку. Оператор создаёт их на найденном мастере и записывает Secret рядом с приложением. Пароль генерируется и в custom resource не хранится.

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

Оператор выбирает member, который доступен на запись и у которого не настроена репликация. Второй member получает схему и учётку через binlog. Secret `payments-app` в namespace `payments` получает `USERNAME`, `PASSWORD`, `PRIMARY_HOST` и `REPLICA_HOST`. Удаление объекта базы схему в MySQL не удаляет. Удаление пользователя удаляет учётку.

Те же объекты с пояснением каждого поля — в [документации](docs/usage.ru.md).

## Документация

- [Как пользоваться оператором](docs/usage.ru.md)

## Установка

В кластере уже должен быть cert-manager. Validating webhook отклоняет неверные объекты (`failurePolicy: Fail`).

`mysql-provisioning-operator` содержит CRD, контроллер и validating webhook. Этого чарта достаточно. `mysql-provisioning-operator-crds` — те же CRD без оператора; его ставят первым, только если CRD ведут отдельно. При последующем `helm upgrade` Helm не обновляет CRD из каталога `crds/` этого чарта.

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

`MySQLCluster` и `MySQLTenant` создаются в `mysql-provisioning-system`. В заметках опубликованного релиза те же команды указаны для registry, который собрал эту версию.

## Разработка

```bash
make test                 # unit-тесты и envtest, без Docker
make test-integration     # контроллеры против настоящего MySQL в контейнере
make lint
```

Нужен Go 1.25. Остальные цели перечисляет `make help`.

## Лицензия

Apache License 2.0.

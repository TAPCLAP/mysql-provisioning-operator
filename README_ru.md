# mysql-provisioning-operator

[English](README.md)

Оператор подключается к уже работающим серверам MySQL и создаёт базы, учётки, права и Kubernetes Secret из custom resource. Он не устанавливает MySQL, не выбирает новый primary и не делает бэкапы.

Тенант создаёт `MySQLDatabase` и `MySQLUser` в своём namespace. Админский пароль и политика тенанта остаются в namespace оператора. `DROP DATABASE` оператор не выполняет.

## Документация

- [Как пользоваться оператором](docs/usage.ru.md)

## Установка

В кластере уже должен быть cert-manager. Validating webhook отклоняет неверные объекты (`failurePolicy: Fail`).

`mysql-provisioning-operator` содержит CRD, контроллер и validating webhook. Этого чарта достаточно. `mysql-provisioning-operator-crds` — те же CRD без оператора; его ставят первым, только если CRD ведут отдельно. При последующем `helm upgrade` Helm не обновляет CRD из каталога `crds/` этого чарта.

```bash
helm install mysql-provisioning-operator-crds \
  oci://ghcr.io/tapclap/charts/mysql-provisioning-operator-crds \
  --version 0.0.6

helm install mysql-provisioning-operator \
  oci://ghcr.io/tapclap/charts/mysql-provisioning-operator \
  --version 0.0.6 \
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

# mysql-provisioning-operator

[English](README.md)

Оператор подключается к уже работающим серверам MySQL и создаёт базы, учётки, права и Kubernetes Secret из custom resource. Он не устанавливает MySQL, не выбирает новый primary и не делает бэкапы.

Тенант создаёт `MySQLDatabase` и `MySQLUser` в своём namespace. Админский пароль и политика тенанта остаются в namespace оператора. `DROP DATABASE` оператор не выполняет.

## Документация

- [Как пользоваться оператором](docs/usage.ru.md)

## Установка

В кластере уже должен быть cert-manager. Validating webhook отклоняет неверные объекты (`failurePolicy: Fail`).

```bash
helm install mysql-provisioning-operator \
  oci://ghcr.io/tapclap/charts/mysql-provisioning-operator \
  --version 0.0.1 \
  --namespace mysql-provisioning-system \
  --create-namespace
```

`MySQLCluster` и `MySQLTenant` создаются в `mysql-provisioning-system`. В заметках опубликованного релиза та же команда указана для registry, который собрал эту версию.

## Разработка

```bash
make test                 # unit-тесты и envtest, без Docker
make test-integration     # контроллеры против настоящего MySQL в контейнере
make lint
```

Нужен Go 1.25. Остальные цели перечисляет `make help`.

## Лицензия

Apache License 2.0.

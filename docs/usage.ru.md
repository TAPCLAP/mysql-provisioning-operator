# Как пользоваться оператором

[English](usage.md)

Группа API: `mysql-provisioning.tapclap.com/v1alpha1`.

Оператор приводит к spec четыре ресурса. Объекты платформы живут в namespace оператора (namespace Helm-релиза, обычно `mysql-provisioning-system`). Объекты тенанта лежат рядом с приложением.

| Ресурс | Кто пишет | Что делает |
|---|---|---|
| `MySQLCluster` | платформа | Адреса, админский Secret, TLS и способ выбора мастера |
| `MySQLTenant` | платформа | Из каких namespace можно создавать какие имена, hosts, target и привилегии |
| `MySQLDatabase` | тенант | `CREATE DATABASE` на выбранных members |
| `MySQLUser` | тенант | Учётка, права, пароль и Secret в том же namespace |

Тенант ссылается на кластер только по имени и не видит админский Secret.

## Установка

cert-manager должен стоять до чарта. У webhook `failurePolicy: Fail`, поэтому без сертификата создание и изменение этих объектов блокируется.

```bash
helm install mysql-provisioning-operator \
  oci://ghcr.io/tapclap/charts/mysql-provisioning-operator \
  --version 0.0.1 \
  --namespace mysql-provisioning-system \
  --create-namespace
```

Чарт ставит CRD, контроллер и validating webhook. `spec.tls` у `MySQLCluster` — отдельная настройка: так оператор сам подключается к MySQL.

## Зарегистрировать кластер

Админский Secret создаётся в namespace оператора. Ключи: `username` и `password`.

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

Для подключения по TLS нужен второй Secret. `ca.crt` — PEM-bundle, он добавляется к системному хранилищу. Необязательный клиентский сертификат — это `tls.crt` и `tls.key` в `spec.tls.clientSecret`.

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

`tls.mode` по умолчанию `Disabled`: обычный TCP. `Required` проверяет сертификат сервера. `SkipVerify` шифрует соединение и сертификат не проверяет. `caSecret` допустим только вместе с `Required`. `serverName` — имя для проверки и SNI; если оно пустое, берётся host каждого member.

`privilegeReplication` говорит, едут ли `CREATE DATABASE` и `CREATE USER` через binlog:

- `Replicated` (по умолчанию): выполнить оператор на мастере, реплики получат его сами. Это обычный режим. `target: All` в этом режиме отклоняется.
- `Local`: каждый member обновляется отдельно. На этой сессии оператор ставит `sql_log_bin=0`, чтобы тот же оператор не записался в binlog ещё раз.

`defaultTarget` используется, когда у базы или пользователя не задан `spec.target`. По умолчанию это `Master`.

Дождитесь готовности кластера:

```bash
kubectl get mysqlcluster -n mysql-provisioning-system
```

`status.master` — member, на котором выполнятся операторы с `target: Master`. `status.members` показывает доступность, `server_id` и какие критерии прошёл каждый узел.

### Как выбирается мастер

`master.mode: Detected` на каждом reconcile опрашивает все members. Включённые критерии складываются через И. Хотя бы один критерий должен быть включён. Пропущенный критерий считается включённым:

- `readOnlyOff`: `@@global.read_only = 0`
- `superReadOnlyOff`: `@@global.super_read_only = 0`
- `noReplicationConfigured`: на узле нет настроенного источника репликации. Остановленный поток реплики при заполненном `SOURCE_HOST` или `MASTER_HOST` критерий не проходит. У primary в Group Replication каналы есть, поэтому этот критерий выключают или ставят `Explicit`.

Ноль совпадений — условие `NoMaster`. Больше одного — `SplitBrain`. В обоих случаях DDL в этом цикле не выполняется.

`master.mode: Explicit` берёт единственный member с `master: true` и критерии для выбора не использует. Ровно один member должен быть помечен. В режиме `Detected` поле `master: true` запрещено. Недоступный явный мастер — это `MasterUnavailable`, DDL тоже пропускается. В status всё равно пишется, прошёл бы этот узел текущие критерии.

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

Явный мастер меняется только вместе с правкой `MySQLCluster`. Со старого узла ничего не удаляется.

### Куда создаётся база и пользователь

`spec.target` — это `Master` или `All`. Пустое значение берёт `defaultTarget` кластера. Политика тенанта может сузить разрешённые значения.

- `Master`: выполнить на выбранном мастере с включённым binlog. При `privilegeReplication: Replicated` реплики получат оператор сами. После смены мастера следующий цикл делает `CREATE ... IF NOT EXISTS` на новом мастере. Уже созданные копии остаются.
- `All`: выполнить на каждом member с `sql_log_bin=0`. Недоступный member оставляет объект в `Degraded`; следующий цикл создаёт объект только там, где его ещё нет. Новый member получает уже существующие объекты с `All`. Объекты с `Master` попадают на него только если он стал мастером.

Переход `Master` → `All` досоздаёт недостающие копии. Обратный переход их не удаляет.

## Разрешить тенанта

`MySQLTenant` тоже только в namespace оператора. Один namespace может входить в несколько политик, разрешения складываются. Нет подходящей политики — отказ. Webhook проверяет это на `kubectl apply`, контроллер повторяет проверку перед любым SQL.

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

Проверки базы и пользователя:

- namespace объекта есть в `spec.namespaces`;
- `spec.cluster` есть в `access`;
- имя базы совпадает с `databases.patterns`, имя пользователя — с `users.patterns`;
- каждый `grants[].database` совпадает с этими паттернами баз, поэтому выдать права на чужую схему нельзя;
- привилегии — подмножество allow-list;
- hosts — подмножество `hosts`;
- `target` входит в `targets`;
- Secret создаётся только в namespace самого объекта.

`type` — это `Glob` (по умолчанию, `*` и `?`) или `Regex` (Go RE2, совпадение на всю строку). Длина паттерна не больше 256 символов. Пустые `hosts` или `targets` у записи access ничего не разрешают: перечислите то, что должно быть разрешено.

`requireTLS: Required` отклоняет пользователя с `spec.requireTLS: false`. `Optional` допускает оба варианта.

Квоты считаются по живым объектам в namespace тенанта. Незаданная квота означает «без ограничения». Если хотя бы у одной подходящей политики квота не задана, ограничение не действует. Иначе берётся наибольший из заданных пределов.

Эти привилегии и схемы запрещены, даже если allow-list широкий:

- схемы `mysql`, `sys`, `information_schema`, `performance_schema`;
- привилегии `SUPER`, `FILE`, `SHUTDOWN`, `RELOAD`, `BINLOG_ADMIN`, `SYSTEM_USER`, `CONNECTION_ADMIN`, `GRANT OPTION`, `CREATE USER`, `ALL`, `ALL PRIVILEGES`.

`status.orphanDatabases` перечисляет схемы, которые подходят под паттерны тенанта и для которых нет объекта `MySQLDatabase`. Это только список. Оператор эти схемы не удаляет.

## Создать базу

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

`name` — идентификатор MySQL не длиннее 64 символов, поле неизменяемое. `characterSet` и `collation` тоже неизменяемые и применяются только в `CREATE DATABASE`. По умолчанию это `utf8mb3` и `utf8mb3_general_ci`. Если схема уже есть и её charset или collation отличаются, оператор её не меняет и ставит `CharacterSetDrift`. `ALTER DATABASE` нет.

`status.appliedOn` — members, где схема уже есть.

Удаление объекта, namespace или самого оператора схему не удаляет. Финалайзера и флага «всё же удалить» нет. Объект удаляется сразу, оператор пишет событие `DatabaseRetained`. Повторное создание объекта подхватывает существующую схему.

## Создать пользователя

Пароля в custom resource нет. Если в Secret ещё нет ключа `PASSWORD`, оператор генерирует пароль и записывает его туда. В status попадают хеш и срок retention, не сам пароль.

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

`username` неизменяемый и не длиннее 32 символов. Каждый host — отдельная учётка. Пустой `hosts` означает `%`. `authPlugin` — `caching_sha2_password` или `mysql_native_password`. Если сервер отвергает plugin, ошибка пишется в status. Тихого перехода на другой plugin нет.

`requireTLS: true` — это `REQUIRE SSL`. `false` — `REQUIRE NONE`. Смена флага делает `ALTER USER` и пароль не ротирует.

Права сверяются с `SHOW GRANTS`: недостающие выдаются, лишние снимаются. `USAGE` на `*.*` игнорируется. Grant ждёт, пока указанная база появится.

`maxUserConnections` и `maxQueriesPerHour` — необязательные ограничения учётки.

### Шаблон Secret

Шаблоны — это Go `text/template` и урезанный Sprig. Функций `env`, `expandenv` и `getHostByName` нет. В шаблон попадает только этот пользователь: `Username`, `Password`, `PreviousPassword`, `Hosts`, `Plugin`, `Cluster`, `Master`, `Members`, `Databases`. Чужие Secret из шаблона не читаются.

`PASSWORD` и `PASSWORD_PREVIOUS` зарезервированы. Оператор записывает их после рендера, поэтому шаблон не может подменить пароль. `entries` повторяет ключ и значение для каждого member кластера. `forEach` поддерживает только `members`. Имя ключа должно совпадать с `[A-Za-z0-9_.-]+`.

Secret создаётся в том же namespace, что и `MySQLUser`, с owner reference. Оператор не watch'ит Secret. Ручная правка `PASSWORD` и момент, когда старый пароль пора снять, подхватываются следующим reconcile: что наступит раньше — минута, срок retention или `rotateEvery`. Изменение spec `MySQLUser` приходит сразу.

### Ротация пароля

MySQL хранит не больше двух паролей. Оператор делает `ALTER USER ... RETAIN CURRENT PASSWORD`, затем `DISCARD OLD PASSWORD`. Мгновенной замены пароля нет.

`spec.password.retention` — сколько старый пароль ещё подходит. По умолчанию `24h`, минимум `5m`. Пока окно открыто, в Secret лежат `PASSWORD` (новый) и `PASSWORD_PREVIOUS` (старый). Срок снятия записан на Secret аннотацией `mysql-provisioning.tapclap.com/previous-discard-after` и копируется в `status.password.retainedUntil`. Аннотация переживает потерю status.

Ротация начинается по одной из двух причин:

- выросло `spec.password.generation`. Оператор ротирует один раз на это значение и записывает его в `status.password.generation`. Так удобно ротировать из GitOps;
- прошёл `spec.password.rotateEvery`. Пустое поле значит «только вручную». Интервал должен быть длиннее `retention`: новая ротация не стартует, пока предыдущий пароль ещё нужно держать (`RotationInProgress`).

Ручная правка ключа `PASSWORD` тоже ротация: если сервер умеет, текущий пароль сохраняется. Повторная запись того значения, которое оператор уже положил, ротацию не запускает. Если сервер старше MySQL 8.0.14, ротация не начинается (`PasswordRetentionUnsupported`), текущий пароль остаётся.

### Удаление пользователя

У объекта есть финалайзер `mysql-provisioning.tapclap.com/user-cleanup`. Удаление делает `DROP USER` для каждой управляемой пары `user@host` на тех же целях, куда учётка создавалась, и удаляет Secret. Пока MySQL недоступен, финалайзер держит объект. Если самого `MySQLCluster` уже нет, оператор снимает финалайзер, пишет `ClusterMissing` и оставляет учётки, чтобы namespace можно было удалить.

## Status

`Ready` равен `True`, когда объект полностью применён.

| Условие | Смысл |
|---|---|
| `Applied` | Нужные операторы выполнены |
| `Authorized` | Политика тенанта разрешает объект |
| `Degraded` | Хотя бы один нужный member не ответил |
| `CharacterSetDrift` | Charset или collation уже существующей схемы отличается от spec |
| `GrantsDrift` | Права не удалось привести к spec |
| `SecretSynced` | Secret приложения совпадает с шаблоном |
| `NoMaster` | Ни один member не прошёл критерии мастера |
| `SplitBrain` | На мастера похоже больше одного member |
| `MasterUnavailable` | Явный мастер недоступен |
| `RotationInProgress` | Предыдущий пароль ещё действует |
| `PasswordRetentionUnsupported` | Сервер не умеет хранить два пароля |

Пароли не пишутся в status, события и логи.

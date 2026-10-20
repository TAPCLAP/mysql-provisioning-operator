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

## Пример

Это самый короткий путь, который создаёт схему, учётку и Secret. Поля по отдельности разобраны в разделах ниже.

Админский Secret и кластер лежат в namespace оператора. `master.mode: Detected` выбирает member, который доступен на запись и у которого не настроена репликация. `privilegeReplication: Replicated` значит, что `CREATE DATABASE` и `CREATE USER` выполняются на этом мастере и доезжают до `db1-2` через binlog.

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
```

`MySQLTenant` тоже создаётся в namespace оператора. Он разрешает namespace `payments` создавать на кластере `db1` базы и пользователей с именами `payments_*`, с перечисленными привилегиями и только с `target: Master`.

```yaml
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
```

Тенант создаёт схему и учётку в своём namespace. Кластер указан по имени, без пароля. Оператор генерирует пароль и заполняет Secret `payments-app`. `.Master` — выбранный мастер. `.Replica` — первый member в `spec.members`, который этим мастером не является. Если мастер — `db1-1`, то `.Replica` — `db1-2`.

```yaml
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

Удаление `MySQLDatabase` оставляет `payments_main` в MySQL. Удаление `MySQLUser` выполняет `DROP USER` и удаляет Secret.

## Установка

cert-manager должен стоять до чарта. У webhook `failurePolicy: Fail`, поэтому без сертификата создание и изменение этих объектов блокируется.

`mysql-provisioning-operator` содержит CRD, контроллер и validating webhook. Этого чарта достаточно. `mysql-provisioning-operator-crds` содержит те же CRD и больше ничего; его ставят первым, только если CRD ведут отдельно. Последующий upgrade чарта CRD не меняет CRD, которые Helm уже применил из его каталога `crds/`.

```bash
helm install mysql-provisioning-operator-crds \
  oci://ghcr.io/tapclap/charts/mysql-provisioning-operator-crds \
  --version 0.0.11

helm install mysql-provisioning-operator \
  oci://ghcr.io/tapclap/charts/mysql-provisioning-operator \
  --version 0.0.11 \
  --namespace mysql-provisioning-system \
  --create-namespace
```

`spec.tls` у `MySQLCluster` — отдельная настройка: так оператор сам подключается к MySQL.

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

Для подключения по TLS положите CA как PEM-bundle. `caSecret` читает Secret, `caConfigMap` читает ConfigMap. Поле `key` задаёт ключ, если данные лежат не в `ca.crt`. Можно указать только один из двух источников. Bundle добавляется к системному хранилищу. Необязательный клиентский сертификат — это `tls.crt` и `tls.key` в `spec.tls.clientSecret`.

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

`tls.mode` по умолчанию `Disabled`: обычный TCP. `Required` проверяет сертификат сервера. `SkipVerify` шифрует соединение и сертификат не проверяет. `caSecret` и `caConfigMap` допустимы только вместе с `Required`. `serverName` — имя для проверки и SNI; если оно пустое, берётся host каждого member.

```yaml
tls:
  mode: Required
  caConfigMap:
    name: db1-ca
    key: ca.pem
```

`privilegeReplication` говорит, едут ли `CREATE DATABASE` и `CREATE USER` через binlog:

- `Replicated` (по умолчанию): выполнить оператор на мастере, реплики получат его сами. Это обычный режим. `target: All` в этом режиме отклоняется.
- `Local`: каждый member обновляется отдельно. На этой сессии оператор ставит `sql_log_bin=0`, чтобы тот же оператор не записался в binlog ещё раз.

`defaultTarget` используется, когда у базы или пользователя не задан `spec.target`. По умолчанию это `Master`.

У member можно задать `attributes` — набор строк. Оператор копирует их в шаблон секрета и в `status.members` и `status.master` у `MySQLDatabase` и `MySQLUser`. Ключ совпадает с `^[A-Za-z][A-Za-z0-9]{0,63}$`, поэтому в шаблоне его читают как `{{ .Item.Attributes.publicHost }}`. Значение — не длиннее 253 символов. У одного member не больше 16 атрибутов.

```yaml
members:
  - name: db1-1
    host: db1-1.internal
    attributes:
      publicHost: db.example.com
      zone: a
```

Дождитесь готовности кластера:

```bash
kubectl get mysqlcluster -n mysql-provisioning-system
```

`status.master` — member, на котором выполнятся операторы с `target: Master`. `status.members` показывает доступность, `server_id` и какие критерии прошёл каждый узел.

### Как выбирается мастер

`master.mode: Detected` на каждом reconcile опрашивает все members и оставляет тот, который проходит `spec.master.criteria`.

`spec.master.criteria` необязателен. Каждое поле — булево, включённые проверки складываются через И. Пропущенное поле по умолчанию `true`, поэтому пустой объект `criteria` или его отсутствие включает все три проверки. `false` выключает только эту проверку. Все три `false` отклоняются: хотя бы одна проверка должна остаться включённой. Недоступный member не проходит никогда.

```yaml
spec:
  master:
    mode: Detected
    criteria:
      readOnlyOff: true
      superReadOnlyOff: true
      noReplicationConfigured: true
```

| Поле | По умолчанию | Узел проходит, если |
|---|---|---|
| `readOnlyOff` | `true` | `@@global.read_only = 0` |
| `superReadOnlyOff` | `true` | `@@global.super_read_only = 0` |
| `noReplicationConfigured` | `true` | На узле нет настроенного источника репликации |

`noReplicationConfigured` читает `performance_schema.replication_connection_configuration`. Если эту таблицу прочитать нельзя, оператор пробует `SHOW REPLICA STATUS`, затем `SHOW SLAVE STATUS`. Остановленный поток реплики при заполненном `SOURCE_HOST` или `MASTER_HOST` всё равно считается настроенной репликацией. Если оператор не смог понять, настроена ли репликация, узел эту проверку не проходит. У primary в Group Replication каналы есть, поэтому это поле выключают или ставят `master.mode: Explicit`.

Ноль совпадений — условие `NoMaster`. Больше одного — `SplitBrain`. В обоих случаях DDL в этом цикле не выполняется. `status.members[].matchesCriteria` записывает результат этих проверок для каждого member.

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
- привилегии `SUPER`, `FILE`, `SHUTDOWN`, `RELOAD`, `BINLOG_ADMIN`, `SYSTEM_USER`, `CONNECTION_ADMIN`, `GRANT OPTION`, `CREATE USER`.

`ALL` разрешена только на именованную базу, которая совпадает с паттернами тенанта, и только если в `privileges` этой политики есть `ALL`. На `*.*` и на схемы `mysql`, `sys`, `information_schema`, `performance_schema` её выдать нельзя.

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

`status.appliedOn` — имена members, где схема уже есть. При `target: Master` в этом списке мастер: оператор пишет туда, а на реплику схема приезжает репликацией. `status.members` содержит каждый member кластера с host, port и attributes. `status.memberNames` — эти имена через запятую, и `kubectl get` печатает эту строку в колонке Members. При `target: All` оба списка содержат только members, куда схема применена. `status.master` — выбранный мастер: имя, host, port и attributes. Колонка Master показывает его имя. Если мастер не выбран, колонка пустая.

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
      REPLICA_HOST: "{{ .Replica.Host }}"
      DSN: "{{ .Username }}:{{ .Password }}@tcp({{ .Master.Host }}:{{ .Master.Port }})/payments_main"
    entries:
      - forEach: members
        key: "{{ .Item.Name | upper }}_HOST"
        value: "{{ .Item.Host }}"
```

`username` неизменяемый и не длиннее 32 символов. Каждый host — отдельная учётка. Пустой `hosts` означает `%`. `authPlugin` — `caching_sha2_password` или `mysql_native_password`. Если сервер отвергает plugin, ошибка пишется в status. Тихого перехода на другой plugin нет.

`usernameSuffix` дописывает к `username` случайные символы из `a-z0-9`. `length` по умолчанию 8. Префикс вместе с суффиксом не длиннее 32 символов. Оператор выбирает имя учётки один раз, на первом reconcile с пустым `status.username`, и записывает его туда. В MySQL и в шаблоне Secret поле `.Username` берётся оттуда. Без `usernameSuffix` записанное имя равно `username`.

Если добавить `usernameSuffix` к пользователю, у которого `status.username` уже заполнен, или потом поменять `length`, учётка не переименовывается. Учётка в MySQL, Secret и `.Username` остаются прежними. Пользователь, созданный до появления этого поля, на первом reconcile этой версии оператора получает `status.username`, равный `username`, поэтому суффикс, добавленный после такого reconcile, ни на что не влияет.

Суффикс применяется, только если он уже указан в том reconcile, который впервые заполняет пустой `status.username`. Тогда оператор создаёт учётку с именем `username` плюс случайный суффикс. Старую учётку, созданную под одним `username`, он не удаляет. `DROP USER` выполняется только при удалении `MySQLUser` и только для имени из `status.username`.

```yaml
username: user
usernameSuffix:
  length: 8
```

`requireTLS: true` — это `REQUIRE SSL`. `false` — `REQUIRE NONE`. Смена флага делает `ALTER USER` и пароль не ротирует.

Права сверяются с `SHOW GRANTS`: недостающие выдаются, лишние снимаются. `USAGE` на `*.*` игнорируется. Grant ждёт, пока указанная база появится.

`maxUserConnections` и `maxQueriesPerHour` — необязательные ограничения учётки.

`status.appliedOn` — имена members, куда учётка применена. При `target: Master` в этом списке мастер. `status.members` содержит каждый member кластера с host, port и attributes. `status.memberNames` — эти имена через запятую, и `kubectl get` печатает эту строку в колонке Members. При `target: All` оба списка содержат только members, куда учётка применена. `status.master` — выбранный мастер: имя, host, port и attributes. Колонка Master показывает его имя. Если мастер не выбран, колонка пустая.

### Шаблон Secret

Шаблоны — это Go `text/template` и урезанный Sprig. Функций `env`, `expandenv` и `getHostByName` нет. Чужие Secret из шаблона не читаются. Доступны только эти поля:

| Поле | Тип | Значение |
|---|---|---|
| `.Username` | строка | Имя учётки в MySQL. Это `spec.username` или `spec.username` плюс случайный суффикс из `status.username` |
| `.Password` | строка | Текущий пароль |
| `.PreviousPassword` | строка | Предыдущий пароль или пустая строка, если его нет |
| `.Hosts` | список строк | `spec.hosts`. Пустой spec даёт один элемент, `%` |
| `.Plugin` | строка | `spec.authPlugin` |
| `.Cluster` | строка | `spec.cluster` |
| `.Databases` | список строк | Имена баз из `spec.grants`, в порядке spec |
| `.Master` | объект | Выбранный мастер. Пустой, если мастер не выбран |
| `.Replica` | объект | Один member, который не выбран мастером: первый такой в `spec.members`. Пустой, если мастер не выбран или мастер — единственный member |
| `.Members` | список объектов | Все members кластера, в порядке `spec.members` |
| `.Item` | объект | Текущий member внутри `entries[].forEach`. Вне `entries` пустой |

У `.Master`, `.Replica`, `.Members` и `.Item` одни и те же поля:

| Поле | Тип | Значение |
|---|---|---|
| `.Name` | строка | `MySQLCluster.spec.members[].name` |
| `.Host` | строка | Host member |
| `.Port` | число | Порт member. `3306`, если в кластере порт не задан |
| `.Index` | число | Позиция в `spec.members`, начиная с 1 |
| `.Attributes` | набор строк | `MySQLCluster.spec.members[].attributes`. Пустой, если у member их нет. Отсутствующий ключ ломает рендер |

`PASSWORD` и `PASSWORD_PREVIOUS` зарезервированы. Оператор записывает их после рендера, поэтому шаблон не может подменить пароль. `entries` повторяет ключ и значение для каждого member кластера. `forEach` поддерживает только `members`, и `.Item` — это этот member. Имя ключа должно совпадать с `[A-Za-z0-9_.-]+`. Пустое значение в Secret не записывается.

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

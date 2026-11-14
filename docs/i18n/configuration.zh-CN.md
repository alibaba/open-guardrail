# 配置与存储

首次本地体验请先阅读[快速开始](../getting-started.md)。本文介绍持久存储、配置优先级和远程部署。

以下命令假定已在源码目录执行 `make build`。使用解压后的原生二进制包时，将 `./bin/open-guardrail` 替换为 `./open-guardrail`。

## 配置

配置依次覆盖：内置默认值 → 显式指定的 YAML 文件 → 环境变量 → 命令行参数。显式设置为空的环境变量也会覆盖文件中的值，并参与校验。

| 设置 | YAML | 环境变量 | 默认值 |
| --- | --- | --- | --- |
| HTTP 监听地址 | `listen` | `OPEN_GUARDRAIL_LISTEN` | `127.0.0.1:8080` |
| 对外 HTTPS 源 | `public_url` | `OPEN_GUARDRAIL_PUBLIC_URL` | 空，表示直接回环访问模式 |
| SQLite 目录 | `data_dir` | `OPEN_GUARDRAIL_DATA_DIR` | `./data` |
| 数据库后端 | `database.driver` | `OPEN_GUARDRAIL_DATABASE_DRIVER` | `sqlite` |
| PostgreSQL 数据库 DSN | `database.url` | `DATABASE_URL` | 空 |
| 数据库连接池大小 | `database.max_connections` | — | `10` |
| 结构迁移模式 | `database.migrations` | `OPEN_GUARDRAIL_DATABASE_MIGRATIONS` | `auto` |

`--listen` 和 `--data-dir` 覆盖对应设置。使用 `DATABASE_URL` 时必须显式选择 `postgres`。未知 YAML 字段会导致校验失败；配置文件必须恰好包含一个 YAML 文档，大小不超过 1 MiB。日志和 API 错误不会包含凭据或原始驱动错误。

监听非回环地址时，必须显式配置 HTTPS `public_url`（或 `OPEN_GUARDRAIL_PUBLIC_URL`），并置于可信 TLS 反向代理之后；不能直接暴露未加密的后端。默认回环地址不需要代理。

`public_url` 必须是 HTTPS 源，不能包含路径、末尾斜杠、用户信息、查询参数或片段。客户端使用非标准端口时，也要包含端口，例如 `https://guardrail.example.com:8443`。配置对外源后，即使后端监听回环地址，也必须使用远程引导令牌流程。

### 结构迁移模式

`auto` 会连接存储，在打开 HTTP 监听端口前应用内置结构迁移。`external` 要求提前准备结构及其初始化版本，服务不会创建表。结构版本缺失或与当前版本不同都会导致启动失败。

两种模式的就绪检查都要求数据库结构为当前版本。另见[运行时存储设计](../architecture.md#storage)。

### HTTPS 代理后的 Docker 部署

完整 Docker Compose 方案包括 TLS、私有后端网络、持久化 SQLite 和首个管理员初始化，见 [Docker 部署](../docker.md)。镜像使用 UID / GID `65532:65532`，在私有网络内监听 `0.0.0.0:8080`，数据保存在 `/data`。必须提供 `OPEN_GUARDRAIL_PUBLIC_URL`，镜像没有内置的对外源。

## 数据库选择

公开发行版支持 SQLite 和 PostgreSQL。SQLite 面向本地评估、个人使用及单服务实例；PostgreSQL 面向共享部署。所有受支持驱动都必须暴露相同的 API、状态机和错误语义，并通过共同的一致性测试套件。

当前模型在各预览版驱动间统一了身份、Policy、Run 过期、快照 Submission、Evidence 和 Decision 语义。SQLite 在本地验证；PostgreSQL 后端测试需要一次性测试实例。

公开能力响应仅报告当前适配器的支持级别和已经验证的多实例保证，不透露驱动名称或连接细节。当前所有适配器均报告 `multiInstance: false`。在未来的业务不变量通过相应一致性验证之前，不承诺共享存储已具备多实例运行能力。

### PostgreSQL 配置

使用现有 PostgreSQL 数据库，为同一服务配置：

```sh
export OPEN_GUARDRAIL_DATABASE_DRIVER=postgres
export DATABASE_URL='postgres://USER:PASSWORD@HOST:5432/DATABASE?sslmode=verify-full'
./bin/open-guardrail serve
```

以上 DSN 是占位示例。实际凭据应通过部署环境的密钥机制提供，不能提交到仓库。YAML 示例见 [`deploy/config.example.yaml`](../../deploy/config.example.yaml)：

```sh
./bin/open-guardrail serve --config /etc/open-guardrail/config.yaml
```

PostgreSQL 不依赖 Docker，直接安装和托管服务使用相同 API。连接或迁移失败会终止启动，服务不会自动回退到 SQLite。修改配置**不会**迁移已有数据。

远程连接 PostgreSQL 时，应配置 TLS 并验证证书，例如以上 `sslmode=verify-full` 设置。服务目前仍处于开发预览阶段。

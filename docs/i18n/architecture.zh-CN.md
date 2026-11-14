# 运行时架构

本文描述当前已经存在的代码。目标产品决策记录在 [ADR 0001](../adr/0001-product-baseline.md)，实现顺序见[仓库中的中文开发路线图](development-roadmap.zh-CN.md)（贡献者文档）。

面向调用方的模型和阅读路径见[核心概念](../concepts.md)及[文档目录](../README.md)。[README 架构图](../../README.md#target-architecture)包含规划中的组件；[快速开始流程图](../getting-started.md#evaluation-flow)展示已经实现的评估闭环。

核心评估模型不依赖特定场景。调用方提供 Scene、策略和 JSON 评估上下文，评估器提交检查和证据，调用方使用记录下来的决策。生产交付与恢复是首批参考集成场景。

## 运行时

`cmd/open-guardrail` 将执行委托给 `internal/servercmd`，由其解析启动参数并创建感知进程信号的上下文。`internal/config` 加载严格校验的 YAML 和环境配置。`internal/bootstrap` 连接存储，按配置模式应用或验证迁移，然后打开 HTTP 监听端口。`internal/adapters/httpapi` 实现生成的标准库 HTTP 服务端接口，提供诊断、身份 API、OpenAPI 和内置资源。`cmd/guardrailctl` 委托给 `internal/ctlcmd`，后者只通过 HTTP 通信。

`api/openapi/openapi.yaml` 是公开 HTTP 模型、路由接口和 Go 客户端的来源。生成代码会提交到仓库，并检查是否与契约发生漂移。生成路由不替代授权和完整请求校验：HTTP 适配器仍负责源、CSRF、凭据、请求体大小和未知字段检查。请求体必须是 UTF-8 JSON 对象，每一层嵌套的键均不得重复；包括转义后代表同一个键的写法，都会在应用命令执行前被拒绝。最外层 HTTP 中间件为每个响应生成不透明的跟踪标识；错误响应重复该标识，不返回内部错误细节。

存储初始化截止时间为 30 秒，就绪查询为 2 秒。收到 SIGINT / SIGTERM 后，服务允许 10 秒排空 HTTP 请求，再关闭存储。只有数据库结构准备就绪后，才打开监听端口。

## 存储

应用恰好选择一个后端。`internal/adapters/storage/sqlite` 使用纯 Go 驱动、WAL、外键和有界的繁忙等待超时；`internal/adapters/storage/postgres` 通过 database/sql 使用 pgx，并限制连接池大小。`internal/adapters/storage` 管理生命周期和从 `migrations/sqlite` 或 `migrations/postgres` 嵌入的 Goose 迁移。编译进服务的扩展通过 `storage/driver` 注册连接工厂、迁移文件系统、方言和冲突处理。共享仓储依赖这些操作，不直接依赖某个扩展或其第三方驱动。

内置持久化基础设施位于 `internal/adapters/storage`。显式驱动注册表选择后端，记录迁移方言、支持级别及当前多实例保证，并为启动和现有数据库维护提供独立连接入口。根包负责迁移 / 就绪检查，并实现共享身份和 Spec 仓储 SQL；后端子包负责数据库专属连接及方言行为。可移植的仓储 SQL 保持共享，只有行为存在差异时才隔离后端操作。仓储和事务接口归属于消费它们的应用用例；领域与应用包不能导入存储适配器。

初始结构创建全部 15 张业务表，包括身份、Policy、Revision、Run、Submission、Evidence 和限定范围的凭据。最终结果和决策保存在 Run 字段上。各数据库各有一个初始化脚本，版本一致。服务会拒绝结构版本不同的数据库。

配置 `database.migrations: auto` 时，启动会安装内置结构；使用 `external` 时，结构和初始化版本必须预先存在。见[数据库初始化](../configuration.md#schema-migration-mode)。

当前预览版每个数据库只支持一个服务实例。引入相应功能时，需要验证 PostgreSQL 多实例协调及 SQLite 业务写入竞争。单个 SQLite 连接可以减少本地竞争，但未来业务正确性仍须由事务、条件写入和约束保证。因此，公开能力响应对所有当前适配器都报告 `multiInstance: false`。它只暴露支持级别和经过验证的运行边界，不暴露所选驱动、连接串、文件路径或数据库拓扑。

已接受的 v0.1 目标保留后端可配置性：SQLite 是本地 / 单实例部署的官方后端，PostgreSQL 是共享 / 多实例部署的官方目标后端。两者必须通过驱动专属实现与共同一致性套件保持相同领域行为。其他驱动使用同一组端口接口，只有通过套件后才能进入兼容性承诺范围。任何驱动选择都不能静默回退到 SQLite。

## 明确的实现范围

管理 UI 是内嵌静态页面，不需要前端构建。未来的组合和 Provider 目录目前只有设计 README，不定义受支持的 Go API，也不增加运行时行为，见[包布局](../extensions.md)。当前范围包括本地管理员引导和会话，见[管理员指南](../local-administrator.md)。

Scene 设置保存在 `scene`，Policy 元数据保存在 `policy`。可编辑草稿和已发布内容都保存在 `policy_revision`。`internal/domain/spec` 负责文档校验、规范 JSON 和摘要。草稿可以在校验前保存；发布会校验并冻结修订。激活是独立的比较并交换更新。草稿编辑以最后一次写入生效的语义更新同一修订行。键、约束和初始化见 [ADR 0002](../adr/0002-persistence-model.md)。

`internal/application/spec.Service` 校验和规范化文档，并为 Scene 初始化和修订发布准备请求身份。HTTP 处理器调用这个服务。存储端口接受准备好的命令，保持 Project / Scene / Policy 加锁顺序、已审阅草稿的精确比较、幂等重放检查和原子审计写入。文档校验不再需要持有数据库写锁；发布时间仍通过延迟调用的应用时钟，在取得必要持久化锁后采样。

运行时 API 根据 `sceneKey` 和任意 JSON 对象 `changeSet` 创建 Run。机器角色是签发模板；每个密钥持久化完整有效权限范围，端点按范围授权。限定 Project 范围的 `RUN_ORCHESTRATOR` 密钥获得 `runs:create`、`runs:read` 和 `submission-credential:rotate`。可信集成密钥可以额外获得 `runs:create-with-revision`，以选择上游系统已经冻结的不可变修订。普通编排器密钥通过精确资源绑定选择修订，未绑定资源时使用 Scene 默认激活修订。独立的 `POLICY_PUBLISHER` 密钥获得 `policies:create` 和 `revisions:publish`，不能创建 Run 或编辑管理员草稿。

选中的策略修订和 Scene TTL，与 `policyId`、`policyRevisionId`、Spec 摘要及规范化 ChangeSet 一起冻结在 Run 中。Run 从 OPEN 开始，创建时只返回一次明文提交凭据，持久化仅保存其摘要。Run 处于 OPEN 时，Project 密钥可以轮换提交凭据。匹配的 Run 凭据携带基线编号提交完整快照；中间快照推进基线，最终快照原子地产生一个 Result 和 Decision，并将 Run 转为 FINISHED。超期未完成的 Run 转为 EXPIRED，已完成 Run 保持终态。Policy Revision 包含不可变 YAML 和规范 Spec，因此 Run 不复制这些内容。

身份应用层负责真正的初始化和登录用例，以及 Project 密钥有效期和状态转换规则。密钥创建默认有效期为 90 天；未显式修改时，轮换保留现有过期时间。管理员会话与密钥签发、按版本检查的轮换或幂等撤销及对应审计写入，在同一身份事务中再次校验。元数据分页不包含秘密或已存储哈希。认证在数据库读取后采样时间，因此连接池等待不会延长凭据寿命。当前引导逻辑和 HTTP 处理器仍依赖本地身份服务。目录 README 中所述的公开企业组合和可替换身份仍在规划。

该服务未内置内部平台依赖、Provider 凭据、私有端点或遥测。公开 API 由 `api/openapi/openapi.yaml` 描述。

## 构建选择

公开模块路径为 `github.com/alibaba/open-guardrail`，与[规范源码仓库](https://github.com/alibaba/open-guardrail)一致。模块身份不意味着规划中的服务端组合或 Provider 接口已经成为受支持的 Go API。

当前发布矩阵包括 Linux / macOS 的 amd64 / arm64。两个命令均关闭 CGO 构建。共享后端集成测试基线为 PostgreSQL 17。Docker 是可选打包工具，解压后的二进制可以直接在宿主机运行。

代码使用 Go 1.27.4；`/livez` 检查进程存活，最小化的 `/readyz` 检查依赖就绪。匿名版本 / 能力发现不包含提交哈希和依赖详情。

## 请求执行预算

HTTP 处理器获得 10 秒上下文截止时间，位于服务端 15 秒套接字读写超时之内。仓储调用把上下文传递给 `database/sql`，限制连接池和行锁等待；客户端更早的取消或截止时间会保留。

取消是协作式的：处理器不会在分离的 goroutine 中继续运行，写入仍保持原有原子事务及幂等重试行为。存储取消映射为现有的不可用响应；写入结果不确定时，调用方应使用相同幂等键重试。

## 持续集成

仓库工作流同时针对一次性 PostgreSQL 和 SQLite 运行共享测试，并检查前端、Python 集成、生成的 API 客户端、依赖清单及发布归档。源码目录中的工作流为 `.github/workflows/ci.yaml`。原生归档包含[开发指南](../development.md)和[发布验证](../releases.md)，但不包含仅供源码使用的 CI 配置。

部署环境特定的运行器设置属于集成平台，位于公开源码发行内容之外。

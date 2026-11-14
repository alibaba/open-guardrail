# guardrailctl 命令行工具

这是访问现有 OpenGuardrail 服务的 HTTP CLI。首次 Run 的完整操作见[快速开始](../getting-started.md)。从源码根目录运行示例，并把 `guardrailctl` 放到 `PATH`；也可以在 `make build` 后使用 `./bin/guardrailctl`。原生归档中使用 `./guardrailctl`。

## 命令与凭据

当前支持 `status`、`capabilities`、`version`、`bootstrap init`、`auth login`、`logout`、`whoami`、`create-key`、`list-keys`、`rotate-key`、`revoke-key`、`scene apply` 和 `binding list`、`binding put`、`binding delete`，以及 `run create`、`run get`、`run wait`、`run revision`、`run submission`、`run list`、`run rotate-credential`、`run submit`。

`run get` 报告冻结的截止时间、惰性过期和决策有效期；`run wait` 等待 Run 进入 `FINISHED` 或 `EXPIRED`。Execution Claims 仍在规划，当前没有 checkpoint 模型。

| 命令组 | 凭据 |
| --- | --- |
| `status`、`version`、`capabilities` | 公开诊断，不需要凭据 |
| `bootstrap init` | 初始化首个管理员，远程模式要求私有引导令牌 |
| `auth`、`scene apply`、`binding` | 管理员会话，由 `auth login` 创建 |
| `run create/get/wait/list/revision/submission/rotate-credential` | 具有端点有效权限范围的 Project 密钥 |
| `run submit` | 绑定该 Run 的提交凭据 |

## Scene 与管理员会话

凭据文件保存在源码或解压归档之外。需要隔离会话时，创建私有目录并登录：

```sh
umask 077
guardrail_credentials_dir=$(mktemp -d)
export OPEN_GUARDRAIL_SESSION_FILE="$guardrail_credentials_dir/admin-session.json"
guardrailctl auth login --username admin
guardrailctl auth create-key \
  --result-file "$guardrail_credentials_dir/orchestrator-key.json"
```

已有 Project 密钥时，使用其现有私有目录，不必再次签发。以下 Run 示例假定目录包含 `orchestrator-key.json`。

使用管理员 CLI 会话创建 Scene、保存 Spec 并发布：

```sh
guardrailctl scene apply --key recovery --name Recovery \
  --spec ./examples/policies/recovery.yaml --publish
```

这个命令可以重复执行：复用匹配的 Scene、跳过未变化的草稿，未变化的发布返回现有激活修订。名称不同或激活冲突时失败，不覆盖其他管理员的工作。新 Scene 必须带 `--publish`；已有 Scene 可以省略此参数，保存不完整草稿供后续编辑。草稿保存成功但发布失败时，错误会说明部分成功，便于操作者在重试前检查状态。Project 密钥不能创建、保存或发布 Scene。

管理员命令使用 `auth login` 保存的会话，默认位于操作系统用户配置目录；以上示例选择独立私有文件。CI 或隔离凭据可以通过 `OPEN_GUARDRAIL_SESSION_FILE` 或 `--session-file PATH` 覆盖路径。通过 `OPEN_GUARDRAIL_URL` 或 `--server URL` 选择服务源。

机器密钥默认有效期 90 天，`auth create-key --expires-in 24h` 可以创建更短有效期的凭据。`auth list-keys` 返回有界元数据分页，包含 ID、版本、过期和撤销时间，不包含秘密。通过 `--page-size` 控制页大小，把返回的 `nextAfter` 作为 `--after` 继续翻页。

`auth rotate-key --key-id ID --expected-version VERSION --result-file NEW_FILE` 原子替换有效秘密，保留授权；未指定 `--expires-in` 时也保留过期时间。旧秘密随即失效，已有输出文件不会被覆盖。过时版本会失败，不会再次轮换。`auth revoke-key --key-id ID` 可以安全重复执行。凭据生命周期及响应不确定时的恢复见[管理员指南](../local-administrator.md#project-machine-keys)。

## Run 与 Submission

使用 `auth create-key` 产生的默认 `RUN_ORCHESTRATOR` Project 密钥文件创建和查询 Run。该角色固定获得 `runs:create`、`runs:read` 和 `submission-credential:rotate`。策略发布自动化使用 `auth create-key --role POLICY_PUBLISHER`，其密钥获得 `policies:create` 和 `revisions:publish`，不能操作 Run。

需要选择已经冻结的不可变修订时，HTTP 集成使用通过 `--scope runs:create-with-revision` 创建的独立密钥。普通编排器密钥不能提供 `policyRevisionRef`。省略资源时，`run create` 选择 Scene 默认激活修订；传入 `--resources FILE` 可使用可信 `{kind,id}` JSON 数组进行资源绑定选择。活跃 / 固定版本、分页和重试见[资源绑定示例](../../examples/resource-bindings/README.md)。创建操作把只返回一次的 Run 提交凭据保存到新的私有文件：

```sh
guardrailctl run create --key-file "$guardrail_credentials_dir/orchestrator-key.json" \
  --scene recovery --change-set ./change-set.json \
  --operation-id pipeline-123 \
  --submission-credential-file "$guardrail_credentials_dir/run-credential.json"
guardrailctl run get --key-file "$guardrail_credentials_dir/orchestrator-key.json" run_EXAMPLE
guardrailctl run wait --key-file "$guardrail_credentials_dir/orchestrator-key.json" run_EXAMPLE
guardrailctl run revision --key-file "$guardrail_credentials_dir/orchestrator-key.json" run_EXAMPLE
guardrailctl run submission --key-file "$guardrail_credentials_dir/orchestrator-key.json" \
  --submission-id sub_EXAMPLE run_EXAMPLE
guardrailctl run list --key-file "$guardrail_credentials_dir/orchestrator-key.json" \
  --scene recovery --status OPEN --limit 20
guardrailctl run submit --submission-credential-file "$guardrail_credentials_dir/run-credential.json" \
  --submission ./submission.json --operation-id submit-123 run_EXAMPLE
```

`run wait` 轮询权威 Run，直到进入 `FINISHED` 或 `EXPIRED`，再输出最终 Run 文档。默认等待到 Run 自身截止时间；`--timeout` 可以设置更早的限制，`--interval` 控制轮询周期。退出码 0 表示 `FINISHED`；`EXPIRED` 会打印状态并以非零退出，因为截止前没有最终 Submission。

传输错误、`5xx` 和 `429` 会在截止前重试，认证及其他客户端错误立即返回。获知 Run 截止时间前，连接重试最多持续 30 秒；`429` 的 `Retry-After` 大于 `--interval` 时，在截止时间内遵从它。等待只观察 Run；调用方执行受保护操作前，仍须核验 Decision、有效期和证据。

只有 Run 提交凭据可以向绑定 Run 提交。凭据丢失时，在 Run 仍为 `OPEN` 的情况下，用 Project 密钥替换：

```sh
guardrailctl run rotate-credential --key-file "$guardrail_credentials_dir/orchestrator-key.json" \
  --submission-credential-file "$guardrail_credentials_dir/replacement-run-credential.json" run_EXAMPLE
```

CLI 创建权限为 `0600` 的凭据文件，从不打印原始令牌。`run list` 按创建时间倒序返回有界轻量摘要，其中包含 `changeSetDigest`，省略可能很大的 `changeSet`；需要完整不可变输入时使用 `run get`。将响应的 `nextCursor` 原样传入 `--cursor` 获取下一页；游标是不透明值，调用方不能自行构造。

`--operation-id` 会成为 HTTP 的 `Idempotency-Key`，自动化应提供稳定值用于重试。交互使用可省略，CLI 会生成操作 ID，并在不确定的传输错误中附上它。重试 `run create` 可以找回已有 Run，但无法找回只返回一次的明文凭据。先检查现有凭据文件；如果未保存，使用 Project 密钥及新的输出路径执行 `run rotate-credential`。

## 传输与兼容性

HTTP 命令使用从 `api/openapi/openapi.yaml` 生成的 Go 客户端。CLI 不打开服务数据库，也不内嵌另一套决策引擎。人员、Agent 和 CD 集成使用相同命令语义。凭据文件和传输行为保留在 CLI 中；策略校验、证据接受和状态转换保留在服务端。

管理命令在发送凭据或改变状态前，协商 `v1alpha1` 及服务端声明的 CLI 版本窗口。版本不兼容时诊断命令仍然可用，操作者可以继续检查 `status`、`version` 和 `capabilities`。

## 自动化退出码

进程退出码是稳定的自动化契约：

| 退出码 | 含义 |
| ---: | --- |
| 0 | 成功 |
| 1 | 本地或非预期失败 |
| 2 | 命令、参数或 URL 无效，或传输方式不安全 |
| 3 | 认证或授权被拒绝 |
| 4 | 服务连接或响应传输失败 |
| 5 | 服务端拒绝请求 |
| 6 | API 或 CLI 版本不兼容 |

生成客户端由统一的有界传输层包装，拒绝重定向、限制响应体大小，只向预期源附带凭据。不回显服务端错误消息；失败仅暴露支持排查所需的稳定错误码和跟踪标识。

Jenkins、GitHub Actions 和 Argo 接入方案位于[集成目录](../../integrations/README.md)，平台专属脚本或工作流配置也放在那里。共享业务命令应与匹配的服务端 API 一起加入本 CLI，不应为每个 CD 平台另写一套 CLI 实现。

当前支持的初始化和身份命令见[管理员初始化与 CLI 会话](../local-administrator.md)。

## 审计与诊断导出

使用管理员会话执行 `guardrailctl audit list|export`，使用具备 `runs:read` 权限的 Project 密钥执行 `guardrailctl run report`。导出范围有界，省略业务正文。筛选、分页、权限和隐私边界见[审计与报告指南](audit-and-reports.zh-CN.md)。

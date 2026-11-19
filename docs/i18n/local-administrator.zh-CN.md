# 本地管理员指南

OpenGuardrail 提供一个本地管理员账号，完整操作流程见[快速开始](../getting-started.md)。

## 首次使用

1. 启动 `open-guardrail serve`，打开 <http://127.0.0.1:8080>。
2. 设置用户名和密码，并确认密码。密码至少 6 个字符，最多 128 个 UTF-8 字节。
3. 完成初始化后，自动进入管理页面。

重启时使用同一数据目录，以保留 SQLite 中的账号。管理页面支持英文和简体中文。

免令牌初始化要求：直接通过回环连接访问仅监听回环地址的服务，且未配置 `public_url`。它面向可信本地机器。使用反向代理或配置了对外源时，即使后端是回环地址，也须走[远程初始化流程](#remote-access)。

## 登录与修改密码

使用管理员账号登录，会话在八小时后过期。密码失败会记录审计，并统一返回未授权响应。失败或未完成的尝试会消耗下文说明的共享临时退避预算。本地管理员仅应在可信机器上使用，登录端点不能暴露给不可信客户端。

修改密码需要当前密码，并会使所有管理员会话退出。之后使用新密码重新登录。

### 忘记用户名或密码

在服务主机上，使用与服务相同的二进制版本和数据目录运行：

```sh
open-guardrail admin reset --data-dir /path/to/persistent/data
```

确认重置，选择用户名（回车可保留），再输入两次新密码，不需要旧密码。已经配置的部署使用 `--config /path/to/config.yaml` 以及服务的数据库环境变量。

重置会退出所有管理员会话，但保留 Scene、Spec 和 Project 密钥。它要求已有且完成初始化的数据库，因此必须保护主机与数据库访问权限。

## Project 机器密钥

只有服务需要调用 OpenGuardrail 时，才创建机器密钥。管理员签发时选择角色：

- `RUN_ORCHESTRATOR`（默认）：固定有效权限范围是 `runs:create`、`runs:read` 和 `submission-credential:rotate`。它不能提交结果，提交必须使用 Run 凭据。默认只能使用 Scene 默认激活修订。可信集成需要重放其他系统已经冻结的修订时，可以额外授予 `runs:create-with-revision`。
- `POLICY_PUBLISHER`：固定有效权限范围是 `policies:create` 和 `revisions:publish`。不能编辑草稿、激活修订、初始化 Scene 或创建 Run。

角色是签发模板，每个密钥持久化完整的有效权限范围，运行时授权检查这些范围，而不是角色名。两种角色都限定在一个 Project 内，均不授予管理员访问权，也不批准受保护操作。

密钥只展示一次，应安全保存，不能提交到仓库。新密钥默认 90 天后过期。`--expires-in` 接受 `1m` 到 `8760h`（365 天）范围内、可表达为整数秒的时长。密钥具有稳定的非秘密 ID 和秘密代次版本；列表只返回元数据，不返回明文密钥或已存储哈希。撤销是幂等操作，会立即阻止后续认证，但不会取消已经通过认证的请求。

轮换要求当前版本，原子替换秘密并保留 ID、角色和权限范围。省略 `--expires-in` 保留现有过期时间，也可以指定新的有界有效期。已过期或撤销的密钥不能轮换或恢复，应签发新密钥。同一版本的并发轮换只有一个成功。轮换提交后旧秘密失效。需要逐步切换使用方时，先创建独立密钥，更新使用方，再撤销旧密钥。

创建响应丢失时，不要自动重试签发，因为密钥可能已创建。轮换结果不确定时，先检查 `auth list-keys`。如果版本已推进但秘密丢失，可以用新版本和新的输出文件再次轮换，或撤销密钥。重放旧版本返回 `KEY_STATE_CONFLICT`，不会再签发秘密。

## 可选 CLI 用法

不用浏览器时，可以在本地初始化：

```sh
guardrailctl bootstrap init --username admin
```

密码提示是隐藏的。初始化不会自动登录 CLI。会话和密钥应放在源码或解压归档以外的私有目录。以下示例创建新目录和 CLI 会话：

```sh
umask 077
guardrail_credentials_dir=$(mktemp -d)
export OPEN_GUARDRAIL_SESSION_FILE="$guardrail_credentials_dir/admin-session.json"
guardrailctl auth login --username admin
guardrailctl auth whoami
guardrailctl auth create-key \
  --result-file "$guardrail_credentials_dir/orchestrator-key.json"

# Optional short-lived key and a bounded metadata page.
guardrailctl auth create-key --expires-in 24h \
  --result-file "$guardrail_credentials_dir/short-lived-key.json"
guardrailctl auth list-keys --page-size 20
# Follow nextAfter with --after CURSOR if another page is present.

# Copy KEY_ID and KEY_VERSION from the metadata list. Use a new output path.
guardrailctl auth rotate-key --key-id "$KEY_ID" --expected-version "$KEY_VERSION" \
  --result-file "$guardrail_credentials_dir/replacement-key.json"
guardrailctl auth revoke-key --key-id "$KEY_ID"

# Issue a separate integration key that may select an explicit immutable Revision.
guardrailctl auth create-key --scope runs:create-with-revision \
  --result-file "$guardrail_credentials_dir/revision-orchestrator-key.json"

# Issue a separate Policy Publisher key for trusted automation.
guardrailctl auth create-key --role POLICY_PUBLISHER \
  --result-file "$guardrail_credentials_dir/policy-publisher-key.json"
guardrailctl auth logout
```

仅运行实际需要的可选签发或轮换示例。仍有使用方依赖密钥文件时，应保留私有目录。丢弃密钥文件前，在管理员会话有效期间按 ID 撤销密钥。退出登录或删除本地文件不会撤销 Project 密钥。另一个终端复用目录前，先把 `guardrail_credentials_dir` 设置为现有私有目录。

管理员会话默认保存在操作系统用户配置目录，上述示例选择独立私有文件。默认 OpenGuardrail 目录权限为 `0700`，会话文件为 `0600`。默认过期会话在下次登录前自动删除；有效会话不会被静默覆盖。默认文件无效或属于另一服务时，CLI 会报告精确路径，供操作者对相应服务退出登录，或有意识地移开文件。会话与密钥文件应像密码一样保护。

对应 HTTP 接口是 `GET/POST /api/v1alpha1/auth/api-keys`、`DELETE /api/v1alpha1/auth/api-keys/{keyId}` 和 `POST /api/v1alpha1/auth/api-keys/{keyId}/rotation`。轮换接受 `expectedVersion` 和可选 `expiresInSeconds`；创建接受可选 `expiresInSeconds`。所有接口均要求有效管理员会话，变更保留管理侧 Origin / Host / CSRF 检查。机器密钥不能管理凭据。

CI、测试或隔离凭据通过 `OPEN_GUARDRAIL_SESSION_FILE` 或 `--session-file PATH` 覆盖默认位置。显式凭据输出文件从不覆盖已有文件。

不同服务地址通过 `OPEN_GUARDRAIL_URL` 或 `--server URL` 指定。自动化可使用 `--password-stdin` 传入密码，不能把密码写入命令参数。

## 远程访问

远程访问要求 HTTPS `public_url`（或 `OPEN_GUARDRAIL_PUBLIC_URL`）及负责 TLS 终止的反向代理。代理必须保留外部 Host、剥离不可信转发头、阻止直接访问 HTTP 后端，并将管理界面限制为可信操作者。本地管理员的共享临时退避不替代可信反向代理上按来源实施的限流。不能通过隧道、端口转发或代理暴露默认本地模式。

远程初始化还要求服务主机上的 `bootstrap.token` 文件，启动日志会显示路径。在初始化页面输入令牌，或使用 CLI 初始化时传 `--token-file PATH`。文件应私密保存，权限为 `0600`。

令牌一小时后过期。未完成初始化时，重启服务可更新过期令牌；初始化完成后，令牌不再有效。

配置项见[配置与存储](../configuration.md)，API 细节见 [OpenAPI 规范](../../api/openapi/openapi.yaml)。

## 临时登录退避

本地管理员在各服务实例和尝试的用户名之间，共享持久化的尝试预算。五次失败或未完成尝试后，进一步尝试暂时返回 HTTP 429 `RATE_LIMITED`。退避从 1 秒开始，随被接受但失败的尝试翻倍，上限 30 秒。冷却期内被拒绝的请求不延长冷却时间。响应提供保守的 `Retry-After: 30` 指引。成功登录或主机管理员重置会清空预算；15 分钟没有被接受的尝试时，重置突发计数。账号不会永久锁定。

每个服务实例最多同时允许两次密码验证。哈希计算在身份写事务之外完成，签发会话前再次检查密码快照，因此密码重置会使使用旧哈希的进行中登录失效。取消可能消耗一次尝试而没有签发会话，应在冷却后重试。部署还应在可信反向代理上按来源限流；不会使用转发的客户端 IP 头绕过共享管理员预算。

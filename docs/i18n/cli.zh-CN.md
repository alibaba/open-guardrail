# guardrailctl 命令行工具

页面、CLI 和管理 API 均可直接使用；只有评估提交需要绑定对应 Run 的提交凭据。详见[访问指南](access.zh-CN.md)。通过 --server URL 或 OPEN_GUARDRAIL_URL 选择服务。

| 命令 | 凭据 |
| --- | --- |
| `status`、`version`、`capabilities` | 无需凭据 |
| `scene apply`、`binding list/put/delete`、`audit list/export` | 无需凭据 |
| `run create/get/wait/list/report/revision/submission/rotate-credential` | 无需凭据 |
| `run submit` | 绑定对应 Run 的提交凭据 |

```sh
guardrailctl scene apply --key recovery --name Recovery   --spec ./examples/policies/recovery.yaml --publish
umask 077
guardrail_credentials_dir=$(mktemp -d)
```

Scene apply 复用匹配的 Scene、保存发生变化的草稿，并可选择发布。新 Scene 必须带 --publish；已有 Scene 可以保存不完整草稿。遇到冲突时失败，不覆盖其他编辑。Run 凭据文件必须是权限为 0600 的普通文件。

## Run 与 Submission

Run 的创建、查询和凭据轮换均可直接使用。省略资源时，run create 选择 Scene 默认激活修订；--resources FILE 可通过可信资源引用选择绑定的 Policy/Revision。创建操作将只返回一次的 Run 提交凭据保存到新的私有文件：

```sh
guardrailctl run create \
  --scene recovery --change-set ./change-set.json \
  --operation-id pipeline-123 \
  --submission-credential-file "$guardrail_credentials_dir/run-credential.json"
guardrailctl run get run_EXAMPLE
guardrailctl run wait run_EXAMPLE
guardrailctl run revision run_EXAMPLE
guardrailctl run submission \
  --submission-id sub_EXAMPLE run_EXAMPLE
guardrailctl run list \
  --scene recovery --status OPEN --limit 20
guardrailctl run submit --submission-credential-file "$guardrail_credentials_dir/run-credential.json" \
  --submission ./submission.json --operation-id submit-123 run_EXAMPLE
```

`run wait` 轮询权威 Run，直到进入 `FINISHED` 或 `EXPIRED`，再输出最终 Run 文档。默认等待到 Run 自身截止时间；`--timeout` 可以设置更早的限制，`--interval` 控制轮询周期。退出码 0 表示 `FINISHED`；`EXPIRED` 会打印状态并以非零退出，因为截止前没有最终 Submission。

传输错误、`5xx` 和 `429` 会在截止前重试，认证及其他客户端错误立即返回。获知 Run 截止时间前，连接重试最多持续 30 秒；`429` 的 `Retry-After` 大于 `--interval` 时，在截止时间内遵从它。等待只观察 Run；调用方执行受保护操作前，仍须核验 Decision、有效期和证据。

```sh
guardrailctl run rotate-credential \
  --submission-credential-file "$guardrail_credentials_dir/replacement-run-credential.json" run_EXAMPLE
```

CLI 创建权限为 `0600` 的凭据文件，从不打印原始令牌。`run list` 按创建时间倒序返回有界轻量摘要，其中包含 `changeSetDigest`，省略可能很大的 `changeSet`；需要完整不可变输入时使用 `run get`。将响应的 `nextCursor` 原样传入 `--cursor` 获取下一页；游标是不透明值，调用方不能自行构造。

## 按业务编号关联 Run

```sh
guardrailctl run create \
  --scene release-check --business-id 'release-42/batch-1/attempt-1' \
  --change-set change-set.json --operation-id release-42-create \
  --submission-credential-file "$guardrail_credentials_dir/release-42.json"
guardrailctl run list \
  --scene release-check --business-id 'release-42/batch-1/attempt-1'
```

`run create --business-id ID` 和 `run list --business-id ID` 可将发布单的一次评估与 Run 关联。配合 `--scene` 查询，避免混淆不同场景的相同编号。控制台 Runs 页面同样支持精确筛选、列表与详情展示，筛选条件保存在页面链接中。

业务编号为 1–128 个 UTF-8 字节，不含首尾空白或 ASCII 控制字符；传参时使用引号，不能自行截断或规范化。省略保留原有行为，显式空值无效。同项目、同场景中，同编号和同内容重放原 Run，不同内容返回 `BUSINESS_ID_CONFLICT`。完成或过期后编号仍保留，新评估使用新编号。

重放时 CLI 返回退出码 5，并提示原 Run ID，不重新签发凭据。检查原凭据文件；只有 OPEN Run 丢失凭据时才轮换，轮换使旧凭据失效。排障报告包含业务编号，分享前检查其中的业务信息。

[发布单关联示例](../../examples/business-id/README.zh-CN.md)提供真实服务验证和接入约束。

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

直接访问及 Run 提交凭据的使用方式见[访问指南](access.zh-CN.md)。

## 审计与诊断导出

直接查询默认项目中的审计事件或导出 Run 报告：

```sh
guardrailctl audit list --limit 20
guardrailctl run report --output ./run-report.json run_EXAMPLE
```

导出结果有大小限制，并省略业务正文。筛选、分页及隐私边界见[审计与报告指南](audit-and-reports.zh-CN.md)。

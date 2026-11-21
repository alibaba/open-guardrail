# API 概览

页面、CLI 和管理 API 均可直接使用，无需账号或登录；提交评估仍需绑定 Run 的提交凭据。详见[访问指南](access.zh-CN.md)。

带版本的 [OpenAPI 文档](../../api/openapi/openapi.yaml)是契约来源。本文汇总当前路由，请求结构和完整路由列表以契约为准。

领域模型见[核心概念](../concepts.md)，可执行的完整流程见[快速开始](../getting-started.md)。

## API 接口与凭据

管理路由使用 `/api/v1alpha1`，机器评估路由使用 `/runtime/v1alpha1`。策略文档的 `guardrail.open/v1alpha1` 是 GuardrailSpec 格式标识，不是 HTTP 路径。

| 操作 | 访问方式 |
| --- | --- |
| Scene、Spec、绑定、Run 管理和审计 | 直接访问默认 workspace/project |
| Run 评估提交 | 使用绑定对应 Run 的提交凭据 |
| 内部操作者 API | 由内部传输适配器和各 Policy 的维护人权限校验 |

## CLI 传输

`open-guardrail serve` 启动服务，`guardrailctl` 调用已经运行的服务：

```sh
guardrailctl --server http://127.0.0.1:8080 status
guardrailctl --server https://guardrail.example.com version
```

CLI 也接受 `OPEN_GUARDRAIL_URL` 和 `OPEN_GUARDRAIL_TOKEN`。它使用根据已发布 OpenAPI 契约生成的客户端；管理命令在发送凭据或改变状态前，验证服务端的 API / CLI 兼容窗口。稳定的自动化退出码见 [CLI 指南](../../cmd/guardrailctl/README.md)。

诊断命令仅在 HTTPS 上传递可选令牌，且从不跟随重定向；公开诊断端点不要求令牌。身份和 Run 命令使用显式凭据文件，仅对字面回环 IP 地址允许明文 HTTP。每个 HTTP 响应都包含服务端生成的 `X-Trace-ID`，错误响应以 `traceId` 重复该值，便于运维关联 CLI 失败，不暴露数据库或凭据细节。

## 当前端点摘要

| 端点 | 用途 |
| --- | --- |
| `GET /livez` | 检查进程存活，不依赖数据库可用性 |
| `GET /readyz` | 检查数据库连接，失败返回 503 |
| `GET /api/v1alpha1/meta/version` | 公开产品版本，不包含提交元数据 |
| `GET /api/v1alpha1/meta/capabilities` | API / CLI 兼容性、特性及非敏感存储运行边界 |
| `GET /openapi.yaml` | 公开 OpenAPI 契约 |
| `GET /runtime/v1alpha1/runs` | 列出默认 Project 内的 Run |
| `POST /runtime/v1alpha1/runs/{runId}/submissions` | 使用绑定 Run 的凭据提交完整中间或最终快照 |
| `GET /runtime/v1alpha1/runs/{runId}` | 查询默认 Project 内的 Run |
| `GET /runtime/v1alpha1/runs/{runId}/policy-revision` | 读取 Run 选择的不可变策略修订 |
| `GET /runtime/v1alpha1/runs/{runId}/submissions/{submissionId}` | 读取 Run 内已持久化 Submission 及其完整快照 |
| `GET /api/v1alpha1/audit-events` | 项目审计查询，支持筛选、游标分页及可选脱敏 |
| `GET /api/v1alpha1/runs/{runId}/report` | Run 诊断报告 |
| `GET /api/v1alpha1/runs` | 为调用方的只读管理视图列出 Run |
| `GET /api/v1alpha1/runs/{runId}` | 在管理视图检查单个 Run |
| `POST /api/v1alpha1/scenes/{key}:initialize` | 原子创建 Scene、默认 Policy 和修订 1 |
| `GET /api/v1alpha1/scenes/{key}/bindings` | 使用 limit / after 分页列出精确资源绑定 |
| `PUT /api/v1alpha1/scenes/{key}/bindings/{kind}/{resourceId}` | 幂等创建或替换绑定，可固定到已发布修订 |
| `DELETE /api/v1alpha1/scenes/{key}/bindings/{kind}/{resourceId}` | 删除绑定，不存在时返回 204 |
| `POST /api/v1alpha1/scenes/{key}/policies` | 创建另一个 Policy |
| `PUT /api/v1alpha1/policies/{policyId}/maintainers` | 替换非空维护者列表；内部操作者模式仅允许当前维护者写入 |
| `GET /api/v1alpha1/policies/{policyId}/draft` | 读取当前草稿修订，无草稿返回 404 |
| `PUT /api/v1alpha1/policies/{policyId}/draft` | 保存并返回草稿修订 |
| `POST /api/v1alpha1/policies/{policyId}/draft:publish` | 将已审阅草稿发布为同一个修订 |
| `POST /api/v1alpha1/policies/{policyId}/revisions` | 直接发布修订 |
| `GET /api/v1alpha1/policies/{policyId}/revisions?pageSize=20` | 分页读取修订摘要，跟随 `nextBefore`，按 ID 获取 YAML |
| `PUT /api/v1alpha1/policies/{policyId}/active-revision` | 激活修订 |
| `GET /` | 内置管理界面外壳 |

Run 创建和 Submission 都要求 `Idempotency-Key`，同一个逻辑操作应保持相同键和内容重试。读取 Run 的 `finalSubmissionId`，再通过 Submission 读取接口检查已接受证据。`FINISHED` Run 暴露 Result、Decision 和 `decisionExpiresAt`；调用方还必须核验依赖的证据及评估身份。Execution Claims 和 ActionResults 仍在规划。完整生命周期见 [Run 与 Submission](../runs.md)。

隐私边界和有界历史查询见[审计与报告导出](audit-and-reports.zh-CN.md)。

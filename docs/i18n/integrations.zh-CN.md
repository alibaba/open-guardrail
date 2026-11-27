# 参考集成

OpenGuardrail 的策略、证据和决策模型不绑定特定业务领域。本目录收录了面向 AI 编程助手（MCP）与持续交付（CD）的参考集成；其他业务场景也可以直接调用同一套 HTTP API 或 `guardrailctl` CLI，自定义 Scene、策略 Spec、评估上下文和证据来源。

**当前状态：** `MCP Proxy`（`guardrailctl mcp-proxy`）、`GitHub Actions` 发布门禁、共享 Recovery 门禁、Jenkins 适配器和 Argo Rollouts Analysis Job 均已实现并可独立复现；Argo CD 同步卡点仍在规划中。

接入前建议先阅读[快速开始](getting-started.zh-CN.md)并体验[本地 Agent 示例](../../examples/agent-demo/README.md)。模型概念见[核心概念](concepts.zh-CN.md)，命令行参数见 [CLI 指南](cli.zh-CN.md)。

## 职责分工

- **工具代理 / CD 平台侧**：负责在关键执行点拦截或暂停操作、保留构建或调用上下文、向 OpenGuardrail 核验权威决策并决定放行或阻断。
- **外部评估器 / Agent 侧**：负责按冻结的 Spec 采集实时观测数据，使用 Run 专属提交凭据上报评估快照（Submission）。
- **OpenGuardrail 服务端**：负责版本化策略管理、Run 生命周期与超时控制、结构化证据校验以及审计存证。

所有集成示例均仅依赖公开组件和模拟数据，方便任何团队在本地完整复现。

| 平台 / 场景 | 目录 | 集成状态 |
| --- | --- | --- |
| MCP stdio 客户端（Cursor / Claude Code / Cline 等） | [mcp-proxy](../../integrations/mcp-proxy/README.md) | 选择性工具护栏代理：未命中规则的普通调用直通、参数哈希防重放、Fail-Closed SQL 判定、`destructiveHint` 自动升级 |
| GitHub Actions | [github-actions](../../integrations/github-actions/README.md) | 复合 Action（`action.yml`）、3 项生产 CD 检查模板 `release-gate.yaml`、双 Job 独立复验示例与本地可跑 Demo |
| pi / DeepSeek Harness | [agent-plugins](../../integrations/agent-plugins/README.md) | 内部预览：原生工具与 Skill，用于隔离目录中的文件清理场景 |
| Jenkins | [jenkins](../../integrations/jenkins/README.md) | 模拟流水线示例（`Jenkinsfile`）、构建上下文适配器及加密凭据交接的外部 Agent 客户端 |
| Argo Rollouts | [argo](../../integrations/argo/README.md) | 内联 Analysis Job、加密凭据交接、分层失败诊断与可重复的 8 用例验收 |
| Argo CD | [argo](../../integrations/argo/README.md) | 同步钩子（Sync hooks）规划中 |

## 相关入口

- 共用 CLI：[cmd/guardrailctl](cli.zh-CN.md)
- 场景包目录：[scenarios](../../scenarios/README.md)（pi 适配器提供统一宿主入口，新场景通过该包注册）
- 扩展与 Provider 规划：[核心与适配器职责](../extensions.md#core-and-adapter-responsibilities)（源码设计笔记位于 `extension/delivery/` 和 `internal/adapters/delivery/`，不包含在原生二进制归档中；上述 CD/MCP 集成均直接调用 HTTP API 或 CLI，不依赖规划中的服务端 Provider）

> 仓库根目录下的 `.github/workflows` 仅用于项目自身的开发测试与发布构建，与本目录面向接入方的部署配方相互独立。

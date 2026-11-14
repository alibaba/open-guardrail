# 参考集成

OpenGuardrail 的策略、证据和决策模型可以用于任何场景。生产交付与恢复是本目录中的首批参考场景。其他领域也可使用相同 HTTP API 或 CLI，自行定义 Scene、策略、评估上下文和证据来源。

当前状态：共享 Recovery Agent 门禁、Jenkins 适配器和 Argo Rollouts Analysis Job 已实现为使用合成观测的 CD 侧演示。GitHub Actions 和 Argo CD 同步钩子仍在规划。

配置交付平台前，先阅读[快速开始](../getting-started.md)并运行[本地 Agent 示例](../../examples/agent-demo/README.md)。策略、Run 和 Submission 模型见[核心概念](../concepts.md)，命令见 [CLI 指南](../../cmd/guardrailctl/README.md)。

当前交付方案运行在 CD 侧，在显式检查点调用共同的 HTTP API 或 CLI。它们保留平台构建 / 变更身份，等待权威决策，检查失败或结果不确定时停止。

CD 平台负责部署和继续执行。[共享 Recovery 门禁](../../integrations/recovery_agent/README.md)负责 Run 创建、加密交接和 Decision 核验；外部 Agent 负责评估与 Submission。各平台适配器映射自己的构建 / 检查点身份、观测来源和凭据注入方式。需要服务端上下文查询时，应由独立的交付 Provider 承担。示例使用公开依赖和合成数据，便于每个接入方复现相同流程。

| 平台 | 目录 | 集成状态 |
| --- | --- | --- |
| pi / DeepSeek Harness | [agent-plugins](../../integrations/agent-plugins/README.md) | 内部预览：通过原生工具和 Skill 运行隔离目录中的真实文件清理示例 |
| Jenkins | [jenkins](../../integrations/jenkins/README.md) | 合成观测 Jenkinsfile、构建上下文适配器，以及使用加密交接的外部 Agent 客户端 |
| GitHub Actions | [github-actions](../../integrations/github-actions/README.md) | 规划中的部署工作流，调用共享 CLI 或 API |
| Argo Rollouts | [argo](../../integrations/argo/README.md) | 内联 Analysis Job、加密交接、分层失败诊断及可重复的八用例验收 |
| Argo CD | [argo](../../integrations/argo/README.md) | 同步钩子仍在规划 |

共享 CLI 位于 [cmd/guardrailctl](../../cmd/guardrailctl/README.md)。场景源码和可安装状态见[场景包目录](../../scenarios/README.md)；pi 适配器提供统一宿主入口，新场景通过该包注册。规划中的服务端上下文契约和可复用 Provider 实现见[核心与适配器职责](../extensions.md#core-and-adapter-responsibilities)。源码设计说明位于 `extension/delivery/` 和 `internal/adapters/delivery/`，这些仅供源码使用的目录不包含在原生归档中。CD 侧方案通过 HTTP API 或 CLI 独立使用服务，不依赖规划中的 Provider API。

项目自身的 `.github/workflows` 用于开发检查，与面向接入方的部署方案相互独立。

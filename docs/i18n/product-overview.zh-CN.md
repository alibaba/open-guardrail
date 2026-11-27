# 认识 OpenGuardrail

别只靠系统提示词祈祷 AI Agent 不出事故。**OpenGuardrail** 是一套开源的循证操作护栏，在删库、高危 MCP 工具调用和生产发布前建立强制的准入与审计关口。

## 为什么选择 OpenGuardrail？

当 AI Agent 拥有操作数据库、删除文件或推进发布的权限时，只在 Prompt 里叮嘱“操作前先检查”既不可靠也无法审计。OpenGuardrail 把**策略版本**、**各项检查结论**、**结构化证据**和**最终决策**强绑定在同一份不可篡改的 Run 记录里：

- **给人看（团队与审计）：** 每次放行或拦截依据什么策略、查了什么数据，一目了然，事后随时可追溯。
- **给程序和 Agent 看（执行门禁）：** 下游代理或发布流水线只需核验一条与当前目标强绑定、且在有效期内的权威 `ALLOW` 记录即可决定放行与否。

## 工作机制

1. **定义规则：** 用版本化的 `GuardrailSpec` YAML 声明前置检查项、通过/拦截/未知判定标准与必交证据。
2. **排查举证：** Agent 或 CI 脚本采集实时信号，持 Run 专属提交凭据上报完整评估快照。
3. **核验放行：** 全部检查为 `PASS` 时记录 `ALLOW`，任一项为 `BLOCKED`、`UNKNOWN` 或超时则一律 `DENY`；执行端核验通过后才真正执行动作。

## 按你的场景快速上手

- **保护 Cursor / Claude Code / Cline 的高危 MCP 工具：** 使用 `guardrailctl mcp-proxy`，未命中门禁规则的普通调用零开销直通，写库和破坏性调用按精确参数强制举证。详见 [MCP Proxy 接入指南](../../integrations/mcp-proxy/README.md)。
- **在 GitHub Actions 中卡点生产发布：** 直接复用复合 Action 与 3 项生产检查模板 `release-gate.yaml`。详见 [GitHub Actions 发布门禁](../../integrations/github-actions/README.md)。
- **体验 30 秒首批发布复核：** [让你的 AI 编程助手跑一遍首批检查](getting-started.zh-CN.md#跑一遍首批检查)，直观感受 Agent 如何顺着写库报错反查出“已扣款但状态仍停留在 `pending`”的订单并阻止下一批放量。
- **下载单二进制或体验插件：** 直接[下载原生二进制包](native-install.zh-CN.md)（运行无需 Go/Node/Python 环境），或在 pi / DeepSeek Harness 中体验 [Agent 插件](../plugin-trial.zh-CN.md)。

准备接入自己的工具或流水线？继续阅读[集成指南](integrations.zh-CN.md)。

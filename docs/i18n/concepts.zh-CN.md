# 核心概念

OpenGuardrail 为每次评估记录策略、评估上下文、评估结果和决策。调用方定义场景，外部评估器执行调查，调用系统决定如何使用记录下来的结论。

## 策略与评估记录

| 术语 | 含义 |
| --- | --- |
| Scene（场景） | 具有稳定键、Run 超时时间（`runTtlSeconds`）和可选默认 Policy 的场景。一个 Scene 可以包含多个 Policy。 |
| Policy（策略） | Scene 内策略的身份、归属、名称、生命周期及当前激活修订的指针。 |
| GuardrailSpec | 定义指令、Check、证据要求和决策有效期的 YAML 策略文档，文档版本为 `guardrail.open/v1alpha1`。 |
| Policy Revision（策略修订） | 处于 `DRAFT` 或 `PUBLISHED` 状态的规则内容。每个 Policy 最多有一个可编辑草稿；发布时分配修订号并冻结内容。Run 选择已发布修订。发布和激活是两个独立操作。 |
| Run | 一次独立评估，冻结 Scene、策略修订引用、Spec 摘要、调用方提供的上下文和 Run 截止时间。 |
| Evaluation context（评估上下文） | API 字段 `changeSet` 携带的任意 JSON 对象。业务字段由调用方定义，服务负责规范化并记录。 |
| Check（检查项） | 冻结的 Spec 要求完成的一项评估，具有 `PASS`、`BLOCKED` 或 `UNKNOWN` 结论及适用证据。 |
| Submission（提交） | 包含所有 Check 及其适用证据的一份完整快照。一个 Run 可以接受多个中间快照和一个最终快照。 |
| Evidence（证据） | 附属于 Check 的结构化观测，包括要求 ID、观测时间、查询摘要、结果摘要和可选引用。 |
| Result（评估结果） | 服务端校验并汇总 Check 后得出的 `PASS`、`BLOCKED` 或 `UNKNOWN`。 |
| Decision（决策） | 根据 Result 生成并存储的 `ALLOW` 或 `DENY` 结论，具有独立的有效期。 |

服务校验 Submission 是否符合冻结的 Spec，并汇总 Check 结论；服务不独立鉴证观测是否真实。集成方在应用决策之前，需要核验其依赖的证据。

## Run 状态与时间

新 Run 的状态是 `OPEN`。完整的中间 Submission 会推进基线编号。最终 Submission 将 Run 改为 `FINISHED`，并原子地产生 Result 和 Decision。未完成的 Run 在截止时间或之后被观察到时，会转为 `EXPIRED`，结果为 `UNKNOWN / DENY`。

| 时间约束 | 来源 | 含义 |
| --- | --- | --- |
| Run 截止时间（`deadlineAt`） | Scene 的 `runTtlSeconds`，创建 Run 时冻结 | 接受新 Submission 的最晚时间。中间提交和凭据轮换不会延长它。 |
| 决策有效期（`decisionExpiresAt`） | 冻结 Spec 的 `spec.validForSeconds` | Run 进入终态后，已存储 Decision 的有效期限。经过这个时间不会改写历史 Run 状态。 |

`PASS` 对应 `ALLOW`；`BLOCKED` 和 `UNKNOWN` 对应 `DENY`。调用方使用已存储的 `ALLOW` 前，还必须确认它仍有效，并且关联的是预期评估。并发和重试行为见 [Run 与 Submission](../runs.md)。

## 凭据与职责

| 凭据 | 使用者 | 用途 |
| --- | --- | --- |
| 管理员会话 | 可信人员或管理员自动化程序 | 管理 Scene、Policy、草稿、激活和 Project 密钥；查看管理视图。浏览器会话与 CLI 会话相互独立。 |
| Project 机器密钥 | 编排器或策略发布方 | 只能在一个 Project 内调用有效权限范围允许的端点。默认编排器可以创建 / 读取 Run，并轮换处于 OPEN 状态的 Run 的提交凭据。 |
| Run 提交凭据 | 外部评估器 | 只能向指定的一个 Run 提交完整快照，不能创建 / 读取 Run、读取策略修订或管理 Project 凭据。 |

机器角色是签发模板，每次请求根据有效权限范围授权。最终 Submission 使用 Run 凭据，而不是 Project 密钥。明文密钥和 Run 凭据只返回一次，必须私密保存。签发、轮换和撤销见[管理员指南](../local-administrator.md#project-machine-keys)。

## 当前范围与未来的执行控制

决策的实际应用由调用系统负责。预览版交付示例会等待当前有效的权威决策，并在继续执行之前核验已持久化的证据。这些交付示例使用合成观测。

Execution Claims、ActionResults、评估器鉴证和可替换的身份 / Provider API 均在规划中。[目标架构](../../README.md#target-architecture)包含这些未来范围；[快速开始的评估流程](../getting-started.md#evaluation-flow)展示已经实现的评估闭环。剩余工作见[仓库中的中文路线图](development-roadmap.zh-CN.md)（贡献者文档）。

返回[文档目录](../README.md)。

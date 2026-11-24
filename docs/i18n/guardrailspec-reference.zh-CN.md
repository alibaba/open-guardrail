# GuardrailSpec YAML 字段手册

页面、CLI 和管理 API 均可直接使用，无需账号或登录；提交评估仍需绑定 Run 的提交凭据。详见[访问指南](access.zh-CN.md)。

[English](../guardrailspec-reference.md) | 简体中文

GuardrailSpec 定义一类操作需要完成的检查及证据。它是一份 YAML 策略文档，
不是可执行的规则语言。外部 Agent 或集成方观察真实系统，为每个 Check 提交
结论和证据；OpenGuardrail 服务依据该 Run 冻结的 Spec 校验提交内容，但不独立
证明观测为真。执行工具需要在操作前核验权威决策、已接受的证据及当前操作。
参见[核心概念](../concepts.md)和 [Run 与提交](../runs.md)。

本文对应 `guardrail.open/v1alpha1`。当前版本接受的字段与约束以
[OpenAPI 契约](../../api/openapi/openapi.yaml)和
[Spec 校验代码](../../internal/domain/spec/document.go)为准。

## 可直接使用的完整示例

将下面内容保存为 `file-cleanup.yaml`：

```yaml
apiVersion: guardrail.open/v1alpha1
kind: GuardrailSpec
metadata:
  name: file-cleanup
spec:
  description: Check a proposed cleanup of temporary files
  instructions: Inspect the exact files and submit current observations for every check.
  validForSeconds: 60
  checks:
    - id: temporary-only
      displayName: Temporary files only
      description: Protect business files from deletion
      instructions: Confirm that every target is an existing temporary file in the intended workspace.
      criteria:
        pass: Every target is an existing temporary file in the intended workspace
        block: At least one target is a protected business file
        unknown: Target identity or current file state cannot be verified
      evidenceRequirements:
        - id: files
          description: Workspace identity, exact targets and observed file metadata
          appliesToVerdicts: [PASS, BLOCKED, UNKNOWN]
```

这份 Spec 本身不会检查或删除文件；[文件清理 Demo](../../examples/cleanup-demo/README.md)
提供了外部观测与执行逻辑，并使用一份包含两个 Check 的 Spec。
[Recovery Spec](../../examples/policies/recovery.yaml)也是一份完整的简短样例。

## 字段与约束

字段名与枚举值区分大小写。下表标为必填的字段必须出现；文本不能只有空白字符，
长度按 Unicode 字符计算。未知字段会被拒绝。

| 路径 | 必填 | 含义与约束 |
| --- | --- | --- |
| `apiVersion` | 是 | 固定为 `guardrail.open/v1alpha1`。 |
| `kind` | 是 | 固定为 `GuardrailSpec`。 |
| `metadata.name` | 是 | Spec 标识符：1–128 个小写字母、数字或连字符；以字母开头，以字母或数字结尾。 |
| `spec.description` | 是 | Spec 的用途，最多 1,024 字符。 |
| `spec.instructions` | 是 | 对评估方的整体指引，最多 8,000 字符。 |
| `spec.validForSeconds` | 否 | 终态决策的有效秒数：Run 正常完成（`FINISHED`）时从完成时起算；Run 过期（`EXPIRED`）时从逻辑截止时间起算。十进制整数，范围 `1` 至 `9007199254740991`，默认 `3600`；不能加引号或前导零。 |
| `spec.checks` | 是 | 1–20 个 Check；同一 Spec 中的 Check ID 不可重复。 |
| `spec.checks[].id` | 是 | Check 标识符，格式与 `metadata.name` 相同。 |
| `spec.checks[].displayName` | 是 | 给人看的检查名称，最多 128 字符。 |
| `spec.checks[].description` | 是 | 检查覆盖的事项，最多 1,024 字符。 |
| `spec.checks[].instructions` | 是 | 评估方如何调查该检查，最多 8,000 字符。 |
| `spec.checks[].criteria.pass` | 是 | 该检查何时可以报 `PASS`，最多 2,048 字符。 |
| `spec.checks[].criteria.block` | 是 | 该检查何时应报 `BLOCKED`，最多 2,048 字符。 |
| `spec.checks[].criteria.unknown` | 是 | 无法核实时何时报 `UNKNOWN`，最多 2,048 字符。 |
| `spec.checks[].evidenceRequirements` | 是 | 1–20 条证据要求；同一 Check 内的要求 ID 不可重复。 |
| `spec.checks[].evidenceRequirements[].id` | 是 | 证据要求标识符，格式与 `metadata.name` 相同。 |
| `spec.checks[].evidenceRequirements[].description` | 是 | 需要何种观测，最多 1,024 字符。 |
| `spec.checks[].evidenceRequirements[].appliesToVerdicts` | 是 | 从 `[PASS, BLOCKED, UNKNOWN]` 中选 1–3 个不重复的值。提交所列结论时必须提供这条证据；其他结论不能附带它。 |

标识符的完整格式为 `^[a-z]([a-z0-9-]{0,126}[a-z0-9])?$`。
`criteria` 是给评估方的文字判定标准，服务不会执行这些文字。
最终 Submission 必须覆盖全部 Check 及其适用证据：全部 `PASS` 得到
`PASS / ALLOW`；有任一 `BLOCKED` 得到 `BLOCKED / DENY`；否则有
`UNKNOWN` 则得到 `UNKNOWN / DENY`。提交格式详见 [Run 与提交](../runs.md)。

## YAML 与 Scene 的边界

## 发布与使用

启动服务并使用管理员 CLI 会话，从仓库根目录发布 YAML：

```sh
guardrailctl scene apply --key file-cleanup --name 'File cleanup' \
  --spec ./file-cleanup.yaml --publish
```

## 常见错误

| 写法 | 问题 | 正确做法 |
| --- | --- | --- |
| 在 YAML 中写 `sceneKey` 或 `runTtlSeconds` | 两者属于 Scene，不属于 Spec。 | 用 `scene apply --key` 选择 Scene；另行配置 Run 截止时间。 |
| 在 Check 中写 `requirement: SQL must have WHERE` | `requirement` 不是现有字段。 | 填写 `description`、`instructions`、三个 `criteria` 值及 `evidenceRequirements`。 |
| 只在 `criteria` 里写通过条件 | 文本不会自动检查系统或证明观测为真。 | 由外部集成采集证据，并随适用结论提交。 |
| `validForSeconds: "300"` 或 `validForSeconds: 0300` | 唯一的数字字段不能加引号或前导零。 | 使用 `validForSeconds: 300`。 |
| 拿到 `ALLOW` 就执行任意后续操作 | 决策关联特定 Run、被冻结的上下文及有效期。 | 执行前回读权威 Run 和已接受的 Submission，并核对操作身份、证据及当前状态。 |

# 创建、提交与查询 Run

Run 冻结其 Scene、策略修订 ID、Spec 摘要、Run TTL，以及调用方通过 `changeSet` 提供的 JSON 上下文。完整示例见[核心概念](../concepts.md)和[快速开始](../getting-started.md)。

## 创建与查询 Run

机器密钥需要 `runs:create` 来创建 Run，`runs:read` 来列出或读取 Run，`submission-credential:rotate` 来更换提交凭据。先[以管理员登录](../local-administrator.md#project-machine-keys)，再创建 Project 密钥。在源码或解压归档根目录运行示例，并确保 `guardrailctl` 位于 `PATH`。

将 `guardrail_credentials_dir` 设为包含 `orchestrator-key.json` 的私有目录，目录应位于源码或解压归档之外。如果在同一终端完成了管理员指南，该变量已经设置；否则，用实际私有目录路径替换以下占位值：

```sh
guardrail_credentials_dir=/absolute/path/to/private/guardrail-credentials
```

新 Run 凭据保存在这个目录。`change-set.json` 和 `submission.json` 示例见快速开始。

创建时通过资源绑定选择 Policy / Revision；资源全部未绑定时使用 Scene 默认激活修订，操作与规则见[资源绑定](../../examples/resource-bindings/README.zh-CN.md)。具有 `runs:create-with-revision` 的可信 HTTP 调用方可以通过 `policyRevisionRef` 显式选择不可变修订，普通编排器密钥不能这样做。当前 CLI 没有显式修订参数。机器调用方可以在启用集成前，通过 `GET /runtime/v1alpha1/identity` 检查有效权限范围。

```sh
guardrailctl run create --key-file "$guardrail_credentials_dir/orchestrator-key.json" \
  --scene recovery --change-set ./change-set.json \
  --operation-id create-123 \
  --submission-credential-file "$guardrail_credentials_dir/run-credential.json"
guardrailctl run get --key-file "$guardrail_credentials_dir/orchestrator-key.json" run_EXAMPLE
guardrailctl run wait --key-file "$guardrail_credentials_dir/orchestrator-key.json" run_EXAMPLE
guardrailctl run list --key-file "$guardrail_credentials_dir/orchestrator-key.json" \
  --scene recovery --status FINISHED --limit 20
```

Run 列表返回有界摘要，包含 `changeSetDigest`，不包含可能很大的 `changeSet`。使用 `run get` 或 `GET /runtime/v1alpha1/runs/{runId}` 读取单个 Run 的完整不可变 ChangeSet。

`run wait` 轮询 `GET /runtime/v1alpha1/runs/{runId}`，直到 Run 进入终态，然后输出最终文档。只有 `FINISHED` 返回退出码 0；`EXPIRED` 因截止前没有最终提交而返回非零，等待超时也返回非零。

临时传输错误、`5xx` 和 `429` 会在截止时间内重试，认证及其他客户端错误立即失败。在获知 Run 截止时间前，连接重试最多持续 30 秒。如果 `429` 的 `Retry-After` 大于 `--interval`，会在剩余等待时间内遵从它。等待不替代调用系统对 Decision、有效期和证据的核验。

## Run 重试与凭据恢复

`POST /runtime/v1alpha1/runs` 仅在首次创建时返回 Run 和明文提交凭据。CLI 将凭据写入新的私有文件。幂等重放返回 Run，不重新签发凭据。

创建或提交结果不确定时，重试保持相同操作 ID 和输入。重试前检查返回的 Run 及已保存凭据。凭据未保存时，可以在 Run 为 `OPEN` 时轮换。轮换使旧凭据失效，不延长截止时间：

```sh
guardrailctl run rotate-credential --key-file "$guardrail_credentials_dir/orchestrator-key.json" \
  --submission-credential-file "$guardrail_credentials_dir/replacement-run-credential.json" run_EXAMPLE
```

API 路径为 `POST /runtime/v1alpha1/runs/{runId}/submission-credential:rotate`。只有匹配的 Run 凭据可以提交；CLI 从不覆盖已有凭据输出文件。

轮换和 Submission 共用 Run 锁，提交在锁下再次检查凭据。最终提交会阻止轮换。轮换成功后，旧凭据不能创建或重放 Submission；Run 进入终态后，仍可以用当前凭据精确重试。

服务在取得 Run 锁后检查时间，因此连接或锁等待不会延长截止时间。截止后，新提交或轮换会使 Run 过期并返回 `RUN_EXPIRED`；对已经接受的 Submission 做精确重试，仍返回原有记录。

## 完整快照提交

每次 Submission 都包含所有 Check 结论及适用结构化证据，即使中间提交也必须是完整快照。`baseSubmissionNo` 必须匹配 Run 的最新编号。服务验证汇总结论：`BLOCKED` 优先于 `UNKNOWN`，`UNKNOWN` 优先于 `PASS`。

顶层 `summary` 必填，可以为 `""`。非空摘要必须包含非空白文本，且不超过 2048 字符。Check 摘要和 Evidence 的查询 / 结果摘要必须包含非空白文本。

以下 JSON 仅展示结构。实际使用时应填写真实观测时间和调查结论，并包含 Run 冻结 Spec 要求的每个 Check。

```json
{
  "baseSubmissionNo": 0,
  "finishRun": true,
  "verdict": "PASS",
  "summary": "Recovery checks passed",
  "checks": [{
    "checkId": "readiness",
    "verdict": "PASS",
    "summary": "Service is healthy",
    "evidence": [{
      "requirementId": "health",
      "observedAt": "2026-10-26T00:00:00Z",
      "querySummary": "Read current health metrics",
      "resultSummary": "All replicas ready"
    }]
  }]
}
```

保存为 `submission.json`，然后运行：

```sh
guardrailctl run submit --submission-credential-file "$guardrail_credentials_dir/run-credential.json" \
  --submission ./submission.json --operation-id submit-123 run_EXAMPLE
```

`finishRun: false` 保存中间快照、递增 `latestSubmissionNo`，Run 保持 `OPEN`。`finishRun: true` 保存最终快照，原子地产生一个 Result 和 Decision，使 Run 进入 `FINISHED`。相同幂等键的精确重试返回原有 Submission，请求变化则冲突。

## Run 状态与决策有效期

| 约束 | 冻结来源 | 行为 |
| --- | --- | --- |
| `deadlineAt` | 创建 Run 时 Scene 的 `runTtlSeconds` | 限制接受新 Submission 的时间，中间快照和轮换不延长截止时间 |
| `decisionExpiresAt` | 所选 Spec 的 `spec.validForSeconds` | 限制终态 Decision 的有效期；经过有效期不会改写历史 Result 或 Run 状态 |

未完成 Run 在截止时间被观察到时，会成为 `EXPIRED`，结果为 `UNKNOWN / DENY`。`FINISHED` Run 保持 `FINISHED`。过期和最终提交使用同一 Run 锁，因此只有一个终态结果能生效。即使惰性过期稍后才持久化，过期 Run 的 Decision 也从逻辑截止时间开始计时。

`PASS` 产生 `ALLOW`，`BLOCKED` 和 `UNKNOWN` 产生 `DENY`。调用系统行动前必须检查权威 Run、决策有效期、评估身份和被接受的证据。Execution Claims 和 ActionResults 仍在规划；服务不会执行部署，也不独立鉴证提交的观测。

## 读取不可变输入与已接受证据

Project 编排器无需策略管理权限，即可读取 Run 的 Revision 和 Submission：

```sh
guardrailctl run revision --key-file "$guardrail_credentials_dir/orchestrator-key.json" "$RUN_ID"
guardrailctl run submission --key-file "$guardrail_credentials_dir/orchestrator-key.json" \
  --submission-id "$SUBMISSION_ID" "$RUN_ID"
```

它们分别对应 `GET /runtime/v1alpha1/runs/{runId}/policy-revision` 和 `GET /runtime/v1alpha1/runs/{runId}/submissions/{submissionId}`。后者返回 Submission 元数据及已持久化的 `snapshot`。两个读取接口都检查权限范围和 Run 归属，Run 提交凭据不能使用。对于已完成 Run，使用其 `finalSubmissionId` 作为 `SUBMISSION_ID`。Run 元数据不包含完整证据快照。

管理页面默认显示最新接受的 Submission 及证据。管理员可以通过 `GET /api/v1alpha1/runs/{runId}/submissions?limit=20` 的元数据分页选择更早提交，再使用 `GET /api/v1alpha1/runs/{runId}/submissions/{submissionId}` 读取单个快照。将 `nextBefore` 作为下一页的 `before` 参数。选择历史不会改变 Run 的最终结果或决策，Project 密钥运行时读取接口保持不变。

## 证据限制

每条 Evidence 最多 16 KiB，每个 Submission 最多 100 条 Evidence，每个请求最多 1 MiB。文本长度按 Unicode 码点计算。具有凭据特征的内容会被拒绝，包括 JSON 摘要、嵌套对象、数组及 JSON 编码字符串中的已知凭据字段；重复 JSON 键和过度复杂的 JSON 结构也会被拒绝。

筛查会保留已接受文本及其重试身份。调用方仍必须在提交前清除秘密，因为自由文本筛查不是通用的数据防泄漏系统。管理 UI 可以列出和检查 Run，不暴露凭据，也不接受 Submission。字段及错误码见 [OpenAPI 契约](../../api/openapi/openapi.yaml)。

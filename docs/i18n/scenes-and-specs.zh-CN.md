# 场景、策略与 GuardrailSpec

逐字段 YAML 指南、完整示例和常见校验错误见 [GuardrailSpec YAML 参考](../guardrailspec-reference.md)。

Scene 具有稳定键、可配置的 Run TTL 和可选默认 Policy，一个 Scene 可以包含多个 Policy。每个 Policy 最多有一个可变草稿修订、多个不可变已发布修订和一个激活修订。服务之间使用 `policyRevisionId` 标识修订，修订号仅用于展示。统一术语见[核心概念](../concepts.md)。

## 初始化 Scene

使用管理页面或管理员 CLI 会话，在源码或解压归档的根目录运行命令。登录与凭据设置见[快速开始](../getting-started.md)。

```sh
guardrailctl scene apply --key recovery --name Recovery \
  --spec ./examples/policies/recovery.yaml --publish
```

首次应用会校验 YAML，并原子创建 Scene、默认 Policy、修订 1 及其激活记录。新 Scene 必须带 `--publish`。相同内容再次应用会复用激活修订；已有 Scene 可以不带 `--publish`，只保存默认 Policy 的草稿。

管理员也可以调用 `POST /api/v1alpha1/scenes/{key}:initialize`，提供 `name`、`defaultPolicyName`、`yaml` 和 `Idempotency-Key`。相同键和请求返回原有资源；请求内容发生变化，或 Scene 已通过另一请求初始化时，返回冲突。

## 编辑与发布

对于默认 Policy，管理页面以最后一次写入生效的语义保存草稿 YAML。发布时必须提交调用方已经审阅的精确 YAML。应用层先校验并规范化 YAML，再获取数据库写锁。随后，存储事务在 Policy 锁下将已审阅 YAML 与当前草稿比较，分配下一个修订号，把同一个草稿修订标记为 `PUBLISHED`，并写入审计记录。反复保存更新同一条 `DRAFT` 记录；草稿没有修订号和发布元数据。发布后再次保存会创建具有新业务 ID 的草稿。已发布内容不可变。

没有草稿时，控制台以只读方式显示激活策略，并提示 **No draft yet（尚无草稿）**。**Edit a copy of this version（编辑此版本的副本）** 打开可编辑的本地副本；**Save draft（保存草稿）** 持久化副本，不改变激活版本。已有草稿（包括有意保存的空草稿）直接打开供编辑。如果无法读取激活策略，控制台会先提供重新加载操作，再允许编辑副本。

已审阅 YAML 无效时，在比较草稿状态之前返回 `422 SPEC_VALIDATION_FAILED`。对于内容有效的新发布，如果草稿被另一会话替换，会返回 `409 RESOURCE_STATE_CONFLICT`，不创建或激活修订。应重新加载并审阅新内容，再次发布。精确幂等重试按下述规则返回原有修订。

激活仍是独立请求。管理页面将发布和激活串成一次操作流程，API 中两者保持独立。通过 `POST /api/v1alpha1/policies/{policyId}/revisions` 直接发布用于可信生产方，直接创建已发布修订，不修改人工草稿。

Policy 响应包含元数据和激活修订指针。通过 `GET /api/v1alpha1/policies/{policyId}/draft` 读取可编辑修订，没有草稿时返回 `404 NOT_FOUND`。保存操作返回该 `DRAFT` 修订，包括身份、YAML 和编辑元数据；发布前没有仅用于发布的字段。Policy 响应不包含规则内容或草稿字段。修订历史和 Run 选择只包含已发布修订；管理侧按修订 ID 直接读取时可以读取两种状态。结构见[持久化模型](../adr/0002-persistence-model.md)。

- `GET /api/v1alpha1/policies/{policyId}/draft`：读取当前草稿修订。
- `PUT /api/v1alpha1/policies/{policyId}/draft`：保存 YAML（允许不完整草稿），返回草稿修订。
- `POST /api/v1alpha1/policies/{policyId}/draft:publish`：携带 `Idempotency-Key`，发送 `{"yaml":"<reviewed Draft YAML>"}`，校验并发布这份精确草稿。即使草稿后来变化，精确重试仍返回原有修订；相同键配合不同 YAML 会冲突。
- `POST /api/v1alpha1/policies/{policyId}/revisions`：携带 `Idempotency-Key`，直接发布完整 YAML。
- `PUT /api/v1alpha1/policies/{policyId}/active-revision`：使用 `expectedActiveRevisionId` 进行比较并交换，激活修订。
- `PUT /api/v1alpha1/scenes/{key}/default-policy`：使用 `expectedDefaultPolicyId` 选择激活 Policy。
- `PATCH /api/v1alpha1/scenes/{key}`：修改 `runTtlSeconds`，仅影响未来 Run。

Policy 可以归档和恢复，但归档默认 Policy 前必须先更换默认策略。已发布修订和已有 Run 保持不变。归档策略不能编辑、发布、激活或用于创建 Run；其中已有草稿仍可读取。归档不删除或发布草稿；恢复 Policy 后，可以继续编辑和发布同一份草稿。

GuardrailSpec YAML 使用 `apiVersion: guardrail.open/v1alpha1` 和 `kind: GuardrailSpec`。解析器拒绝重复键、锚点、别名、未知字段和无效约束，文件上限为 64 KiB。`spec.validForSeconds` 控制最终 Decision 的有效期，Scene 的 `runTtlSeconds` 控制 Run 截止时间。另见 [OpenAPI 契约](../../api/openapi/openapi.yaml)和 [Run 生命周期](../runs.md#run-state-and-decision-validity)。

## 读取已发布历史

`GET /api/v1alpha1/policies/{policyId}/revisions?pageSize=20` 返回 `{"items": [...], "nextBefore": 123}`。条目包含修订身份、编号、摘要和发布元数据，不包含 YAML。默认每页 20 条，最多 100 条。下一页传 `before=<nextBefore>`；这个排他的修订号游标保证新修订发布时旧页仍稳定。没有 `nextBefore` 表示没有更早条目。控制台提供较新 / 较旧页切换，仅在选择某个版本时通过 `GET /api/v1alpha1/policy-revisions/{revisionId}` 获取其 YAML。

## 有效期取值范围与规范身份

`spec.validForSeconds` 接受 1 到 9007199254740991（`2^53 - 1`）之间的十进制整数，含两端。省略时，在计算规范 JSON 和 SHA-256 摘要前补入 3600。这是互操作边界，不是可配置的业务时长上限，7200 等既有值仍然有效。

固定结构只有这一个数值字段。限制为 IEEE 754 安全整数，可使 Go 和 ECMAScript 的 RFC 8785 实现保留相同的精确数值与十进制 JSON 表示。更大整数会被拒绝，不会四舍五入；字符串、小数、指数和非十进制 YAML 表示同样被拒绝。既有有效范围内 Spec 的规范字节和摘要保持不变。从已存储规范 Spec 解析有效期时也应用同一边界。已有超范围修订不会被改写，应先发布修正修订，再据此创建新 Run。

参考：[RFC 8785 第 3.1 与 3.2.2.3 节](https://www.rfc-editor.org/rfc/rfc8785.html#section-3.1)。

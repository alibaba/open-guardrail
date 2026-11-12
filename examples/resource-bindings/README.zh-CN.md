# 资源绑定操作示例

[English](README.md)

管理员通过 CLI 将资源绑定到策略，可信接入方使用 Project key 和原始资源引用创建 Run。
服务选出策略版本并冻结在该 Run 中。

## 隔离环境验收

在源码根目录执行 `make smoke-bindings`。使用原生发行归档时，在解压目录执行
`python3 examples/resource-bindings/smoke.py --binaries .`。
CLI 需要包含 `binding` 和 `run create --resources`，此前的 Alpha 产物尚未包含这些命令。

脚本启动真实的本地服务和临时 SQLite 数据库，创建私密凭据，通过 CLI 管理绑定并创建 Run。
准备非默认策略和发布版本使用已有 HTTP 接口。验收覆盖资源编码、分页、活跃与固定版本、
旧 Run 冻结、幂等重放、部分绑定拒绝、冲突拒绝、默认策略回落以及重复删除。
成功时输出 `result: PASS`、检查名称和公开对象 ID，结束后清理临时服务、数据库与凭据。
该策略选择示例无需模型账号。

## 使用已有服务

先完成[管理员登录和场景发布](../../cmd/guardrailctl/README.md#scenes-and-administrator-sessions)。
使用 `scene apply` 返回的策略 ID，或同一场景中已有的策略 ID。将凭据和输入文件保存在私密工作目录。

```sh
export OPEN_GUARDRAIL_URL=https://guardrail.example
export OPEN_GUARDRAIL_SESSION_FILE=/absolute/private/admin-session.json
policy_id=REPLACE_WITH_POLICY_ID
revision_id=REPLACE_WITH_PUBLISHED_REVISION_ID
key_file=/absolute/private/orchestrator-key.json

# 跟随策略的活跃版本，资源 ID 直接传原始值。
guardrailctl binding put --scene recovery --kind repository \
  --resource-id 'team/repository' --policy-id "$policy_id"

guardrailctl binding list --scene recovery --limit 20
# 有 nextAfter 时，将原值传给下一次调用的 --after。

# 固定到该策略下的已发布版本。
guardrailctl binding put --scene recovery --kind repository \
  --resource-id 'team/repository' --policy-id "$policy_id" \
  --revision-id "$revision_id"

umask 077
cat > resources.json <<'JSON'
[{"kind":"repository","id":"team/repository"}]
JSON
cat > change-set.json <<'JSON'
{"changeSetRef":"example-operation-1"}
JSON

guardrailctl run create --key-file "$key_file" --scene recovery \
  --change-set change-set.json --resources resources.json \
  --operation-id example-operation-1 \
  --submission-credential-file /absolute/private/new-run-credential.json
```

Run 输出包含 `policyId`、`policyRevisionId`，命中绑定时还有 `selection`，说明活跃或固定版本模式。
通过 `run get` 再次查询，通过 `run revision` 获取冻结版本内容。
修改绑定或启用新版本影响后续新建 Run，旧 Run 和完全相同的幂等重试保留原有策略版本。
保管创建时保存的 Submission Credential，重放不能向新文件再次返回明文凭据。
恢复操作见[Run 与凭据说明](../../cmd/guardrailctl/README.md#runs-and-submissions)。

再次 PUT 时省略 `--revision-id` 可恢复跟随活跃版本。删除绑定使用
`guardrailctl binding delete --scene recovery --kind repository --resource-id 'team/repository'`。
重复删除成功，场景默认策略保持原有配置。

## 选择规则与权限

- 管理命令使用管理员会话，Project key 沿用现有 Run 权限。
- `--resources` 接收 1–100 个互不重复的 `{kind,id}` JSON 数组，文件上限 64 KiB。
  ID 使用包含斜杠、中文等字符的原始值。
- 省略资源或所有资源均未绑定时选场景默认策略。部分命中或命中不同绑定时返回 409。
  绑定目标未发布或已归档时也会失败。
- 接入方负责从真实操作生成可信资源引用，并核对执行对象与这些引用一致。
  具体规则见[绑定契约](../../docs/extensions.md#resource-bindings)。
- 写请求超时后可能已生效，应先查询绑定实际状态再决定重试。列表每次返回一页，
  分页时直接使用服务返回的 `nextAfter`。

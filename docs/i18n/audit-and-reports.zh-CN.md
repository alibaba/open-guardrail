# 审计查询与 Run 排障报告

管理员可在控制台“审计事件”中查询当前项目的变更，在 Run 详情下载排障报告。报告用于排障和关联查询，不代表已执行外部操作。

## 查询与导出

```sh
guardrailctl audit list --server http://127.0.0.1:8080 \
  --session-file ./session.json --resource-type policy-binding --limit 20

guardrailctl audit export --server http://127.0.0.1:8080 \
  --session-file ./session.json --resource-type policy-binding \
  --from 2026-11-01T00:00:00Z --to 2026-12-01T00:00:00Z \
  --output ./audit-page.json
```

使用 `auth login` 创建的管理员会话。支持按事件类型、操作者 ID、资源类型、存储资源 ID 精确筛选，以及包含开始时间、不包含结束时间的范围筛选。CLI 时间必须带 RFC3339 时区；页面使用浏览器本地时区。

`--resource-id` 指审计记录里的 ID。绑定事件使用 `sceneKey + "\n" + kind + "\n" + resourceId` 的 SHA-256；管理员查询详情可看到场景和变更前后的绑定。

结果按事件 ID 倒序排列，每页默认 20 条，上限 100 条。将 `nextCursor` 传给 `--cursor` 获取下一页，保持项目与筛选条件一致。新插入的事件不会导致后续旧页重复。每次导出一页；页面导出时重新查询，第一页可能包含新事件。继续导出时使用响应中的游标，另存为新文件。

导出移除操作者、资源原始 ID 和详情，保留它们的指纹、事件 ID、类型及时间。哈希可用于关联，不能保证低熵标识无法猜测；管理员普通查询含原始详情，不能直接视为脱敏结果。分享前仍需检查保留字段。

缺失的历史详情正常返回；非法或超过 4096 字节的详情省略并标注 `detailsUnavailable`。未归属工作空间和项目的旧事件不会被分配给当前项目。接口只查询服务已经写入的事件，不新增执行回执、读取审计或数据保留/删除机制。项目密钥和 Run 提交凭据不能查询审计。

## Run 报告

```sh
guardrailctl run report --server http://127.0.0.1:8080 \
  --key-file ./project-key.json --output ./run-report.json run_REPLACE_ME
```

所有参数放在 Run ID 前。项目密钥需要 `runs:read`，只能访问所属项目；管理员使用控制台或管理 API。Run 提交凭据不能读取报告。

报告包含冻结策略版本与摘要、状态、决策、有效期、结构化原因，最多 20 条近期提交摘要，以及最终提交中检查结论、证据要求、观测时间与内容指纹。`nextBefore` 表示仍有旧提交，可使用现有提交列表接口继续查询。并发产生的新提交需要刷新后才能看到。

报告不包含 YAML、ChangeSet 正文、资源选择详情、自由文本摘要或原始证据；保留场景、检查和证据要求等标识，分享前应检查。未形成最终提交的 Run 没有最终证据块，中间观测需通过已鉴权的提交入口查询。指纹只标识已存内容，不证明观测真实，报告不能作为执行授权。

两种导出默认写标准输出；`--output` 创建私有的 `0600` 新文件，不覆盖已有文件或符号链接。不要向问题反馈附加会话或密钥文件。

## 接口与验收

- 管理员：`GET /api/v1alpha1/audit-events`，`redacted=true` 返回脱敏页，`from`/`to` 使用 Unix 秒。
- 管理员：`GET /api/v1alpha1/runs/{runId}/report`。
- 具有 `runs:read` 的项目密钥：`GET /runtime/v1alpha1/runs/{runId}/report`。

运行 `make smoke-bindings` 启动隔离的真实服务，完成绑定增改删、审计分页与脱敏导出，再提交拒绝结果，核对报告中的策略和证据指纹。结束后清理临时服务与文件。

审计分页索引由 SQLite、PostgreSQL、MySQL 的第 4 版迁移创建，保留已有事件。自动迁移在启动时执行，外部管理数据库需先应用随包迁移。

文件导出目录需要支持硬链接。目标已存在时返回用法错误（退出码 2），权限或文件系统能力不足时返回导出失败（退出码 1），可改用标准输出或支持硬链接的文件系统。没有操作者的历史记录省略操作者指纹。

# Agent 清理插件预览


版本化安装、配置诊断与独立验收请见[安装试用指南](https://github.com/alibaba/open-guardrail/blob/main/docs/plugin-trial.zh-CN.md).

[English 与完整命令说明](README.md)

为 pi、DeepSeek Harness 提供原生工具和调查取证 Skill，复用现有文件清理示例。
Agent 读取 Run 的冻结策略和实际文件观测，自行给出检查结论；插件提交原始观测作为
证据，执行工具再次核验权威决策、证据、操作参数及当前文件状态。

当前支持 pi 1.5.0、DeepSeek Harness 0.2.2-rc.1；需要 Node.js 22+、Python 3.9+、
Linux/macOS 原生服务和 CLI 二进制，以及宿主自己的有效模型登录。没有发布公共 npm 包。

从仓库根目录执行：

```sh
make build
export OPEN_GUARDRAIL_BINARIES="$PWD/bin"
plugin_install_dir=$(mktemp -d)
(cd integrations/agent-plugins && npm pack --pack-destination "$plugin_install_dir")
```

pi 安装解压后的目录；不能直接把本地 tgz 当成扩展目录：

```sh
tar -xzf "$plugin_install_dir/openguardrail-cleanup-preview-0.1.0-alpha.0.tgz" -C "$plugin_install_dir"
pi install "$plugin_install_dir/package"
pi --no-mcp --tools 'read,guardrail_cleanup_*'
```

DSH 需要 pnpm，用所选 profile 的插件管理入口安装：

```sh
dsh plugin --profile headless add "$plugin_install_dir/openguardrail-cleanup-preview-0.1.0-alpha.0.tgz" --ignore-scripts
dsh --profile headless '使用 guardrail-cleanup Skill，先评估全部四个示例文件并尝试执行，展示拒绝；再缩小到允许的目标，重新评估执行，最后核对实际文件与两条决策。'
```

可用 `PI_CODING_AGENT_DIR` 或 `DSH_HOME` 指定单独的测试配置，保留正常工作环境。
模型、模型地址和推理凭据均由宿主管理，不写入插件。初次工具调用会启动隔离服务；
不会复用已有管理员会话、数据库或用户工作目录。包内包含示例实现和策略，运行时
只需上述原生二进制，不依赖源码目录和 Go。

预期结果：首次得到 DENY，四个文件均保留；缩小范围后新建 Run，得到 ALLOW，
只删除 `cache.tmp` 和 `download.tmp`，保留 `orders.csv` 和 `config.yaml`。
用 `guardrail_cleanup_status` 核对实际状态和历史 Run。即使 Agent 错报 PASS，
执行工具仍会根据真实文件状态和业务规则拒绝删除受保护文件。

会话退出或插件卸载会停止自有服务、撤销示例密钥并删除临时数据，记录不是长期存储；
需要保留时在退出前保存不含凭据的状态结果。发生部分失败后查看实际文件，不能把
取消、超时或服务端 ALLOW 当作执行回执。同一进程内串行化不是全局执行 Claim。

首批只保护插件自有的四个临时示例文件，不接受任意路径，也不承诺拦截其他 shell、
文件工具或同一操作系统用户的进程。完整边界、重试、验证、升级及卸载命令见英文说明。

`make test-python` 和 `make test` 默认包含 `make test-agent-plugins`，
会构建原生二进制并验证隔离服务与真实文件，需要 Go、Python 和 Node 22+。
证据核验与实际删除依赖这层门禁；直接执行测试且不配置二进制时，live 用例会跳过。
完整宿主 SDK 测试还需按英文说明设置
`OPEN_GUARDRAIL_HOST_MODULES`。真实模型和实际安装包的体验任务需要另外运行，
不能用 mock 或工具注册成功替代。

## DSH 无人值守运行与退出验收

DSH 0.2.2-rc.1 与 pi-ai 1.5.0 的 Codex 订阅路由存在会话连接清理缺失，
主回复与后台标题生成并发首次建连时还可能覆盖缓存条目，留下无法由会话清理入口
释放的连接和五分钟定时器。无插件、纯文本任务也复现退出延迟。

在该版本组合下，Codex 无人值守任务使用宿主支持的 SSE 传输。在现有
`llm-pi-ai.config.providers.openai-codex` 中增加 `transport: sse`，保留完整的
认证、模型、其他提供方及配置。DSH patch 可能替换整个 `providers` 映射，
单独加载只含 `transport` 的片段会丢失其他设置。英文说明包含配置位置示例。
主回复与标题生成均应使用该传输。插件包由宿主管理模型配置。

安装包内附带 `verify-host-exit.py`，可按英文说明用现有宿主登录运行真实任务。
它保存输出与 `exit.json`，单独记录父进程是否发送 SIGTERM/SIGKILL。超时后即使
宿主返回退出码 0，验收也失败。输出目录必须是新建的私有路径，分享前检查宿主日志。
业务验收仍需核对拒绝、收窄后放行、权威证据、临时文件删除与保护文件保持不变。
本次已复验的 SSE 配置属于该版本组合的兼容措施，DSH/pi-ai 根因修复仍需宿主侧完成。

创建 Run 或校验冻结版本时失败，同一 requestId 会明确报告创建结果不确定，需检查
现有 Run 后再继续，原操作目录与凭据会保留。成功 prepare 后相同参数重试返回原 Run。
提交获确认后立即删除本地提交凭据，相同检查的重试读取权威 Run；结果不确定时保留
凭据与原快照供同一提交重试，会话结束后统一清理。首调预算七分钟，确认初始化后
每次调用预算为九十秒，超时或取消会停止该会话并要求核对实际结果。

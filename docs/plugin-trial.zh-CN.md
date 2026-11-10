# Agent 插件安装与试用

[English](plugin-trial.md)

使用同一版本的服务、CLI 与插件，在 pi 或 DeepSeek Harness 中体验检查、取证、拒绝和放行。清理工具操作四个临时样例文件，每个会话启动独立本地服务，退出后清理临时数据。

## 环境与兼容范围

| 组件 | 要求或已验证基线 |
| --- | --- |
| 原生二进制 | Linux/macOS，amd64 或 arm64，同一发行版本的服务与 CLI |
| Python | 3.9 及以上 |
| Node.js | 22 及以上 |
| pi | 1.5.0，`@earendil-works/pi-coding-agent` |
| DeepSeek Harness | 0.2.2-rc.1，`@deepseek-ai/dsh`，安装插件需要 pnpm |
| 模型 | 在所选宿主中单独完成模型登录 |

上述宿主此前已在 macOS arm64 上通过真实模型清理验证。其他平台二进制经过交叉编译，CI 在 Linux 和 macOS 执行原生安装验收，具体结果以对应提交的 CI 为准。升级宿主或切换模型路由后，需要重新验证真实模型任务。

固定版本的 DSH 使用 Codex 订阅时，请在现有配置中选择 SSE。配置及自然退出验收见[宿主限制说明](../integrations/agent-plugins/README.zh-CN.md)。

## 获取配套产物

从同一经过批准的版本产物目录获取 `plugin-release.json`、`install.py`、当前平台的原生归档和 `openguardrail-cleanup-preview-VERSION.tgz`，放在同一目录。将下文 VERSION 替换为实际版本。正式公开产物在 [GitHub Releases](https://github.com/alibaba/open-guardrail/releases) 获取。维护者提供的候选包应附源提交和验收结果，注册表安装以实际发布状态为准。

维护者可在经过审查的源码目录生成候选产物，需要 Go、Python 3.12+、Node 22+ 和 npm

```sh
make smoke-plugin-release VERSION=0.1.0-alpha.0
```

该入口执行公开打包检查，生成四平台原生归档、同版本插件、校验清单及安装器，并在仓外完成安装验收。配套产物校验完成后整体写入 `dist/plugin-release-VERSION/`，其他版本及 `dist/` 中间文件均不进入该目录。已存在的产物目录会保留，重新构建时通过 `PLUGIN_RELEASE_DIR=/absolute/path/to/new-bundle` 选择新目录。

## 安装与诊断

在下载的产物目录执行

```sh
python3 install.py --assets-dir . --prefix "$HOME/.local/share/open-guardrail" --version VERSION
source "$HOME/.local/share/open-guardrail/VERSION/env.sh"
plugin_dir="$HOME/.local/share/open-guardrail/VERSION/plugin"
node "$plugin_dir/doctor.mjs" --live
```

安装器验证当前平台与插件归档的 SHA-256，拒绝不安全的归档条目，创建独立版本目录。已存在的版本会保留。安装器与清单应来自同一可信分发渠道，哈希用于完整性核对，发行者身份验证还需可信下载渠道或另行核验来源证明、签名。

诊断检查 Node、Python 和二进制可用性。`--live` 会启动独立服务，通过真实 CLI 查询服务并核对插件版本。增加 `--host pi` 或 `--host dsh` 可检查固定宿主版本，DSH 还检查 pnpm。模型登录、工具与 Skill 实际加载通过后续宿主任务验证。

先运行确定性自检

```sh
node "$plugin_dir/trial.mjs"
```

自检创建真实 Run，验证首次拒绝且文件全部保留，随后收窄范围并放行，核对两个临时文件实际删除、保护文件元数据与内容哈希保持一致，以及权威 Submission 关联。输出不包含凭据的摘要，会话结束后临时记录清理。该步骤使用程序指定结论，真实模型参与需要继续完成下述验收。

## 在宿主中试用

pi 安装解压后的插件目录

```sh
node "$plugin_dir/doctor.mjs" --host pi
pi install "$plugin_dir"
pi --no-mcp --tools 'read,guardrail_cleanup_*'
```

DSH 将下载的插件归档安装到实际使用的 profile

```sh
node "$plugin_dir/doctor.mjs" --host dsh
dsh plugin --profile headless add /absolute/path/to/openguardrail-cleanup-preview-VERSION.tgz --ignore-scripts
dsh --profile headless
```

隔离试用可指定独立 `PI_CODING_AGENT_DIR` 或 `DSH_HOME`，并在该环境通过宿主正常登录模型。向模型发送任务

> 使用 guardrail-cleanup Skill。先评估四个样例文件并尝试执行，展示拒绝。随后使用新的 requestId 缩小范围，重新评估并执行。最后检查实际文件、两次权威决策，并用 status(runId) 查看各 Run 接受的证据。

检查工具结果。首次请求应拒绝且四个文件均保留，第二次只删除 `cache.tmp` 和 `download.tmp`，`orders.csv` 和 `config.yaml` 保持不变。退出会话前检查权威记录。无人值守 DSH 试用还需按插件说明使用包内 `verify-host-exit.py` 验证自然退出。

## 真人独立验收记录

请未参与开发的使用者在自己的测试环境按本文操作，每人单独记录

| 项目 | 记录要求 |
| --- | --- |
| 参与者与日期 | 实际填写 |
| 服务、插件、平台、宿主与模型路由 | 实际版本与配置类型 |
| 安装耗时、首次任务完成耗时 | 实际记录 |
| 是否理解拒绝原因并完成修正 | 通过或失败，附原因 |
| 实际删除范围与保护文件状态 | 工具返回与文件核对结果 |
| 冻结策略、接受的证据与决策 | 实际 Run ID 与核对结果 |
| 无人值守自然退出 | 退出验收器结果 |
| 维护者介入与阻塞 | 具体步骤、错误及协助内容 |

维护者的干净环境自动化测试、真实模型验证、真人独立试用分别记录。真人试用完成后再填写结果。分享日志前清理凭据、个人路径和私有模型数据。

## 升级、卸载与排障

升级时安装到新版本目录，停止宿主，加载新 `env.sh` 并重新注册插件，重跑诊断和验收。保留旧目录可用于回退。pi 切换到新版本目录前需移除旧注册，否则两份插件会注册相同工具名

```sh
pi remove /absolute/path/to/OLD-VERSION/plugin
pi install /absolute/path/to/NEW-VERSION/plugin
```

DSH 按同一包名替换，安装后检查实际 profile。卸载前停止宿主并注销插件，再删除对应版本目录。DSH 按包名 `@openguardrail/cleanup-preview` 移除插件，pi 按安装目录移除。

诊断失败时按 `fix` 字段处理。模型登录或 Skill 发现失败时检查实际 profile 和宿主诊断。执行超时或失败时，先核对实际状态及历史 Run，再决定恢复方式。问题反馈应提供版本、失败步骤、Run ID 与脱敏诊断摘要，使用[问题模板](https://github.com/alibaba/open-guardrail/issues/new?template=bug_report.md)。

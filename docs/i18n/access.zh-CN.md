# 访问方式与 Run 提交凭据

OpenGuardrail 启动后即可使用。直接打开控制台，或通过 API、CLI 操作，
无需管理员账号、密码、登录、会话文件和 Project API Key。
服务自动准备默认 workspace 和 project。

能够访问服务的调用方可以管理 Scene、Spec、绑定、Run 和审计记录，也可以轮换
未结束 Run 的提交凭据。默认监听 `127.0.0.1:8080`；共享部署应使用可信网络或
具有认证能力的反向代理。远程部署仍要求配置 HTTPS 源、可信 TLS 代理及精确的
Host/Origin 校验。页面和 CLI 自动携带写请求所需的 `X-Guardrail-CSRF: 1`。

## Run 提交凭据

创建 Run 时仍会签发绑定该 Run 的提交凭据。提交中间或最终评估必须使用该凭据，
并继续校验有效期、Run 状态、证据和裁决一致性。凭据文件需为权限 `0600` 的普通文件，
保存在源码目录之外。Run 凭据只用于对应的 Submission 接口。

```sh
umask 077
guardrail_credentials_dir=$(mktemp -d)
guardrailctl scene apply --key recovery --name Recovery   --spec ./examples/policies/recovery.yaml --publish
guardrailctl run create --scene recovery --change-set ./change-set.json   --submission-credential-file "$guardrail_credentials_dir/run.json"
guardrailctl run get run_EXAMPLE
guardrailctl run submit run_EXAMPLE --submission ./submission.json   --submission-credential-file "$guardrail_credentials_dir/run.json"
```

重试与轮换规则见 [Run 指南](runs.zh-CN.md)。

## 现有安装

已有 Scene、Spec、Run 和审计记录保持可用。旧账号、会话、引导令牌和 Project Key
不再读取或接受；旧 Spec 的维护人列表不再限制开源独立部署的编辑。
保留历史数据库迁移以兼容升级。

## 内部集成

内部 `trusted_operator_namespace` 模式仍为纯 API 模式，保留操作者处理和
Spec 维护人校验；内部身份与权限适配器继续按原有边界工作。

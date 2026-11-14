# 安装原生预览版

当前预览版为 **v0.1.0-alpha.1**。服务与 CLI 可直接在 Linux 或 macOS 运行，无需 Go、Node.js 或 Python。可选 Demo 需要 Python 3.9+。

## 选择平台下载

打开[发行下载页](https://github.com/alibaba/open-guardrail/releases/tag/v0.1.0-alpha.1)，选择对应 macOS / Linux 和 amd64 / arm64 的原生归档，以及同一版本的 `SHA256SUMS`。按[归档校验说明](../releases.md#check-and-try-a-downloaded-archive)核对后，解压到空目录。

在 pi 或 DeepSeek Harness 中使用时，再下载同版本的完整插件安装包，按 [Agent 插件指南](../plugin-trial.md)操作。

## 体验文件清理 Demo

在解压目录中运行：

```sh
python3 examples/cleanup-demo/demo.py
```

设置密码，打开终端输出的 Demo URL。预定义 Runner 不使用模型，服务调用和临时文件操作均真实执行。

## 启动持久服务

在解压目录中运行：

```sh
./open-guardrail serve
```

打开 `http://127.0.0.1:8080`，创建首个管理员。SQLite 在本地保留数据；远程或 PostgreSQL 部署见[配置指南](../configuration.md)。

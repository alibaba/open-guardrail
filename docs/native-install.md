# Install the native preview

The current preview is **v0.1.0-alpha.1**. The server and CLI run directly on
Linux or macOS without Go, Node.js or Python. The optional demo needs Python 3.9+.

## Download for your platform

Open the [release downloads](https://github.com/alibaba/open-guardrail/releases/tag/v0.1.0-alpha.1).
Choose the native archive for macOS or Linux and amd64 or arm64, plus `SHA256SUMS`
from the same release. Follow the [archive verification instructions](releases.md#check-and-try-a-downloaded-archive),
then extract the verified archive into an empty directory.

For pi or DeepSeek Harness, also choose the complete plugin installation archive
from that release. Follow the [Agent plugin guide](plugin-trial.md).

## Try the cleanup demo

From the extraction directory:

```sh
python3 examples/cleanup-demo/demo.py
```

Choose a password and open the printed Demo URL. The predefined runner uses no
model; the service and disposable file operations are real.

## Start a persistent service

From the extraction directory:

```sh
./open-guardrail serve
```

Open `http://127.0.0.1:8080` and create the first administrator. SQLite retains
the data locally. See [configuration](configuration.md) for remote or PostgreSQL deployment.

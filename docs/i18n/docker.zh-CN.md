# HTTPS 代理后的 Docker 部署

需要 Docker Engine、Compose v2 插件；本地示例还需要 OpenSSL。在仓库根目录执行以下命令。Compose 文件默认构建使用 SQLite 的公开发行版，PostgreSQL 必须显式配置。原生本地安装从[快速开始](../getting-started.md)开始。

服务监听私有容器网络，只有 TLS 代理发布宿主机端口。即使在 localhost 测试，也必须显式提供 HTTPS 对外源；容器网络不支持免令牌本地初始化。

## 本地 HTTPS 示例

创建仅对 localhost 和 127.0.0.1 有效的一次性证书：

```sh
umask 077
mkdir -p deploy/tls
openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 1 \
  -keyout deploy/tls/privkey.pem -out deploy/tls/fullchain.pem \
  -subj /CN=localhost -addext 'subjectAltName=DNS:localhost,IP:127.0.0.1'
chmod 600 deploy/tls/privkey.pem
export OPEN_GUARDRAIL_TLS_DIR="$(pwd)/deploy/tls"
export OPEN_GUARDRAIL_PUBLIC_URL=https://localhost:8443
docker compose -f deploy/compose.yaml up -d --build
curl --fail --retry 30 --retry-connrefused --retry-delay 1 \
  --cacert deploy/tls/fullchain.pem https://localhost:8443/readyz
```

本地浏览器信任该临时证书后，打开 <https://localhost:8443>。默认发布地址为 `127.0.0.1:8443`，其他机器不能连接。对外 URL 必须精确匹配浏览器源，包括非标准端口，不能包含路径或末尾斜杠。自动化不能关闭 TLS 验证。两个必填变量缺失时，Compose 插值会失败；服务也会在打开存储前拒绝缺失或无效的 HTTPS 源。

## 创建首个管理员

运行时镜像没有 shell 或 `cat`。用 Docker 文件复制命令取出私有令牌，不要把它写进 Compose 环境变量或日志：

```sh
umask 077
setup_dir=$(mktemp -d)
docker compose -f deploy/compose.yaml cp guardrail:/data/bootstrap.token "$setup_dir/bootstrap.token"
chmod 600 "$setup_dir/bootstrap.token"
```

在本地读取该文件，在浏览器初始化页面输入其内容，以及管理员用户名和密码。令牌应保密，一小时后过期；未初始化服务重启时会更新过期令牌。初始化完成后，令牌不能再创建第二个管理员。设置完成后删除宿主机副本：

```sh
rm -f "$setup_dir/bootstrap.token"
rmdir "$setup_dir"
```

也可以独立安装 `guardrailctl`，通过 `guardrailctl bootstrap init --server URL --username admin --token-file PATH` 初始化服务；该主机必须信任 TLS 证书。见[管理员初始化](../local-administrator.md)。

## 持久数据与权限

镜像包含 `LICENSE`、`NOTICE`，以及位于 `/usr/share/licenses/open-guardrail/` 的第三方原始声明。

镜像以 UID / GID `65532:65532` 运行，`/data` 归该身份所有，权限为 `0700`；Docker 从此目录初始化**新命名卷**。Compose 示例把 `guardrail-data` 挂载到该位置，因此 SQLite、WAL 文件和权限为 `0600` 的引导令牌可在容器重建后保留。不能把同一个 SQLite 卷挂载到多个服务实例。

`docker compose -f deploy/compose.yaml down` 保留命名卷，增加 `-v` 会删除卷及其中的管理员、策略和 Run。升级前应停止服务，再备份数据库。保留数据、重建后端和代理：

```sh
docker compose -f deploy/compose.yaml up -d --build --force-recreate
```

替换为 Linux 绑定挂载时，必须在启动容器**之前**创建目录，设置属主 `65532:65532` 和权限 `0700`，例如 `sudo install -d -o 65532 -g 65532 -m 0700 /srv/open-guardrail/data`。绑定挂载会遮蔽镜像目录权限；Docker 不会修正已有卷或宿主机目录的属主。Rootless Docker / 用户命名空间部署需要把容器 UID / GID 映射为对应宿主机身份。不能用 `chmod 777` 或让应用以 root 运行来绕过权限失败。

## 远程主机

提供真实主机名及客户端信任的证书：

```sh
export OPEN_GUARDRAIL_PUBLIC_URL=https://guardrail.example.com
export OPEN_GUARDRAIL_TLS_DIR=/absolute/path/to/tls
export OPEN_GUARDRAIL_HTTPS_BIND=0.0.0.0:443
docker compose -f deploy/compose.yaml up -d --build --force-recreate
```

TLS 目录必须包含 `fullchain.pem` 和 `privkey.pem`，并以只读方式挂载到 NGINX。提供实际 DNS 名称，通过主机 / 防火墙把访问限制为可信操作者。远程部署的密钥保存在源码树以外。本地 `deploy/tls/` 被 Git 忽略，PEM / 密钥和本地环境文件不进入 Docker 构建上下文。镜像不内置证书或令牌，续期后需重新加载或重建代理。

### 稳定远程主机名的可选 HSTS

共享代理示例默认关闭 HSTS，因为它也用于采用临时证书的本地 Demo。对于稳定远程主机名，只有证书已被客户端信任，且 HTTPS 将持续可用时，才启用 HSTS。先在 `deploy/nginx.conf` 的 TLS `server` 块中使用较短策略：

```nginx
add_header Strict-Transport-Security "max-age=300" always;
```

验证部署后再增加 `max-age`。只删除响应头不会清除浏览器已缓存的策略。除非有意管理更广范围的 HTTPS 承诺，否则不添加 `includeSubDomains` 或 `preload`。见 [NGINX HSTS 指南](https://blog.nginx.org/blog/http-strict-transport-security-hsts-and-nginx)。

### 代理与应用超时

代理的 `proxy_read_timeout 20s` 限制连续响应读取之间的空闲间隔，长于应用 10 秒的协作式请求预算和 15 秒套接字写超时。当前同步 API 围绕该预算设计；更长操作需要异步 API，或协调修改服务和代理的超时。单独增加代理超时不会延长应用预算。

提供的 NGINX 配置保留外部 `Host`（**包括端口**），传递 `Origin` 和授权头，移除常见客户端转发头。应用验证配置的源，在这种部署下始终要求引导令牌。不能发布后端明文 8080 端口，不能把无关容器接入私有网络，也不能通过隧道暴露默认原生回环模式。管理员身份功能仍处于预览，应落实[远程访问](../local-administrator.md#remote-access)中说明的访问控制。

使用 PostgreSQL 时，显式选择 `OPEN_GUARDRAIL_DATABASE_DRIVER=postgres`，通过私有部署配置注入 `DATABASE_URL`。即使使用外部数据库，仍应保持 `/data` 私密且持久，以保存引导状态；数据库网络访问需在此隔离 SQLite 示例之外单独安排。

## 验证部署

`make smoke-docker` 要求 Docker daemon、Compose v2 和 OpenSSL。它构建真实镜像，并使用临时证书、唯一 Compose 项目和新卷。测试专用 Compose 覆盖配置为镜像设置不同于默认命名的显式标签，因此镜像查找必须使用实际创建容器的镜像 ID。

检查包括配置拒绝、TLS 就绪、非 root 存储、原始许可证 / 声明文件哈希、要求令牌的初始化、源 / CSRF 拒绝、管理员登录，以及容器重建后的持久化。清理只移除该一次性项目的容器和卷，公开 CI 在 Docker 运行器上执行同一目标。

参考：[Docker 卷初始化](https://docs.docker.com/engine/storage/volumes/)和 [NGINX 代理头行为](https://nginx.org/en/docs/http/ngx_http_proxy_module.html#proxy_set_header)。

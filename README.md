# go-net-connector

以 Go 实现 Ruby net-connector 0.18.0 冻结工作区的设备连接、配置采集、原生上传、拓扑审计和批次执行模块。支持 Go 1.26+，目标平台为 Linux/macOS、amd64/arm64。

底层使用 `github.com/gatework/go-expect` 和系统 SSH/Telnet。此工作区包含尚未发布的 expect 兼容补丁；`v0.0.0` 仅为本地依赖占位，不能据此直接从公共 Go 模块代理安装。源码快照、补丁和逐文件校验清单均包含在本项目，原始 expect 工作区不会被改写。

## 构建

```sh
python3 scripts/prepare-workspace.py
go test ./...
go build -o bin/ ./cmd/...
```

准备脚本从 `dependencies/go-expect-source.tar.gz` 重建冻结源码，在独立的 `.work/go-expect` 中应用 `patches/go-expect-compat.patch`，校验候选源码，然后创建本地 `go.work`。已存在的候选目录只校验，不覆盖；额外源码、符号链接或已记录文件内容变化都会被拒绝。也可传入与基线完全相同的本地 go-expect 源目录；发生漂移时脚本拒绝继续。

```sh
python3 scripts/prepare-workspace.py /path/to/go-expect
```

系统需要 `ssh`；Telnet 连接另需系统 `telnet`。SSH 归档服务器需要管理员安装 `net-connector-archive`，默认路径 `/usr/local/bin/net-connector-archive`。客户端只探测协议版本和能力，不上传或安装 worker。

## 命令入口

| 命令 | 默认行为 |
| --- | --- |
| `net-backup` | 根入口，strict 成功策略，每厂商抽样 5 台，直接写配置目录，JSON 结果 |
| `net-backup-batch` | selected 策略，全量选择，创建批次目录 |
| `net-backup-tftp` | 原生 TFTP 上传、可选本地/SSH 文件核验和归档 |
| `net-topology` | 只读拓扑规划；修改必须显式 `--apply` |
| `net-connector-archive` | 归档服务器上的标准输入/输出协议 worker |

```sh
bin/net-backup --help
bin/net-backup --init --non-interactive
bin/net-backup --show-config
bin/net-backup --plan --hosts 192.0.2.0/24
bin/net-backup-batch --all --json
bin/net-backup-tftp --tftp-server 192.0.2.9 --tftp-root /srv/tftp --success-policy verified --json
bin/net-topology --inventory-only --json
bin/net-topology --plan --hosts 192.0.2.1 --json
bin/net-topology --apply --hosts 192.0.2.1 --json
bin/net-backup --export 192.0.2.1 --directory ./backups --output ./saved.cfg
```

这些执行命令会访问配置的清单和设备。帮助、初始化、公开配置查看、本地导出均可离线运行；`--inventory-only` 可读取清单，但不连接设备。

退出码：`0` 策略成功，`1` 已执行但不完整，`2` 参数/清单/基础 I/O 错误，`130` 中断。进度写 stderr；JSON 写 stdout。批次目录保留 `plan.json`、`events.jsonl`、`summary.json`、`summary.txt`；会话日志的位置与内容由日志策略决定。拓扑还保留逐设备报告、命令文件和 SHA-256。

## 配置和凭据

优先级为 CLI > 原进程环境 > YAML > `.env` > 默认值。配置发现包括系统目录、用户目录、程序目录和启动目录，不向上搜索父目录。相对路径按配置来源解析；读取不会修改进程环境。YAML 禁止别名、重复键、多文档和非字符串键。

```yaml
# backup.yml
netdisco:
  source: http
  url: https://netdisco.example.invalid
  cache_mode: prefer
  cache_directory: ./.cache/net-connector/inventory
inventory:
  include_hosts: [192.0.2.0/24]
  name_match: contains
ssh:
  protocol: ssh
  host_key_policy: accept_new
  known_hosts: ./known_hosts
  log_level: info
  # log_directory: ./logs
backup:
  directory: ./backups
  parallel: 4
```

密码不放在 YAML。环境变量包括 `ND_USERNAME`、`ND_PASSWORD` 或 `ND_API_KEY`，以及 `NC_DEVICE_USERNAME`、`NC_DEVICE_PASSWORD`、`NC_ENABLE_PASSWORD`。设备凭据支持 `NC_<VENDOR>_USERNAME/PASSWORD/ENABLE_PASSWORD`，例如 `NC_H3C_PASSWORD`。显式通用 CLI 凭据覆盖对应厂商值。终端可用 `--ask-password`；自动化可用 `--stdin-credentials`，先发送一行凭据 JSON，执行备份或拓扑应用前再发送 `RUN`。

日志配置使用 `ssh.log_directory` / `NC_LOG_DIRECTORY` 与 `ssh.log_level` / `NC_LOG_LEVEL`。级别为 `debug`、`info`、`warn`、`error`；默认 `info`，只在 `debug` 记录完整脱敏回显。未指定日志目录时，普通根入口和批量备份不创建会话日志，批量 TFTP 与拓扑使用批次目录下的 `logs`；显式目录覆盖这些默认值。

主机密钥策略 `replace` 必须显式指定 `ssh.known_hosts`，不会隐式替换用户默认信任文件中的密钥。

PostgreSQL 使用 `netdisco.source: postgres`、`query`、`query_params`；身份来自 `ND_DB_HOST/NAME/USER/PASS`。查询通过参数绑定和只读游标执行。`cache_mode: only` 仅使用已校验缓存，禁止源查询。

TFTP 的 `server`、`servers`、`verification_root`、`verification_roots`、`source_files`、`source_interfaces`、`vrfs` 支持默认值与精确地址覆盖。SSH 核验目录采用 `ssh://user@host/absolute/path`，凭据通过 `NC_TFTP_SSH_PASSWORD` 或 SSH 密钥提供。所有服务器及策略预检成功后才开始设备任务。PAN 固定上传名需要可用归档根目录。

## Go API

```go
profile, err := vendors.Profile("cisco_ios")
if err != nil { return err }
device, err := connector.New(profile, connector.ConnectionConfig{
    Host: host, Username: username, Password: password,
    KnownHosts: knownHosts, HostKeyPolicy: "strict",
})
if err != nil { return err }
defer device.Close()
result, err := device.RunningConfig(ctx)
if err != nil { return err }
configuration, ok := result.Config()
_ = configuration
_ = ok
```

`connector.New` 不连接设备；`vendors.Open` 会建立连接。`ExecuteScript` 先校验完整脚本，再取得设备操作租约，操作并发或重入返回忙错误。每条命令共享发送、分页和交互的绝对期限；默认响应上限 32 MiB，也支持脚本累计预算。

常用包：

- `connector`：设备生命周期、命令/脚本、租约、回执、原文与渲染输出、配置采集和本地备份。
- `vendors` / `textfsm`：12 个厂商档案、19 个内嵌 TextFSM 模板、离线配置解析。
- `topology`：发现、描述审计、纯规划、确认后应用、读回验证和保存。
- `transfer`：原生 TFTP、Junos FTP/SCP、目标策略及设备报告回执。
- `archive`：本地/SSH 归档池、worker 协议和服务器核验。常规归档使用 `NewPool`；`Client` 是低层协议组合接口。
- `inventory` / `netdisco` / `parallel`：纯选择器、HTTP/PG 来源、缓存、计划、Fleet、报告和有界并发。
- `storage`：稳定路径锁、私有日志、原子写入与提交/持久化回执。
- `config`：不可变配置快照；`internal/bootstrap` 是唯一 samber/do 组合根。

Go API 直接注入 `ConnectionConfig.Logger` 且不设置 `LogLevel` 时，保留底层脱敏回显写入行为；设置 `LogLevel` 后使用结构化事件，只有 `debug` 包含回显。CLI 始终使用配置中的日志级别。

操作失败时仍需检查返回结果：已完成的命令、已提交文件和已确认上传可能保留在部分回执中。库不会自动重放设备写入。`device_reported` 仅表示设备报告完成；`server_verified` 表示已读取并核验服务器文件。用户回调、日志 sink 和上传回调必须配合 context；库不以遗弃 goroutine 的方式模拟取消。

## 验证与边界

首版发布流程与产物校验见 [发布说明](docs/releasing.md)，当前发布验证状态见 [发布验证](docs/release-verification.md)。

```sh
go test -race ./...
go vet ./...
GOTOOLCHAIN=go1.26.0 go test ./...
python3 scripts/verify-consumer.py
python3 scripts/verify-linux.py
```

来源和平台证据见 [验证记录](docs/verification.md)，模块和厂商对应关系见 [兼容矩阵](docs/compatibility.md)，所有权与失败边界见 [架构](docs/architecture.md)，实施中的兼容性与交付裁定见 [决策记录](docs/decisions.md)。本地假设备、PTY、HTTP/TLS、临时 PostgreSQL、localhost sshd 和安装后的 worker 是不同的证据层；这些结果不代表生产设备互通、远端 CI 或发布完成。

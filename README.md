# go-net-connector

以 Go 实现 Ruby net-connector 0.18.0 冻结工作区的设备连接、配置采集、原生上传、拓扑审计和批次执行模块。支持 Go 1.26+，目标平台为 Linux/macOS、amd64/arm64。

底层直接依赖 GitHub 上的 `github.com/gatework/go-expect` 和系统 SSH/Telnet。依赖版本由 `go.mod` 固定，构建不使用本地 `replace` 或跨仓库 `go.work`。历史校验记录仅用于审计，不参与当前构建。

## 构建

当前模块固定使用正式发布的 go-expect `v0.4.0`。本地构建和测试通过 Go 模块解析正式依赖，不需要候选源码或本地替换。

```sh
GOWORK=off go mod download
GOWORK=off go test ./...
GOWORK=off go build -o bin/ ./cmd/...
```

公开 Go API 的变更见 [迁移说明](docs/api-migration.md)。当前版本统一使用平铺的 `net-connector.yml`，归档协议升级为 v2；迁移步骤见上述说明。

系统需要 `ssh`；Telnet 连接另需系统 `telnet`。SSH 归档服务器需要管理员安装 `tftp-archive`，默认路径 `/usr/local/bin/tftp-archive`。客户端只探测协议版本和能力，不上传或安装 worker。

## 命令入口

完整中文参数与示例见 [命令参考](docs/cli-reference.md)，由命令树生成并校验。

统一运维入口为 `netman`，归档协议服务使用 `tftp-archive`。

```sh
bin/netman --help
bin/netman config init --non-interactive
bin/netman config show
bin/netman config defaults
bin/netman config defaults --for tftp --json
bin/netman inventory list --hosts 192.0.2.0/24
bin/netman backup plan --method cli --json
bin/netman backup run --json
bin/netman backup tftp --tftp-server 192.0.2.9 --tftp-root /srv/tftp --success-policy verified --json
bin/netman topology plan --hosts 192.0.2.1 --json
bin/netman topology apply --hosts 192.0.2.1 --json
bin/netman backup export 192.0.2.1 --batch ./backups/具体批次 --output ./saved.cfg
bin/netman completion zsh
```

备份默认全量选择和批次归档。配置采集默认 selected；TFTP 默认 verified，要求服务器核验并取回文件、本地持久化及暂存清理确认成功。`--success-policy selected` 可显式选择以设备报告判断上传结果。文本文件统一为 `设备名称-IP.txt`；可使用 `--sample` 抽样、`--success-policy strict` 严格判定及 `--layout flat` 平铺。设备名缺失时使用 `unnamed`。CLI 不生成 `.raw` 旁车：正常编码转换为 UTF-8；设备配置本身无法无损解码时保留原编码字节并报告编码警告，不猜字、不丢字，也不伪装成有效 UTF-8。

帮助、版本、补全、默认配置打印、配置查看和初始化不访问设备或清单；清单预览和备份规划可访问清单但不连接设备。拓扑规划需要只读访问设备，应用前会重新核验。

退出码：`0` 策略成功，`1` 不完整或没有可执行目标，`2` 参数、清单或 I/O 错误，`130` 中断。中文进度写 stderr，JSON 结果写 stdout。批次保留 `plan.json`、`events.jsonl`、`summary.json` 和 `summary.txt`。

| 原用法 | 新用法 |
| --- | --- |
| net-backup / net-backup-batch | netman backup run |
| net-backup-tftp | netman backup tftp |
| --inventory-only | netman inventory list |
| --show-config / --init | netman config show / init |
| net-topology --discover / --plan / --apply | netman topology discover / plan / apply |
| --export IP | netman backup export IP --batch 具体批次 |

四个旧入口不再提供。初始化向导使用 `config init --interactive`，完成后重新运行业务命令。导出以指定批次报告定位并核验文件，不自动查找最新批次。TFTP 上传文件名继续遵守厂商约束，本地归档保留原生扩展名。

## 运行进度

运行后立即显示配置与清单读取阶段，随后显示选中/跳过数量、并发、批次目录、设备连接和采集状态、完成计数及耗时；等待期间定时刷新。失败显示受控原因，结束显示结果统计和报告位置。进度写 stderr，不包含密码、配置正文或设备原始回显。

`--verbose` 在原地面板内显示更多连接和命令阶段详情；`--quiet` 隐藏进度。`--json --quiet` 适用于自动化。CLI 文本备份不写原文旁车，原生 TFTP 仍保留厂商文件格式。

## 配置和凭据

优先级为 CLI > 原进程环境 > YAML > `.env` > 默认值。配置发现包括系统目录、用户目录、程序目录和启动目录，不向上搜索父目录。相对路径按配置来源解析；读取不会修改进程环境。YAML 禁止别名、重复键、多文档和非字符串键。

```yaml
# net-connector.yml
netdisco:
  url: https://netdisco.example.invalid
```

起步配置只填写环境事实；并发、目录、筛选等按需覆盖。配置备份、TFTP 备份和拓扑任务共用 `backup.parallel`，需要时可用 `topology.parallel` 单独覆盖。初始化省略继承的默认值，明确设置的 false 和空筛选会保留。`config init --json` 返回实际创建的文件；`backup export --output 文件` 默认输出中文回执，可加 `--json` 获取元数据；直接导出到 stdout 始终只包含配置原文件。

程序名为 `netman`，所有用途只发现 `net-connector.yml`，默认初始化到 `~/.config/net-connector/net-connector.yml`，不再发现 `backup.yml` 或 `topology.yml`。文件内直接使用 `netdisco`、`inventory`、`ssh`、`backup`、`tftp`、`topology` 分组，不再使用 `common` 或用途下的重复嵌套。既有 `NC_`/`ND_` 环境变量继续有效。归档端部署与协议说明见 [tftp-archive](docs/tftp-archive.md)。

密码不放在 YAML。环境变量包括 `ND_USERNAME`、`ND_PASSWORD` 或 `ND_API_KEY`，以及 `NC_DEVICE_USERNAME`、`NC_DEVICE_PASSWORD`、`NC_ENABLE_PASSWORD`。使用 API key 且启用清单缓存时，需提供非敏感账户标识 `ND_USERNAME` 或 `ND_CACHE_NAMESPACE`，避免不同账户复用同一份缓存。设备凭据支持 `NC_<VENDOR>_USERNAME/PASSWORD/ENABLE_PASSWORD`，例如 `NC_H3C_PASSWORD`。显式通用 CLI 凭据覆盖对应厂商值。终端可用 `--ask-password`；自动化可用 `--stdin-credentials`，先发送一行凭据 JSON，执行备份或拓扑应用前再发送 `RUN`。

日志配置使用 `ssh.log_directory` / `NC_LOG_DIRECTORY` 与 `ssh.log_level` / `NC_LOG_LEVEL`。级别为 `debug`、`info`、`warn`、`error`；默认 `info`，只在 `debug` 记录完整脱敏回显。未指定日志目录时，普通配置备份不创建会话日志，TFTP 与拓扑使用批次目录下的 `logs`；显式目录覆盖这些默认值。

主机密钥策略 `replace` 必须显式指定 `ssh.known_hosts`，不会隐式替换用户默认信任文件中的密钥。

连接器自动检查命令响应的编码，对可无损识别的文本统一转换为 UTF-8；现有 Go API 调用与 CLI 无需增加编码参数。有效 UTF-8 原样保留，GBK/GB2312/GB18030 中文输出由 [chardet](https://github.com/saintfish/chardet) 提供候选，再由 `golang.org/x/text` 严格转换。检测在完整响应收齐后进行，分页和网络分包不会拆坏中文字符，终端退格/光标操作按原始字节处理后再转换。

`StepResult.Response().Raw()` 保留原始响应字节，`Response().Encoding()` 提供编码诊断。普通命令、`RunningConfig` 与拓扑解析仍严格拒绝不可解码输出。

Go API 默认 `Backup` 另提供保真阅读视图（CLI 使用无旁车的原字节保留模式）：按完整终端行保留有效 UTF-8，旧编码仅在检测与逐字节往返验证通过后转换；无法确认的源字节显示为大写 `\xNN`，不会把整份正常中文转为转义或用 `�` 代替未知字节。这是阅读视图，不是可自动恢复到设备的配置脚本。编码识别仍有歧义，文件 UTF-8 合法并不证明文字语义正确。

Go API 默认保真备份包含阅读文件与 `<备份路径>.<原文 SHA-256>.raw` 旁车，后者保留配置采集各步骤的原始响应，不包含认证对话。`BackupReceipt.source` 记录原文大小、哈希、步骤偏移及独立持久化回执；`encoding` 与 `text_warning` 记录编码及异常总数、最多 64 个原始字节位置（偏移从 0 开始，行/字节列从 1 开始）。原文先持久化，再原子保存阅读文件，两者权限均为 `0600`。采集完整、双文件持久化成功的编码异常计为成功并告警；截断、设备拒绝、超限和文件失败仍不能报成功。读取旧文件与未变化判断同样核验原文旁车。所有大小与转义膨胀仍受输出上限约束。

帮助、交互、运行进度、文本报告和日志解释统一中文；命令参数、环境变量、JSON 字段及状态码保持兼容，归档 worker 的正常 stdout 仍仅用于协议。发送命令、交互透传和原生 TFTP 文件不作转码。

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
if err = device.Connect(ctx); err != nil { return err }
result, err := device.RunningConfig(ctx)
if err != nil { return err }
configuration, ok := result.Config()
_ = configuration
_ = ok
```

`connector.New` 不连接设备；先登记 `Close`，再显式调用 `Connect`，连接失败时仍由调用方持有清理责任。`vendors.Open` 已移除。`ExecuteScript` 先校验完整脚本，再取得设备操作租约，操作并发或重入返回忙错误。每条命令共享发送、分页和交互的绝对期限；默认响应上限 32 MiB，也支持脚本累计预算。

常用包：

- `connector`：设备生命周期、命令/脚本、租约、回执、原文与渲染输出、配置采集和本地备份。
- `vendors` / `textfsm`：12 个厂商档案、19 个内嵌 TextFSM 模板、离线配置解析。
- `topology`：发现、描述审计、纯规划、确认后应用、读回验证和保存。
- `transfer`：原生 TFTP、Junos FTP/SCP、目标策略及设备报告回执。
- `archive`：本地/SSH 归档池、worker 协议和服务器核验。常规归档使用 `NewPool`；`Client` 是低层协议组合接口。
- `inventory` / `netdisco`：清单选择器、HTTP/PG 来源与缓存。
- `fleet` / `parallel`：通用计划、批次、报告及有界并发。
- `storage`：稳定路径锁、私有日志、原子写入与提交/持久化回执。
- `config`：不可变配置快照；`internal/bootstrap` 是唯一显式依赖注入组合根。

Go API 直接注入 `ConnectionConfig.Logger` 且不设置 `LogLevel` 时，保留底层脱敏回显写入行为；设置 `LogLevel` 后使用结构化事件，只有 `debug` 包含回显。CLI 始终使用配置中的日志级别。

操作失败时仍需检查返回结果：已完成的命令、已提交文件和已确认上传可能保留在部分回执中。库不会自动重放设备写入。`device_reported` 仅表示设备报告完成；`server_verified` 表示已读取并核验服务器文件。用户回调、日志 sink 和上传回调必须配合 context；库不以遗弃 goroutine 的方式模拟取消。

## 验证与边界

本轮改进、基准和平台验证见 [2026-10-29 优化记录](docs/2026-10-29-quality-verification.md)，公开 API 变更见 [迁移说明](docs/api-migration.md)。

首版发布流程与产物校验见 [发布说明](docs/releasing.md)，当前发布验证状态见 [发布验证](docs/release-verification.md)。

```sh
go test -race ./...
go vet ./...
GOTOOLCHAIN=go1.26.0 go test ./...
python3 scripts/verify-consumer.py
python3 scripts/verify-linux.py
```

来源和平台证据见 [验证记录](docs/verification.md)，模块和厂商对应关系见 [兼容矩阵](docs/compatibility.md)，所有权与失败边界见 [架构](docs/architecture.md)，实施中的兼容性与交付裁定见 [决策记录](docs/decisions.md)。本地假设备、PTY、HTTP/TLS、临时 PostgreSQL、localhost sshd 和安装后的 worker 是不同的证据层；这些结果不代表生产设备互通、远端 CI 或发布完成。

### TFTP 等待上限与每日归档

CLI 上传命令默认最多等待 180 秒，通过 `tftp.upload_timeout`（秒）或 `NC_TFTP_UPLOAD_TIMEOUT` 调整；不会在超时后自动重发上传命令。该默认与 Ruby TFTP 策略一致。Go API 的 `TFTPOptions.Timeout` 为零时同样沿用 180 秒，正值覆盖上传命令的等待上限。

服务器按 UTC+8 日期归档，例如 `/srv/tftp/archive/2026-11-04/设备名-IP.cfg`。同一天多次备份在核验成功后原子更新当天文件；上传失败保留原版本，不创建随机 SSH 子目录。历史日期与此前批次目录保持原位。本机批次报告仍独立保存。worker 必须提供 `daily_latest` 能力，预检失败时不会执行设备上传。

### 最小环境配置与统一结果展示

所有站点设置集中在 `net-connector.yml` 一处，包括清单服务、TFTP 服务器、归档根目录、设备例外与本机目录。空筛选、空 hosts 映射、缓存默认值和重复 SSH 设置无需填写。CLI 备份与 TFTP 使用一致的分区展示，输出备份方式、目录、文本报告及 JSON 报告；自动化继续使用 `--json --quiet`。成功策略可用 `backup.success_policy`、`NC_BACKUP_SUCCESS_POLICY` 或 `--success-policy` 覆盖，优先级遵循统一约定。

默认 H3C、Radware、Centec 采用 management 上传源，Huawei 默认源文件为 `flash:/startup.cfg`，NX-OS 默认 management VRF；环境不同仍可显式覆盖。Hillstone 路由域和逐设备例外保留为站点设置。

CLI 文件备份的完整性与文本编码质量分别统计。设备原生配置中截断的 UTF-8 不会被误猜为 GBK；保存文件标记 `original-bytes`，`text_warning` 给出异常字节位置，正文可能不是合法 UTF-8。TFTP 文件通过 SHA-256 核验也不表示原生文本编码合法。CLI 当前配置与厂商原生启动文件具有不同范围，不能仅按状态名或文件哈希直接判定结果矛盾。

### 查看默认值与归档 v2

`netman config defaults` 打印纯 YAML；`--json` 打印 JSON；`--for backup|tftp|topology` 选择命令的成功策略默认值。输出来自程序内置默认值，不读取本地配置、环境覆盖、凭据或清单。`config show` 则展示加载后的生效配置及来源。默认值不必写入站点文件，通常只维护实际环境差异；`.env` 仍只承载凭据等环境值。

归档准备/排队、设备上传、核验及本地提交、清理分别有 240/180/100/10 秒默认窗口。准备完成后才开始设备上传窗口；它仍受调用方总 context 约束。可选 `tftp.archive_prepare_timeout`、`tftp.upload_timeout`、`tftp.archive_verify_timeout`、`tftp.archive_cleanup_timeout` 按秒调整，环境变量分别为 `NC_TFTP_ARCHIVE_PREPARE_TIMEOUT`、`NC_TFTP_UPLOAD_TIMEOUT`、`NC_TFTP_ARCHIVE_VERIFY_TIMEOUT`、`NC_TFTP_ARCHIVE_CLEANUP_TIMEOUT`。所有默认值由归档定义统一生成，不重复维护。

`netman` 与 `tftp-archive` 必须同步升级到协议 v2，旧 worker 在设备连接前预检失败。清理失败不会撤销已核验和已持久化的文件，但批次仍不完整；可使用指定批次导出已保存文件。

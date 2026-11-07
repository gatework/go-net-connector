# tftp-archive

`tftp-archive` 安装在 TFTP 服务器上，由 `netman backup tftp` 通过 SSH 调用。它在设备上传前检查协议能力和目录权限，上传后核验文件是否为本次产生、稳定读取并计算 SHA-256，按日期归档，再将文件取回客户端。TFTP 网络请求由服务器上的 TFTP 服务接收。

```sh
tftp-archive --help
tftp-archive --version
```

直接在终端无参数运行会显示中文帮助；SSH 管道无参数调用使用归档协议 v2，stdout 只承载协议。无效参数通过 stderr 返回中文提示，退出码为 2。

默认安装路径为 `/usr/local/bin/tftp-archive`。管理员应安装与服务器平台匹配的二进制、校验哈希及版本，再执行协议探测和归档目录预检。客户端不会自动安装程序。已有自定义路径可继续通过 `tftp.ssh_worker_path` / `NC_TFTP_SSH_WORKER_PATH` 指定。

服务器归档为 `归档根目录/archive/YYYY-MM-DD/设备名-IP.cfg`，日期采用 UTC+8，同一天保留最后一次成功备份。厂商原生扩展名继续保留；失败上传不会覆盖已确认归档。历史文件与目录锁保持原位；客户端和 worker 必须在同一维护窗口升级到 v2，旧协议请求不再接受。

## 阶段期限与回执

| 阶段 | 默认秒数 | 范围 |
| --- | ---: | --- |
| 准备 | 240 | Pool 排队、通道、目录检查、目标锁、旧文件保留、ready |
| 上传 | 180 | 设备连接、源文件发现、原生上传 |
| 核验 | 100 | verify 请求、服务端归档、正文/尾帧、本地提交及目录同步 |
| 清理 | 10 | cleanup 请求与 complete 确认 |

首次控制帧及每条预检通道固定 10 秒。SSH master 建立保留原连接上限及有界并发；并不意味着整个多主连接 Pool.Prepare 只能用 10 秒。v2 每帧携带 `version: 2`；首次上传请求还携带 `timeouts` 中的 `prepare_ms`、`upload_ms`、`verify_ms`、`cleanup_ms`，均为正整数毫秒。准备预算传剩余值；版本和预算错误在文件操作前拒绝。probe/preflight 不要求上传预算。

客户端的各阶段 context 同时受父 context 约束。worker 在 ready 后开始等待上传，收到 verify 后进入核验；正文发送后等待本地提交，收到 cleanup 后才启用清理期限。远端等待阶段切换控制帧最多额外留 10 秒传输余量，不增加设备上传或本地提交时间。磁盘 Sync/Fsync 与借用回调是合作式取消，不能保证磁盘卡住时硬中断。

`archive.Result.Transfer` 只保存设备报告；`Receipt.Server` 保存归档路径、字节数、SHA-256 及正文核验结果；`Receipt.Local` 保留提交/持久化回执；`Receipt.Cleanup` 为 `not_requested`、`unconfirmed` 或 `complete`。未核验或本地未持久化时不请求清理，已经获得的证据不会被后续错误覆盖。

升级顺序：停止新批次并等待在途任务结束；在维护窗口安装 v2 worker 和配套 netman；运行 `--version` 与受控批次预检。不要用正在执行的设备上传验证版本兼容。新客户端拒绝 v1 worker，程序不会自动部署、降级或重放上传。

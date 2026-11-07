# tftp-archive

`tftp-archive` 安装在 TFTP 服务器上，由 `netman backup tftp` 通过 SSH 调用。它在设备上传前检查协议能力和目录权限，上传后核验文件是否为本次产生、稳定读取并计算 SHA-256，按日期归档，再将文件取回客户端。TFTP 网络请求由服务器上的 TFTP 服务接收。

```sh
tftp-archive --help
tftp-archive --version
```

直接在终端无参数运行会显示中文帮助；SSH 管道无参数调用保持归档协议 v1，stdout 只承载协议。无效参数通过 stderr 返回中文提示，退出码为 2。

默认安装路径为 `/usr/local/bin/tftp-archive`。管理员应安装与服务器平台匹配的二进制、校验哈希及版本，再执行协议探测和归档目录预检。客户端不会自动安装程序。已有自定义路径可继续通过 `tftp.ssh_worker_path` / `NC_TFTP_SSH_WORKER_PATH` 指定。

服务器归档为 `归档根目录/archive/YYYY-MM-DD/设备名-IP.cfg`，日期采用 UTC+8，同一天保留最后一次成功备份。厂商原生扩展名继续保留；失败上传不会覆盖已确认归档。更名不迁移历史文件，不更改 Go 包名、协议字段或目录锁；升级时保留旧路径程序可让尚未切换的客户端继续工作。

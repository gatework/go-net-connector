# Ruby → Go 兼容矩阵

基线为冻结工作区，包括当时已修改及未跟踪源码。Git HEAD、文件哈希和 Ruby 测试记录在 `baseline/`；不以当前原始目录状态替代基线。

| 厂商 | 配置采集 | 保存 | TFTP | 邻居/描述 | 自动描述修改 |
| --- | --- | --- | --- | --- | --- |
| h3c / h3c_wireless | 当前配置，rule-ID hook | 是 | startup/saved file | 是/是 | 是 |
| cisco_ios / cisco_nxos | running config | 是 | running | 是/是 | 是 |
| huawei | 当前配置，su | 是 | 指定源文件 | 是/是 | 否 |
| hillstone | running | 是 | startup dat | 是/是 | 是 |
| radware | dump，HA 状态 | 是 | native tgz | 否/是 | 否 |
| palo_alto | 候选前后差异检查、set 输出 | 否 | 固定 running-config.xml | 是/是 | 否 |
| topsec | running nostop | 否 | 否 | 否/否 | 否 |
| fortinet | 当前授权 VDOM 的完整配置 | 是 | full-config | 是/是 | 否 |
| juniper | 已提交配置 set 输出 | 否 | 否；FTP/SCP | 是/是 | 否 |
| centec | running | 是 | running | 否/否 | 否 |

Go 包采用直接命名：Ruby Device/Engine → `connector`，Vendor → `vendors`，Interfaces → `topology`，Tftp/RemoteBackup → `transfer`，Netdisco SSH Archive → `archive`。`vendors` 避免与 Go 的依赖 `vendor/` 目录冲突。

保留默认超时、命令字节、回复、确认/错误优先级、原生文件扩展名和固定文件名限制。`testdata/ruby-contracts.json` 由冻结 Ruby 代码离线生成，Go 测试直接比较 12 个 profile 的采集/保存命令、超时、提权命令及全部 TFTP 命令和回复。19 个 TextFSM 模板及索引按原字节内嵌。

API 采用 Go 的 context、组合和类型化结果；不模拟 Ruby 动态加载或异常语法。已报告完成与服务器核验、完成命令与持久化文件、原始结果与脱敏诊断均维持独立。无自动 commit、回滚、重新上传或发布。

当前构建依赖 go.mod 固定的 GitHub go-expect 版本，使用 GOWORK=off，不使用历史快照或本地 replace。scripts/verify-consumer.py 在仓库之外复制当前源码，验证公开 API、模板与 CLI；它不代表当前未发布源码已能被外部版本化消费者获取。

# 厂商方言与平台接入

`vendors` 把设备清单身份、设备 CLI 规则和可选业务能力连接起来。每个方言对应一个规范平台键，例如 `h3c_wireless` 或 `cisco_nxos`。Registry 查找依赖 `vendors/dialect` 叶子合约；工作流通过各自消费端接口检查能力，不在 `fleet`、`topology` 或 `transfer` 中维护厂商命令分支。

## 一次任务如何找到设备行为

```text
Inventory 设备厂商/系统/型号
  → fleet 应用主机覆盖、设备规则、厂商覆盖
  → Registry 的内建身份识别
  → Registry.Lookup(规范平台键)
  → Platform.Profile() 建立独立会话配置
  → 消费端定义的可选拓扑、TFTP、远程备份行为接口
  → workflow 汇总设备结果与证据
```

提示符、登录交互、分页、命令错误、配置采集、保存确认和传输完成标记都属于设备行为，由方言家族包负责。共享 workflow 负责选择、并发、会话生命周期、路径与结果汇总。

## Go 命名约定

包内名称使用家族包、接收者和能力接口已经提供的上下文，不重复厂商前缀；平台类型导出为 `Dialect`；多个平台变体只保留区分它们的名称，例如 `cisco.IOSDialect`、`cisco.NXOSDialect` 和 `h3c.WirelessDialect`。配置采集使用 Go 常见缩写 `Config`。跨包能力名保持描述准确，只有上下文已充分表达时才省略限定词。导出 API 直接迁移并更新调用方，不添加旧名转发别名；CLI、配置字段和协议字面量保持既有名称。

## 目录职责

- `dialect/`：稳定的 `Platform`、`Registry` 叶子接口和跨包数据类型，不定义 workflow 能力接口。
- `registry.go`：唯一的静态内建目录、平台键和内建身份匹配优先顺序。
- `<family>/`：每个厂商家族直接位于 `vendors` 下，集中放置该家族的方言、命令、提示符、清单匹配及实际支持的能力策略。文件按业务职责拆分，不为统一目录形状添加空文件。
- `internal/driver/`：跨厂商共享的 Profile、配置采集、拓扑委托、保存回执校验和 TFTP 骨架。这里不放某个厂商专属命令或提示符。
- `internal/topology/`：厂商拓扑解析共用的 TextFSM 执行、字段映射、引号解码和重复冲突检查。命令、模板选择、计数规则及完整性策略放在对应家族包。
- `../internal/ifname/`：拓扑匹配和配置命令共用的接口名称规范化实现。
- `../textfsm/templates/`：TextFSM 模板及索引。模板路径由所属平台拓扑策略声明。

家族包可由外部代码直接导入；`Registry` 仍负责集中提供内建家族目录和清单识别顺序。`vendors/internal` 只容纳不应成为外部 API 的共享实现。

| 家族包 | 规范平台键 |
| --- | --- |
| `centec` | `centec` |
| `cisco` | `cisco_ios`, `cisco_nxos` |
| `fortinet` | `fortinet` |
| `h3c` | `h3c`, `h3c_wireless` |
| `hillstone` | `hillstone` |
| `huawei` | `huawei` |
| `juniper` | `juniper` |
| `paloalto` | `palo_alto` |
| `radware` | `radware` |
| `topsec` | `topsec` |

H3C/Wireless 和 IOS/NX-OS 在家族包内共享稳定规则，同时保留各自不同的平台具体类型和平台覆盖。

## 内建平台能力矩阵

矩阵来自 `Profile` 命令声明及消费端能力接口断言；它描述源码当前声明的支持范围，不代表实机兼容性。配置列表示 CLI 配置采集策略；“保存命令”表示 Profile 声明通用保存脚本，成功仍需正向保存回执。拓扑按发现、接口描述读取、接口身份映射和描述写入分别列出。

| 平台 | 配置采集 | 保存命令 | 邻居发现 | 描述读取 | 接口映射 | 描述写入 | TFTP | 原生远程备份 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Centec | 是 | 是 | — | — | — | — | 是 | — |
| Cisco IOS | 是 | 是 | 是 | 是 | 是 | 是 | 是 | — |
| Cisco NX-OS | 是 | 是 | 是 | 是 | 是 | 是 | 是 | — |
| Fortinet | 是 | 是 | 是 | 是 | 是 | — | 是 | — |
| H3C | 是 | 是 | 是 | 是 | 是 | 是 | 是 | — |
| H3C Wireless | 是 | 是 | 是 | 是 | 是 | 是 | 是 | — |
| Hillstone | 是 | 是 | 是 | 是 | 是 | 是 | 是 | — |
| Huawei | 是 | 是 | 是 | 是 | 是 | — | 是 | — |
| Juniper | 是 | — | 是 | 是 | 是 | — | — | 是 |
| Palo Alto | 是 | — | 是 | 是 | 是 | — | 是 | — |
| Radware | 是 | 是 | — | 是 | 是 | — | 是 | — |
| Topsec | 是 | — | — | — | — | — | — | — |

`vendors/profiles_test.go` 固定验证平台命令、保存脚本和能力矩阵；`vendors/topology_capability_contract_test.go` 单独验证读写能力和厂商命令生成。新增平台或改变能力时，应先更新这两处行为约束，再更新本表。能力未声明时，Topology/Transfer 在计划或预检阶段拒绝操作，不使用空实现模拟成功。

## 新增或扩展平台

1. 先确认权威的平台键、厂商/OS/型号字段和 Ruby 对照行为。没有可靠证据的能力先不声明。
2. 在现有家族包中新增平台类型；只有业务规则确实属于不同家族时才新建包。构造函数返回导出的具体平台类型（满足 `dialect.Platform`），使调用方可以命名该类型并断言能力接口。读取类拓扑能力用 `driver.NewTopologyReader` 组合；共享 Profile 基础项及 `driver.PositionMarkerCommandErrors`、`driver.YesNoConfirmations` 等按 CLI 机制命名的规则，平台差异留在当前家族包。
3. 在 `inventory.go` 实现平台私有的 inventory matcher。Registry 会将厂商、OS、型号统一转换为小写 ASCII 词组后再调用；保留 `registry.go` 的平台声明顺序，以免重叠标签改变既有归属。fleet 消费 `fleet.PlatformResolver`。
4. 只实现设备实际支持的消费端能力接口：拓扑接口定义在 `topology`，传输接口定义在 `transfer`。配置采集策略负责提示符切换、结果选择和文本清理；保存成功必须匹配正向签名，单独看到命令提示符不算成功。
5. 若支持拓扑，在家族 `topology.go` 组装模板、记录计数、配置校验、接口规范化及写入模式；把模板放入对应目录并补足完整/空结果/异常输出夹具。
6. 若支持 TFTP 或远程导出，在家族传输文件中声明参数校验、源文件发现、命令交互、完成证据和回执元数据。工作流超时、路径安全及服务端核验仍由 `transfer` 负责。
7. 将规范键及内建优先顺序加入 `registry.go`，并加入匹配、Profile 隔离、Ruby parity 和能力边界测试。
8. 执行 `go test ./vendors/... ./fleet ./topology ./transfer` 与 `go test ./...`；不得把静态夹具测试描述成真实设备验收。

内建目录是编译期静态组合，不加载运行时插件。通常增加平台只需家族包、Registry 注册、模板和测试，不需要在各个 workflow 重复写厂商分支。

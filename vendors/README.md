# 厂商平台接入

`vendors` 把设备清单身份、设备 CLI 规则和可选业务能力连接起来。每个平台对象对应一个规范平台键，例如 `h3c_wireless` 或 `cisco_nxos`。Registry 查找依赖 `platform` 叶子合约；工作流通过各自消费端接口检查能力，不在 `fleet`、`topology` 或 `transfer` 中维护厂商命令分支。

## 一次任务如何找到设备行为

```text
Inventory 设备厂商/系统/型号
  → fleet 应用主机覆盖、设备规则、厂商覆盖
  → Registry 的内建身份识别
  → Registry.Lookup(规范平台键)
  → connector.New(platform, …)：平台对象即设备驱动（Terminal + 行为）
  → 消费端定义的可选拓扑、TFTP、远程备份行为接口
  → workflow 汇总设备结果与证据
```

提示符、登录交互、分页、命令错误、配置采集、保存确认和传输完成标记都属于设备行为，由平台家族包负责。共享 workflow 负责选择、并发、会话生命周期、路径与结果汇总。

## Go 命名约定

包内名称使用家族包、接收者和能力接口已经提供的上下文，不重复厂商前缀；平台类型导出为 `Platform`；多个平台变体只保留区分它们的名称，例如 `cisco.IOSPlatform`、`cisco.NXOSPlatform` 和 `h3c.WirelessPlatform`。配置采集使用 Go 常见缩写 `Config`。跨包能力名保持描述准确，只有上下文已充分表达时才省略限定词。导出 API 直接迁移并更新调用方，不添加旧名转发别名；CLI、配置字段和协议字面量保持既有名称。

## 目录职责

- `../platform/`：稳定的 `Platform`、`Registry` 叶子接口、清单身份规范化和跨包数据类型，不定义 workflow 能力接口。
- `registry.go`：唯一的静态内建目录、平台键和内建身份匹配优先顺序。
- `<family>/`：每个厂商家族直接位于 `vendors` 下，集中放置该家族的平台对象、命令、提示符、清单匹配及实际支持的能力策略。文件按业务职责拆分，不为统一目录形状添加空文件。
- `internal/driver/`：跨厂商共享的 `Base`（Terminal、配置采集工厂、保存确认）、`TopologyReader`、配置采集、拓扑委托、保存回执校验和 TFTP 骨架。这里不放某个厂商专属命令或提示符。
- `internal/topoparse/`：厂商拓扑解析共用的 TextFSM 执行、字段映射、引号解码和重复冲突检查。命令、模板选择、计数规则及完整性策略放在对应家族包。
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

矩阵由代码生成：运行 `netman platform list`（或 `--json`）。每一列与对应阶段的准入判断使用同一个接口断言，因此不会与批次实际准入的结果不一致；它描述源码声明的支持范围，不代表实机兼容性。“保存”表示平台声明了保存脚本，成功仍需正向保存回执。

`vendors/platforms_test.go` 固定验证平台命令、保存脚本和能力矩阵；`vendors/topology_capability_contract_test.go` 单独验证读写能力和厂商命令生成。新增平台或改变能力时，应先更新这两处行为约束，再更新本表。能力未声明时，Topology/Transfer 在计划或预检阶段拒绝操作，不使用空实现模拟成功。

## 新增或扩展平台

1. 先确认权威的平台键、厂商/OS/型号字段和实际设备行为（提示符、错误、分页、保存回执样本）。没有可靠证据的能力先不声明。
2. 在现有家族包中新增平台类型；只有业务规则确实属于不同家族时才新建包。构造函数返回导出的具体平台类型（满足 `platform.Platform`），使调用方可以命名该类型并断言能力接口。平台类型嵌入 `driver.Base`（由 `driver.BaseTerminal` + `driver.NewBase` 构造），读取类拓扑能力用 `driver.NewTopologyReader` 组合；需要代码的厂商行为写成平台方法，例如 H3C 的 `PrepareCommand/AfterCommand`、Radware 的 `AfterLogin`。共享 `driver.PositionMarkerCommandErrors`、`driver.YesNoConfirmations` 等按 CLI 机制命名的规则，平台差异留在当前家族包。
3. 在 `inventory.go` 实现 `MatchInventory`（`platform.Platform` 的一部分）。Registry 会将厂商、OS、型号统一转换为小写 ASCII 词组后再调用；保留 `registry.go` 的平台声明顺序，以免重叠标签改变既有归属。fleet 消费 `fleet.PlatformResolver`。
4. 声明平台只读命令：嵌入 `driver.ReadOnlyCommands{Verbs: …}`（路径式菜单用 `PathPrefixes`），供 `netman exec` 在连接前判定；未列入的命令一律视为可能写入。
5. 只实现设备实际支持的消费端能力接口：拓扑接口定义在 `topology`，传输接口定义在 `transfer`。配置采集策略负责提示符切换、结果选择和文本清理；保存成功必须匹配正向签名，单独看到命令提示符不算成功。
6. 若支持拓扑，在家族 `topology.go` 组装模板、记录计数、配置校验、接口规范化及写入模式；把模板放入对应目录并补足完整/空结果/异常输出夹具。
7. 若支持 TFTP 或远程导出，在家族传输文件中声明参数校验、源文件发现、命令交互、完成证据和回执元数据。工作流超时、路径安全及服务端核验仍由 `transfer` 负责。
8. 将规范键及内建优先顺序加入 `registry.go`，并加入匹配、Terminal 副本隔离、设备交互 golden 和能力边界测试，并在家族 `capabilities_test.go` 声明所实现的能力接口。
9. 执行 `go test ./vendors/... ./fleet ./topology ./transfer` 与 `go test ./...`；不得把静态夹具测试描述成真实设备验收。

内建目录是编译期静态组合，不加载运行时插件。通常增加平台只需家族包、Registry 注册、模板和测试，不需要在各个 workflow 重复写厂商分支。

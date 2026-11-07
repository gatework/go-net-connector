# 2026-11-05 厂商方言与 Registry 重构

## 目标与边界

保留现有 12 个平台键、Ruby 对照命令、交互回复、设备状态机、解析结果、TFTP 回执和 capability 矩阵。把散落的厂商脚本收敛为具体方言对象和可选能力接口；不增加运行时插件加载，不对设备执行命令，不提交或发布。

## 实施内容

1. 在 `vendors/dialect` 定义不依赖 workflow 的 `Dialect`、`Registry` 和共享 DTO；Topology 与 Transfer 的可选行为接口由各自消费者拥有。内建 Registry 是静态且不可变的；每次 `ConnectorProfile` 返回可独立修改的副本。H3C/Wireless 与 IOS/NX-OS 保留家族复用和平台覆盖，同时使用不同具体类型。
2. 按厂商家族包组织连接提示符、分页、命令错误、自动交互、采集/保存命令及 capability 声明。H3C 命令 hook 归 H3C；Radware HA 登录证据归 Radware。配置采集采用共享会话/结果校验器加独立家族策略；Cisco 清理、Hillstone 分页、Radware 模式切换和 PAN 候选差异/配置格式分别由各自策略负责。
3. 将保存确认签名放回平台声明。`vendors/internal/driver` 负责公共回执检查；没有已知成功签名的平台继续返回未确认，避免猜测成功。
4. 把拓扑模板、邻居计数、配置完整性校验和接口规范化装配成每个家族方言自己的 parser policy。`vendors/internal/topology` 的 `topology.Parser` 只负责通用 TextFSM 执行、结果映射和重复冲突检查；Fortinet 固件/VDOM 命令选择由 Fortinet 方言持有。接口规范化代码共享于 `internal/ifname`。
5. 把 TFTP 目标校验、来源文件发现、命令/交互、完成签名、归档根目录要求、同路径复用和回执字段拆为平台 TFTP 对象；Juniper 远程配置导出也使用可选方言能力。传输协调、路径语法、通用失败优先级仍由 `transfer` 管理。各完成签名在构造时编译，避免每台设备的完成扫描重复编译正则。
6. 让 `fleet.NewRules`、Bootstrap、Topology 构造/纯计划 API、TFTP 与远程备份 API 显式接收 Registry。只读拓扑写入限制由方言提供错误分类。删除包级 `vendors.Profile` 与 `vendors.Names`，调用方统一经 `Registry.Lookup` 取得 Dialect。

7. 将内建厂商/OS/型号识别实现为 Registry 私有 matcher，由 Registry 保留原先优先级；fleet 只依赖自己的 `InventoryResolver` 接口，并按主机覆盖、设备映射、厂商覆盖、内建识别的顺序决策。

## 2026-11-06 能力边界跟进

- 删除 `connector.Profile.Capabilities` 这份与行为接口重复的状态。Topology、TFTP、远程备份和 inventory 能力现在通过消费端接口判断；Fleet 的备份命令仍由 Profile 的真实配置命令声明。
- 将 topology 拆为邻居发现、描述读取、接口身份、描述写入和写入限制五个小接口。Radware 只组合其有依据的描述读取与身份行为；共享计划/执行器保留重验、写入顺序、读回和保存责任，厂商 writer 仅生成描述变更命令。
- 将内建平台列表作为唯一顺序来源，同时生成名称查找表和 inventory 匹配顺序。具体家族 struct 组合自己的策略类型，不依赖 `transfer` 接口包；Go 的结构化接口满足消费者契约，避免 vendor→consumer import cycle。
- 为 vendor 解析边界增加稳定诊断元数据，Topology 将通用解析失败映射为 `unrecognized_output`，保留 Fortinet `vdom_context_mismatch` 与接口冲突 `ambiguous_description`；不把设备输出或解析正文放入错误文本。
- 定向测试覆盖平台行为集合与变更命令；bench 目前只有迁移后的结果，没有可复用的迁移前同机基线，因此不宣称定量性能提升。测试命令和数据见本轮交付记录。

## 验收与限制

- 现有 Ruby profile/TFTP 契约、会话夹具、解析夹具、配置收尾和回执测试覆盖原行为。
- `go test ./...` 通过。第一轮全量回归捕获了 Cisco IOS 邻居计数正则未登记的问题；恢复 IOS 与 NX-OS 各自原有的记录签名后，拓扑定向测试和全量测试均通过。
- 只读代码审查补出 TFTP 预检遗漏 Registry、无拓扑方言的 `BuildAudit` nil 解引用，以及 workflow/transfer/fleet/topology 仍有少量平台名分支；均通过回归用例修复，并将源文件、归档根目录、目标复用、拓扑只读错误分类及 Juniper 远程导出迁入方言能力。
- 本轮未连接设备、运行真实批次、生成仓库内二进制、提交或发布。性能收益只确认了每次 TFTP 完成判断不再重新编译固定正则，未据此推断批次总耗时变化。

## 家族包与接入文档

家族包直接位于 `vendors/<family>`，包内按业务职责放置方言和可选能力；共享执行骨架集中在 `vendors/internal/driver`。共享拓扑解析位于 `vendors/internal/topology`，不保存厂商计数规则或 Fortinet 命令选择。新平台的业务链路和检查清单见 [`vendors/README.md`](../vendors/README.md)。

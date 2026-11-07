# Go 模块设计与业务抽象改进方案

审查日期：2026-11-07

范围：`go-net-connector` 当前工作区中的生产 Go 代码、模块依赖、业务流程及项目文档。
性质：审查与实施方案；本轮未修改业务代码、未运行测试、未连接设备、未提交或发布。

## 1. 审查结论

项目已经形成了可继续演进的架构基础：`connector.Device` 持有设备会话和操作租约；厂商包直接位于 `vendors/<family>`；可选拓扑和传输能力由消费端接口表达；Fleet 有界并发并保留清单顺序；本地文件与远端归档分别核验；配置和计划阶段不要求建立设备连接。这些边界应作为后续重构的约束，不应推倒重来。

当前最值得优先处理的不是全仓改成“类”，也不是按文件行数机械拆分，而是：

1. **把组合根从业务工作流依赖中移出。** `internal/workflow` 直接接收 `*bootstrap.Services`，并调用其设备、日志等装配方法；`bootstrap` 因此既是依赖构造处，又成为工作流运行时服务对象。
2. **把批次状态与报告格式变成显式业务契约。** Fleet 用字符串表示运行模式和状态，运行时按 backup/TFTP 分支解释产物；报告由 `map[string]any` 组装，`internal/batchreport` 又独立解码相同 JSON。状态规则与 JSON 字段存在漂移风险。
3. **把配置默认值、解析、校验和生效配置收敛成单一边界。** `config.Defaults` 与 `bootstrap.resolveSettings` 都保存默认回退值；`Services` 仍在多个时点按字符串路径从通用 map 取值。
4. **按对象所有权审查核心生命周期。** `connector.Device`、Fleet 批次、拓扑应用、Transfer 回执、Archive Pool/Client/worker 都有状态和资源，但状态有些仍以自由字符串、布尔组合、裸切片或函数参数传递。先明确谁创建、谁验证、谁关闭、谁持有回执，再决定是否抽象为对象。
5. **校正实施文档与源码现状。** `docs/implementation-plan.md` 仍记录旧 CLI 二进制及旧默认差异；这些条款已被 `docs/decisions.md` 的 2026-11-03 CLI 决定覆盖。`docs/progress.md` 最新命名审查写 36 个包，而当前 `GOWORK=off go list ./...` 得到 37 个包；当前统计为 161 个非测试 Go 文件、19,654 行生产 Go 代码。审查依赖源码与现行决定，不把历史计划误当现状。

Go 中的面向对象应落实为**对象持有不变量与生命周期、方法表达对象行为、组合表达变化、接口由消费方定义**。无状态转换仍用函数；不为“面向对象”引入继承式层级、过宽接口、服务定位器或只有一个方法的包装类型。

## 2. 当前模块职责与逐模块改进方向

| 模块 | 当前职责与审查判断 | 改进方向 |
| --- | --- | --- |
| `cmd/netman`、`cmd/tftp-archive` | 两个薄入口，分别建立信号上下文并委托 CLI/worker。 | 继续只做进程边界：参数入口、信号、退出码；业务配置、平台规则和任务状态不放入 `main`。 |
| `internal/buildinfo` | 构建版本与提交身份的单一读取点。 | 保持小而稳定；构建变量及未知/开发版本的呈现规则只在此定义，不让命令自行拼版本字符串。 |
| `internal/cli` | Cobra 命令树、配置加载、交互、密钥读取、流程选择和终端输出集中在 `Runtime`；`cli.go` 编排较多阶段。 | 将命令解析结果转成明确的应用请求；把 setup/config、plan、execute、render 分成窄的包内职责。命令层只决定调用哪个用例，不重新实现业务校验或状态规则。 |
| `internal/workflow` | 统一配置备份、TFTP、topology 的应用流程与报告/进度收尾。职责方向合理，但导入 `internal/bootstrap` 并使用 `*bootstrap.Services`，违反组合根应位于外层的依赖方向。 | 定义工作流消费的窄依赖（例如设备打开器、归档池工厂、不可变运行设置），由 CLI/组合根构造并注入。不要把 bootstrap 的通用 `Services` 作为流程 API；也不要为每个函数再造一层冗长 `Dependencies` 容器。 |
| `internal/bootstrap` | 构造 registry、Fleet rules、设备 profile/config、凭据解析、库存来源/cache、TFTP policy 和运行设置。当前 `Services` 同时像装配结果、运行时服务对象和配置仓库。 | 保留唯一 composition root，但产出按用例可直接消费的不可变依赖；将 inventory source 创建、device opener 创建和配置解码分别设为内部构造职责。密钥只在相应工厂读取，服务对象不暴露整份配置或无关能力。 |
| `config` | 多层配置来源、YAML/env 解析、默认值、初始化、来源追踪和校验。`Snapshot` 的不可变拷贝与 provenance 是正确基础。 | 继续承担语法、来源合并和脱敏；在边界一次性转成领域设置。去除与 bootstrap 重复的 fallback；默认值、类型范围、跨字段约束应有一个明确拥有者。配置键和 YAML/环境变量名称视为公开契约，不因 Go 内部重命名而改动。 |
| `connector` | `Device` 管连接、命令执行、提示符、超时、生命周期关闭和 operation lease；`Profile`/Command/Result 定义设备操作契约。它是核心资源所有者，应保持能力强但边界明确。 | 梳理 `Device` 状态机及公开方法的前置/后置条件；把 Profile 中属于厂商策略的行为留在厂商组合，把会话/传输/回执通用规则留在 connector。运行模式和回执状态使用具名类型；保留部分写入、提示符完成、设备拒绝、持久化确认之间的证据差异。不可因拆文件破坏单一 reader、单一 lease 和取消语义。 |
| `vendors` | 静态内建目录，集中身份解析；方向与 `vendors/README.md` 一致。 | 保留编译期 Registry，不做运行时插件系统。Registry 只负责身份映射、键查找与稳定优先级；不得成为厂商行为总线或配置容器。 |
| `vendors/dialect` | 稳定的平台、注册表、身份和跨模块 DTO 叶子契约。 | 严格控制依赖和增长；只放跨包稳定数据/基础契约，拓扑和 TFTP 工作流接口仍由各消费包定义。避免能力接口汇总成巨型 Platform。 |
| `vendors/internal/driver` | 多家厂商共享 Profile、配置采集、保存证明、拓扑委托与 TFTP 执行骨架。 | 每段共享逻辑必须有多个真实调用者和相同业务不变量。用组合委托平台差异；禁止把厂商命令、提示符和错误签名搬入通用层。针对配置采集与 TFTP 各自检查参数对象是否过宽，以及 nil/默认回退是否掩盖遗漏。 |
| `vendors/internal/topology` | TextFSM 调用、字段抽取、通用行/引号处理及结果检查。 | 只保留语义相同的通用解析原语；平台模板、命令、记录数与配置完整性策略继续属于平台家族。`internal` 不再套 `platform`/厂商名字目录。 |
| `vendors/centec` | 单平台 Profile、身份、配置/TFTP 行为，能力较少。 | 用能力矩阵明确实际支持范围；unsupported 必须在预检阶段可解释，不通过空策略或默认成功实现接口。 |
| `vendors/cisco` | IOS/NX-OS 共用家族规则并保留平台差异。 | 明确共同提示符/交互/配置流程与平台专属差异；避免平台变体 switch 在共享 driver 扩散。身份、能力和覆盖顺序从同一注册声明核对。 |
| `vendors/fortinet` | VDOM、版本、配置/TFTP 与部分拓扑行为。 | 把 VDOM 选择及命令回执作为可审计策略；明确 capability 不支持与输出不可识别的区别，避免用通用解析错误吞掉 Fortinet 具体业务诊断。 |
| `vendors/h3c` | H3C 与无线平台的共享配置/交互/TFTP/拓扑规则。 | 核对两种平台共用的规则是否确实相同；保留无线差异为具体策略。避免按平台重复拷贝命令流程；设备变更保存只接受明确正向证据。 |
| `vendors/hillstone` | 会话、配置和拓扑/TFTP 能力。 | 固定 VRF、命令与描述写入策略归属；和 `topology` 的能力声明保持一致；逐条检查命令模板是否同时被 plan 与实际执行共享。 |
| `vendors/huawei` | 登录/提权、配置、接口映射和拓扑/TFTP 策略。 | Profile 只承载会话策略；配置清理/接口身份/拓扑模板各自有单一实现；不在 connector 中加 Huawei 特判。 |
| `vendors/juniper` | Junos 配置及远程备份/TFTP 行为。 | 将配置读取和 FTP/SCP 远程导出分成准确能力；保留“设备报告完成”和“服务器验证完成”的差别；任何保存或提交行为必须显式且有正向回执。 |
| `vendors/paloalto` | 候选配置、前后差异、配置采集/TFTP 行为。 | 继续把 candidate isolation 与通用配置回执分开建模；候选配置无法隔离时明确失败，不用泛化流程推测成功。 |
| `vendors/radware` | HA/身份、配置、描述读取及受限传输能力。 | 能力接口逐项反映真实支持项，不因实现邻居读取便暗示支持写描述；主备状态证据不得和配置结果混为一体。 |
| `vendors/topsec` | 精简 Profile 与有限能力。 | 保持精简；能力不支持应在 plan/preflight 显示，不添加仅为目录整齐而存在的空文件或空策略。 |
| `fleet` | 设备识别/选择/计划、有界并发、Outcome/Batch、诊断与 Report 汇总。该包承载主要业务状态。 | 统一 `Mode`、计划状态、运行状态、批次状态为明确类型；把“是否成功/部分成功/已尝试”的判定集中在一个转换规则中；将 backup/TFTP 产物解释作为明确策略，而不是在多个统计/诊断函数各自重算。报告保持 schema 兼容，改变先于实现做 schema 映射表。 |
| `parallel` | 通用、泛型、有界并发 Map。 | 仅处理调度、取消、worker 退出，不了解设备、凭据或 Fleet 状态；保持为无状态函数，不因 OOP 要求改成池对象。 |
| `inventory` | 清单设备值、字段规范化、筛选和地址集合。 | 维持纯逻辑包；明确地址规范化和重复主机识别只有一个权威实现。避免把来源 HTTP/DB 细节引入 inventory。 |
| `netdisco` | HTTP、PostgreSQL 来源与缓存，均有资源/请求生命周期。 | 让每种 Source 自己持有请求客户端/连接和 limits；Cache 明确 fresh/stale/disabled 结果状态。配置选哪个来源、凭据从哪里来由 composition root 决定；Postgres query 参数与 HTTP 分页策略不泄漏到 Fleet。 |
| `topology` | 发现、当前描述、变更计划、审计、确认后执行、读回和保存。 | 将 plan/audit/apply 阶段的前置条件与证据建模一致；`Plan`/`Audit` 内公开 slice/map 的可变性需要审查，优先以构造器验证和防御性复制保障 apply 输入稳定。写入、验证、持久化结果分字段表达，不把 `Complete()` 当成所有成功语义。 |
| `transfer` | TFTP/远程导出选项验证、平台策略调用、完成证据解析及 Receipt 生成。 | 分清设备端命令完成、路径核验、服务器端验证和本地文件提交；让 Receipt 拥有明确不可变语义。运行器只负责通用阶段与截止时间，厂商 policy 负责命令和证据语法；不重复 Fleet 诊断/归档协议职责。 |
| `archive` | 本地/SSH Archive Pool、Client、控制协议与 worker 安全文件事务。多个对象分别拥有并发池、通道与单个目标事务，所有权方向合理但关闭/取消状态复杂。 | 建立 Pool 与 Client 生命周期图后再拆职责。Pool 管并发槽位、预检和关闭；Client 管单目标事务/通道；worker 协议解析与目录 FD 文件操作分开。用显式状态或集中转换替代互相矛盾的 bool 组合，仅在锁内证明状态不变量后实施。保持“不重传不确定写入、旧目标失败时保留、双方核验后才清理”。 |
| `storage` | 原子写入、批次目录、读取/导出和安全路径。当前多数 API 是无状态文件操作。 | 先按生命周期确认是否存在可复用的根目录对象；如果调用只有一次，不为面向对象强制创建 `Store`。统一 Receipt/状态枚举和绝对路径/dirfd 校验，不把归档服务器协议混进本地存储。 |
| `internal/batchreport` | 报告 schema 版本与导出所需 artifact 选择。 | 成为报告持久化 DTO 的唯一权威：schema 类型、字段可空规则、版本、设备唯一键和已验证 artifact 选择集中声明。Fleet 负责映射业务对象，storage 只消费契约；避免一个包一份相似 JSON struct 和 `map[string]any`。 |
| `textfsm` | 通用 TextFSM 模板装载/解析和只读模板缓存。 | 保持 parser 状态隔离：编译模板可共享，解析值/输出每次独立。模板查找错误和无匹配输出分开。厂商标识仅用于模板索引，不加入厂商业务分支。 |
| `internal/ifname` | 接口名的 canonical/key/short/config-name 转换。 | 保留为单点纯函数包；用调用场景定义函数含义，不把厂商别名规则复制到 topology 和 vendor 代码。仅在确认 `ConfigName` 等术语与外部行为差异后再改名。 |
| `internal/terminal` | 设备输出的 ANSI/控制字符/编码后可见文本渲染，与 CLI TTY 生命周期不同。 | 通过调用方图确认命名含义；考虑将包名明确为设备终端文本处理，但必须先证明不会造成 import churn/更坏缩写。保留 raw bytes 与 rendered view 的边界。 |
| `internal/textcodec` | 编码探测、文本转换和备份/计划视图。 | 维护原字节、解码正文、可见文本三层契约；将“未知编码”留作可核实证据，不自动吞掉或替换。避免端到端重复解码/渲染。 |
| `internal/display` | 中文状态、错误摘要、表格和列宽输出。 | 保持纯展示函数；业务状态不依赖中文显示码。集中检查显示字典与 Fleet 已知诊断码的一致性，机器 JSON 与用户文案分离。 |
| `internal/progress` | 固定位置的终端进度面板、刷新和异常消息。 | Reporter 继续拥有 ticker/终端刷新生命周期；事件只携带安全元数据。普通事件不得触发新增行；取消时等待刷新 goroutine 有界退出。业务计数由 Fleet/Workflow 产生，CLI 只格式化。 |
| `internal/cli` 的 `terminal.go` | CLI 输入、终端检测、密码提示、交互初始化。 | 与设备 `internal/terminal` 术语区分；stdin/TTY 所有权由 Runtime 明确传入，非交互流程不隐式等待输入。 |
| `internal/display` + `fleet` 诊断 | 错误码 allowlist/中文错误映射/JSON 字段映射跨包维护。 | 错误码常量归属错误产生的领域包；展示包只映射稳定 code。增加覆盖性检查作为后续验收，不建立一个无边界的全局 errors 包。 |
| `internal/terminal`、`internal/textcodec`、`internal/display` | 共同处理设备输出和 CLI 呈现，但输入输出边界不同。 | 明确数据流 `Raw -> Decoded -> Rendered -> Display` 以及哪些层可丢失信息；每一步只做一次，CLI 不消费 raw 设备输出。 |

## 3. 重点业务模型与所有权裁定

### 3.1 设备命令生命周期

保持 `Device` 为会话、单 reader、单操作 lease、Close 取消和 transport 所有者。命令执行不再隐式重连或重放。后续将下列证据表示做成单一、可查询的领域状态：

- 命令是否开始发送、已发送字节数、是否观察到提示符；
- 设备是否拒绝命令；
- 回执是否已登记、日志/hook/observer 是否后续失败；
- 配置文件是否原始保留、解码是否待核实；
- 本地文件是否提交、是否持久化确认。

不把“命令完成”“设备接受”“配置已保存”合并成一个 `Success` bool。命名采用 `Attempt`、`StepResult`、`Receipt` 等现有领域术语，并在 API 迁移表里列出字段和 JSON 兼容策略。

### 3.2 厂商平台与能力

使用 `Profile` 表达登录、提示符、分页、拒绝签名及通用连接命令；平台家族对象以组合实现配置采集、保存证明、拓扑行为和文件传输行为。可选能力接口仍定义在 `topology`、`transfer` 等消费包，避免 `Platform` 变成包办所有工作的“大接口”。

执行前维护 12 个内建平台的能力矩阵：Cisco IOS/NX-OS、H3C/H3C Wireless、Huawei、Fortinet、Hillstone、Juniper、Palo Alto、Radware、Topsec、Centec。逐项记录登录方式、paging、异常签名、interactions、config、save、neighbor discovery、description read/write、TFTP/remote backup、receipt proof。能力为“支持/不支持/平台条件/证据不足”之一；无证据不宣称支持。

### 3.3 批次任务状态和报告

建立一个状态转换表，覆盖 `计划 -> 可执行/筛除/不支持/重复 -> 排队 -> 执行 -> 成功/部分完成/失败/中断/未开始 -> 报告提交`。每条边明确触发原因及是否计入 attempted/coverage。

机器可读 JSON contract 由 `internal/batchreport` 版本化拥有；Fleet 将业务状态映射到 DTO，CLI 负责文本输出。报告、summary、events、文件 receipt、topology result 的“状态/路径/字节数/校验”字段不能再用相同名称表达不同阶段。

### 3.4 配置对象

配置解析产出不可变 Snapshot（来源、优先级、秘密读取已脱敏），组合根再生成按用例分组的值对象。建议初版只包含真实存在的一组运行设置：`InventorySettings`、`DeviceSettings`、`BackupSettings`、`TFTPSettings`、`TopologySettings`，而不是为每个配置键制造类型。默认值只在一个定义处声明；跨字段校验在进入用例之前完成。

## 4. 分阶段实施计划

按依赖从外向内推进。每一阶段先记录旧/新行为边界，再实施小差异；并行重构同一状态模型不适合拆给互不知情的改动。

### 阶段 0：定准基线与裁定文档冲突（最高优先级）

1. 以当前工作树而不是 HEAD 为基线，保存包依赖图、公开 API 清单、CLI 命令/标志/默认值、报告 schema、平台能力矩阵。
2. 更新 `docs/implementation-plan.md` 中已被决定覆盖的条款，至少同步 `netman` / `tftp-archive`、单一 CLI 命令树、当前依赖发布方式和既定配置默认值。保留原有历史证据，不改写历史进度。
3. 将 37 个 Go 包与 161 个生产 Go 文件统计修正到审查记录；分别标清生产源码/测试源码/模板资源，不用 LOC 作为质量排名。
4. 以 `docs/decisions.md` 和实际公开输出为兼容边界；未明确的点先列为决策条目，不在代码重构中暗改。

**验收：** 所有实施条款能映射到当前命令和包；不存在仍要求已删除的旧入口或与现行决策冲突的默认值；每个稳定 JSON/CLI/配置字面量有 owner。

### 阶段 1：恢复依赖方向并缩小运行时服务对象

1. 绘制 `internal/cli -> workflow -> bootstrap` 当前调用链和 `Services` 方法使用表。
2. 在 `internal/workflow` 定义最小消费接口或函数参数（如设备打开/关闭、运行设置、归档 pool 创建）；workflow 不再 import `internal/bootstrap`。
3. 由 CLI 作为装配入口调用 `bootstrap.New`，将实际依赖注入具体用例；避免 `Services` 被 workflow 当服务定位器使用。
4. 以用例拆装配结果，但不重复运行中的对象：备份、TFTP、topology 共用设备工厂和 Fleet 规则，差异通过具体输入/可选能力表达。
5. 构造配置/计划/预览时仍不获取设备密钥、不打开 SSH、不创建远端上传会话。

**验收：** 依赖图中 workflow 不依赖 bootstrap；没有 package global；CLI 有且只有一个 composition root；离线 plan/show/check 行为不触发设备连接；取消和错误语义与现状一致。

### 阶段 2：配置与应用请求模型

1. 盘点 `config.Defaults`、schema、bootstrap fallback、workflow fallback 的每项取值，建立一份 default/validation/provenance 表。
2. 让 `config.Snapshot` 仅负责加载/合并/来源追踪；bootstrap 边界一次生成类型化运行设置，不在 workflow 中反复读取字符串路径。
3. 对 timeouts、parallel、布局、策略、source/cache mode 等枚举/范围使用具名类型或构造验证；只在业务存在时建类型，避免每个 string 都包一层 struct。
4. CLI 解析形成 `BackupRequest`、`TFTPRequest`、`TopologyRequest` 等真正具有不同验证/业务约束的请求；不要把 `options` 的所有 bool/字符串开关传给每层。
5. 配置字段名、环境变量名、来源优先级、secret 行为维持原契约，并在迁移矩阵记录。

**验收：** 每个默认值只声明一次；无效配置在任务开始前报出明确字段；类型化请求不存在互斥 action 的非法组合；CLI help 不触发 config I/O。

### 阶段 3：Fleet 状态机和批次报告契约

1. 依据 Fleet 当前所有分支写出状态转换和 coverage/accounting 表。
2. 定义本包的 `Mode`、`PlanStatus`、`OutcomeStatus`、`BatchStatus`，集中实现 parse/string/JSON 行为，兼容现有 JSON 字符串。
3. 提取 backup/TFTP 的 artifact outcome 规则，确保报告统计、退出码、展示计数使用同一规则，不重复组合布尔条件。
4. 在 `internal/batchreport` 定义 schema-versioned report DTO 与字段合法性；Fleet 从领域对象映射它，`storage` 只验证并选择 artifact。
5. 诊断采用领域 error code 常量和安全元数据；中文映射由 display 层维护，检查所有对外错误码有对应展示或明确使用英文安全回退。
6. 保留库存顺序、重复设备筛除、取消后停止接纳、已接纳任务有界结束、callback failure 单独记录的既有语义。

**验收：** 任一状态统计仅有一套计算路径；schema version/optional/null/verification 语义一致；report 可由 `batchreport` 唯一解析；JSON 结构和 CLI 退出码符合决策文件。

### 阶段 4：先完成厂商对象模型，再按家族迁移

1. 用 12 平台能力矩阵为现有行为建立基线，逐项标出 Ruby/夹具/生产只读证据来源。
2. 定义 platform/family/Profile/可选能力的关系图；确定家族构造函数返回平台对象还是值对象组合。避免 Profile 复制后带可变共享 map/regex 的问题。
3. 清理 `vendors/internal/driver`：保留调用方确实共享的执行算法；命令/提示符/识别规则不越界；共享策略均有明确输入输出和失败证据。
4. 以一个简单家族（Centec/Topsec）作模式校准，再做 Cisco/H3C 双变体，然后逐一处理 Huawei、Hillstone、Fortinet、Juniper、Palo Alto、Radware；每家族独立差异集，避免一次性全仓迁移。
5. 每家族按 `登录 -> 交互 -> 进入权限 -> 分页 -> 命令拒绝 -> 配置采集 -> 保存证明 -> 拓扑/传输可选能力` 顺序检查；不支持的能力在 plan 阶段阻断。
6. 保持包位于 `vendors/<family>`；不恢复 `vendors/internal/platform/<family>`；仅允许有真实共享语义的 `vendors/internal/driver`、`vendors/internal/topology`。

**验收：** Registry 对每个身份只有确定优先级；任意平台 Profile 独立；可选能力边界可列举；新增平台只需家族实现与注册，不在 Fleet/Workflow 加厂商 switch；命令和回执证据与基线一致。

### 阶段 5：Topology 与 Transfer 业务回执

1. topology 领域明确 `Discovery`、`DescriptionSnapshot`、`ChangePlan`、`Audit`、`ApplyResult` 的对象责任及数据复制；目前 `Plan`/`Audit` 的公开 map/slice 是否可变需在保持 JSON 兼容前提下裁定。
2. 只有构造并验证成功的计划可执行；plan 摘要、证据和设备身份必须绑定；Apply 再读证据后才写；读回失败不保存；no-op 无写命令。
3. transfer 把命令策略、原文行证据、源文件解析、目标路径和 receipt 构造分层；同一截止时间覆盖发现、命令与收尾。
4. 明确 TFTP `device_reported` 和 `server_verified` 是不同证据；报告导出、每日归档与 local file 不可混淆；失败时不自动重传。
5. 对 remote backup 的 FTP/SCP positive `Wrote`、requested/actual path、未提交状态分别建模。

**验收：** topology 写入前所有校验完成且仅一个 Device operation lease；TFTP 设备报告不能被提升为服务器已验证；路径和 hash 均来自可信证据；失败不会重放有副作用命令。

### 阶段 6：Archive 资源生命周期和存储所有权

1. 画 `Pool -> Client -> SSH master/channel -> Protocol -> worker -> dirfd/file` 所有权与关闭顺序，列所有并发状态和 close/cancel race。
2. Pool 管预检、槽位与并发收尾；Client 管单次远端目标事务；worker 管协议帧和安全文件系统操作。明确每层错误的 owner 和可重试边界。
3. 以显式事务阶段记录 `prepared/archived/delivered/verified/cleaned`；确认现有 receipt 的 `Verified` 不会被错误复用。状态类型必须服务于清晰性，不能多引入一层状态副本。
4. `worker.go` 仅按职责边界拆分：路径/FD 生命周期、文件稳定读取与 hash、协议 action。不以 537 行作为唯一拆分理由。
5. storage 是否需要 `Store`/`Batch` 对象需先检查跨调用共享根目录、锁和生命周期；若没有持久状态，保留纯函数更简单。统一本地 atomic receipt 和安全导出，不让 archive 实现重复一份本地写入规则。

**验收：** 所有 goroutine/进程/FD/锁都有唯一关闭者；Close 能取消并等待已经接纳的任务；不确定上传不重传；旧目标在失败时保留；成功清理要求双方证据齐全；symlink/path traversal/文件身份检查仍有效。

### 阶段 7：库存来源、缓存及解析管线

1. 保持 `inventory` 纯值与纯选择逻辑；Source/Cache 由 `netdisco` 持有 HTTP client、PostgreSQL 连接与缓存文件事务。
2. 将 netdisco source selection 和 credential lookup 留在 composition root，但 source 参数用类型化选项；同一库存完整或错误，不回传半分页数据。
3. Cache result 对 fresh/stale/offline/disabled/error 建模；身份 hash 排除 secret，source identity 必须包含影响结果的查询参数。
4. `textfsm` 只处理模板引擎和每次独立 ParserOutput；平台决定模板/完整性规则；终端文本 decode/render 只能有一条有界处理链。
5. 逐项核对地址规范化、IPv4-mapped IPv6、主机名、重复 host 的行为在 inventory、fleet、batchreport、storage 一致。

**验收：** 清单不完整时不执行设备；Cache 身份稳定且不含凭据；解析不串用状态；大输入受资源上限约束；不同来源的错误可区分又不泄露凭据/原始响应。

### 阶段 8：名称、文件组织与性能精修

1. 合并公开 API、目录名、receiver、局部变量和配置字面量清单，按包审查重复限定词、布尔参数、无调用 helper、缩写和不明确动词。
2. `dir` 适用于调用上下文清楚的局部/私有变量；领域配置键 `backup.directory`、已发布 Go API 字段、JSON/CLI/协议字面量不能仅为缩短而变更。公开 API 改名须进入破坏性变更裁定，不加无意义兼容转发。
3. 文件按同一业务概念职责拆分；先让类型/API 稳定，后移动代码。禁止复制粘贴再以抽象包装；对重复行为先证明语义一致。
4. 性能改进以可重复 CPU/内存/分配/并发基线为前置：Fleet 大清单计划、TextFSM 高频解析、terminal 大输出、transfer 证据扫描、进度热路径、archive 并发池。只优化观测到的热路径，不以设备网络总耗时推断解析器收益。
5. 评估复杂接口/反射/复制/锁/正则缓存时记录收益和边界；删除抽象带来的重复状态或配置，避免“性能优化”牺牲回执与取消安全。

**验收：** 新名称可从包名、receiver 和业务概念读懂；无不必要的目录层；同一事实无并行副本；性能报告含环境、负载、基线和限制。

### 阶段 9：集成验收与交付文档

计划性验收（实施阶段执行，本次未执行）：

- `GOWORK=off go build ./cmd/...`、`go vet ./...`、完整 `go test ./...`，之后按资源所有权风险运行 race/取消/生命周期用例；本轮不增加或运行测试。
- 用离线 fixture/PTY/本地 HTTP、临时 PostgreSQL 和本地 SSH worker 验证边界，不把这些结果描述成真实网络设备证明。
- 与 `ruby/net-connector` 对照平台命令字节、交互顺序、配置内容、完成证据、报告和退出码；差异必须有已批准的行为说明。
- 对外消费者单独构建，检查 Go API 是否只暴露真实稳定契约；`go mod verify`、tidy 和支持平台交叉构建按项目发布门禁执行。
- 性能用固定数量设备/响应字节/并发数做重复 benchmark，并记录 RSS、allocs、吞吐和置信范围；不声称真实网络提升。
- 最终文档记录本次改动、行为兼容表、测试结果、离线/实机证据边界、未解决风险；提交或发布仍需单独授权。

## 5. 优先级与依赖顺序

| 优先级 | 工作项 | 原因 | 依赖 |
| --- | --- | --- | --- |
| P0 | 校正文档基线与决策冲突 | 否则实现者会按过期方案变更命令和默认值。 | 无 |
| P1 | 移除 workflow 对 bootstrap 的依赖 | 先恢复组合根方向，让后续工作流接口不再绑到 Services。 | P0 |
| P1 | Fleet 状态、报告 schema、配置默认值收敛 | 是 CLI、备份/TFTP、导出的共同业务契约。 | P0，可与 P1 分支评审，不建议同时改同一 API |
| P2 | Vendor 能力模型与逐家族梳理 | 直接影响设备行为正确性，按家族逐个迁移。 | 状态/配置契约稳定后 |
| P2 | Topology/Transfer 回执对象 | 需要依赖已稳定的平台能力和 Fleet artifact。 | Vendor 矩阵及 Fleet 契约 |
| P2 | Archive 生命周期收敛 | 高风险资源代码，需稳定 Transfer proof 和 receipt。 | Transfer 证据合同 |
| P3 | Inventory/Parser/Storage 重复路径收敛 | 减少来源、缓存、导出与展示漂移。 | 报告契约及组合根调整 |
| P3 | 命名和文件拆分 | 在职责稳定后做，避免对临时形状反复改名。 | 前述对象边界基本完成 |
| P4 | 有证据的性能精修与完整验收 | 先有稳定可重复基线，才能判断收益。 | 所有业务契约稳定 |

## 6. 本轮审查边界

- 读取当前源码、架构说明、厂商接入说明、实施计划、决策记录和进度记录；读取生产 Go 依赖图与代码规模统计。
- 未执行设备连接、TFTP 预检、远端命令、配置修改、build/test/vet/race/benchmark，也未判断真实设备或生产负载表现。
- 当前工作树原有修改与未跟踪内容均保留；本方案不授权清理、重置、提交、推送、发布或部署。
- 当前建议以模块/API/业务不变量为先；任何行为变化、配置/JSON 字段变更、错误码变化和设备写入语义变化都应单独列兼容决策并经 Ruby 行为证据确认。

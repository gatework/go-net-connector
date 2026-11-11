# 实施记录

记录每次实施的范围、验证命令与证据边界。v0.6.0 之前的历史记录见 Git 历史。

## 2026-11-09 Go 惯用法、去样板与批量下发

按已批准方案（`~/.claude/plans/go-go-net-connector-dapper-grove.md`）在工作区实施，未提交、未发布。基线：除 `internal/cli` 的 `TestInteractiveDeclinedOptionalEnvContinues` 在本机 PTY 下挂起至超时（改动前即如此，未修改该用例）外，`GOWORK=off go test ./...` 全部通过。

- 现代化：`GOWORK=off go fix ./...`（errors.AsType、maps.Copy、slices.ContainsFunc/Backward/Contains、strings.SplitSeq、range int），删除被内联后未使用的指针辅助函数；`.golangci.yml` 启用 modernize、errorlint（测试除外）与 revive 的 unexported-return、receiver-naming。`pureDeviceRejection`、`primaryDiagnosticError` 保留顶层判断并注明原因。未启用 revive `exported`：缺失文档注释数量大，另行处理。
- 枚举单一拥有者：`batchreport.SuccessPolicy`/`Layout`、`netdisco.CacheMode`、`inventory.MatchMode`、`config.FieldKind`；config、bootstrap、CLI、fleet、display 与补全表改用这些定义，字符串值与 CLI 参考不变。`netdisco.FetchOptions.Mode`、`Cache.Fetch` 改为 `CacheMode`。
- 厂商样板：家族构造函数返回导出类型（`h3c.Dialect`、`h3c.WirelessDialect`、`cisco.IOSDialect` 等）；`driver.TopologyReader` 组合三项只读拓扑能力；`driver.PositionMarkerCommandErrors`、`driver.YesNoConfirmations` 去除 H3C/Huawei 逐字重复；`TFTPBase` 直接持有 `TFTPOptions`，文件名长度与源接口名改为具名常量，参数错误包装 `ErrInvalid` 并只标明字段名。`vendors/profiles_test.go`、parity 与能力契约测试未改断言，全部通过。
- 破坏性配置变更（用户确认）：删除 `tftp.h3c_source_file`、`tftp.h3c_wireless_source_file`、`tftp.huawei_source_file` 及对应环境变量；出现时返回 `FieldError{Problem:"removed", Replacement}`，CLI 提示改用 `tftp.source_files.<平台>`。迁移见 `docs/api-migration.md`。设备凭据键集中到 `credentialKeys`，查找顺序与空白回退语义不变。
- 计划预览：`workflow.NewPlanView` 返回类型化 `PlanView`，TFTP 传输字段在同一处生成，去掉 `[]map[string]any` 类型断言；先录制 `internal/workflow/testdata/plan_view_*.json` golden，重构后字节一致。
- 新增 `netman push plan|run`：连接前校验全部脚本；fleet 规划沿用设备筛选；每台设备一次操作租约内 `ExecuteScript`，可选 `SaveConfig`（仅 ValidateSave 正向回执算保存）；失败不重试、不重放；`--stop-after-failures` 只阻止未开始的设备；`report.json` 区分 AcceptedCommands 与 Attempts。指定 `--save` 时未声明保存脚本的平台在规划阶段记为 `save_unsupported` 并跳过；脚本中的 C0 控制字符（制表符除外）在连接前拒绝。

发布准备：在 origin/main（v0.5.0，`f0772b2`）之上重新应用并解决 config、CLI 与迁移文档冲突，版本定为 v0.6.0（含破坏性配置变更）。随后将 go-expect 从 v0.4.1 升级为正式 v0.5.0：proxy.golang.org 与 sum.golang.org 哈希一致，标签指向发布说明记录的测试提交 `33bf675`；本模块无需源码改动，全包 race 测试与 vet 通过。验证：`GOWORK=off go test -skip TestInteractiveDeclinedOptionalEnvContinues ./...` PASS；`go test -race` 覆盖 internal/...、fleet、vendors/...、config、netdisco、inventory、storage、transfer PASS；`go vet ./...` PASS；golangci-lint v2.15.0 0 issues；`docs/cli-reference.md` 由 `-update-cli-reference` 重新生成。push 使用模拟传输验证拒绝后不再发送、保存未确认、失败上限与脚本无效时不读清单不建设备；不代表真实设备验收。

## 2026-11-09 v0.7.0 重构：清除兼容代码（Phase 1）与平台驱动（Phase 2）

按批准方案（`~/.claude/plans/go-go-net-connector-dapper-grove.md`）在 v0.6.0（`5fb812e`）上实施，未提交。

- Phase 1：删除 deadcode 报告中不被下游使用的 API（见 `api-migration.md` 的 Removed APIs）；`textfsm` 模板索引及两个无引用模板删除，夹具测试改为显式模板路径，Filldown 状态隔离改用内联模板；`netdisco` 旧 `NewCache` 身份路径与遗留缓存迁移测试删除，缓存类型改为包内私有；`config.Load(ctx, …)` 取代兼容包装；删除 `DOTENV_LINEBREAK_MODE=legacy`、旧 `Authenticated()` 钩子拒绝分支；Ruby 对齐测试改名为 `contracts_golden_test.go`、夹具改名 `testdata/device-contracts.json`，断言不变；删除 `scripts/reference-fixtures.rb`。核实后保留 `verify-expect-source.py`（go-expect 候选验证）与 `verify-firemon-source.py`（Firemon 适配器为第三个下游，锁定 v0.2.0），二者并非 Ruby 工具。`docs/design.md` 取代 implementation-plan 与 decisions；历史计划、基线日志与 quality-evidence 从仓库删除（Git 历史保留）。
- Phase 2：`connector.Profile` 拆为 `Terminal` + `Driver`，可选钩子为 `CommandPreparer/CommandObserver/LoginInspector`；`dialect.Platform` 内嵌 `connector.Driver` 并含 `MatchInventory`；`driver.Base` 取代 `BaseProfile/FinishProfile/ProfiledPlatform`；H3C 规则号钩子、Radware HA 状态成为平台方法；每个家族新增 `capabilities_test.go` 编译期能力断言（与能力矩阵一致）。`LegacySSH*` 更名 `LegacyCrypto*`。CLI 尚未开放 `AllowLegacyCrypto` 配置项，旧设备弱加密恢复目前仅库 API 可用。
- 已删除的不可能场景：`fleet` 中"配置采集工厂返回 nil"子用例（新契约下 nil 即通用采集）。

验证：`GOWORK=off go test -race -skip TestInteractiveDeclinedOptionalEnvContinues ./...` PASS；`go vet ./...` PASS；golangci-lint v2.15.0 0 issues；`check-architecture.py`/`test_architecture.py` PASS。

## 2026-11-10 v0.7.0 重构：阶段流水线与统一报告（Phase 3）、CLI 用例（Phase 4）、运维能力（Phase 5）

- Phase 3：`internal/workflow` 以 `Stage`/`Operation`/`Session`/`Evidence` 与 `Runner.Execute` 取代 4 套执行循环；`Session` 懒打开并记录 authenticate 阶段（TFTP 在归档事务内才打开，保留"先准备归档、后上传"的顺序）；首个失败阶段拦截后续阶段并记为 skipped；`Evidence.MarkPartial/MarkReview` 区分部分完成与需复核。拓扑 apply 为 `Operation.Then` 第二轮，`Before` 先写 `scripts/` 与 `topology-plan.json`。统一报告 schema v3 由 `internal/batchreport` 拥有（含导出选择规则）；删除 `fleet.Run/Report/Diagnostic`、Ruby 错误类型映射、`internal/topologyreport` 与旧 workflow 文件。工作者以 `Goexit` 退出时该设备记为 failed（`internal_error/worker`），批次仍写报告。备份与 TFTP 新增 `--save` 阶段。偏离方案：未持久化 `report.txt`（文本由 CLI 从报告渲染，避免业务层依赖呈现）。
- Phase 4：`internal/cli` 每个业务命令为 `useCase`（`prepare` 离线校验、`run` 执行），删除执行分派 switch；未拆分平铺 `options` 结构，留待后续。
- Phase 5：`netman exec`（`driver.ReadOnlyCommands`，12 个平台均声明；重定向类管道拒绝）、`netman backup remote`（Juniper 原生导出，远端文件名为 IP+时间戳，密码仅取 `NC_REMOTE_BACKUP_PASSWORD`）、`netman backup diff`（`internal/textdiff` Myers 行级 diff，与 GNU `diff -U2` 输出一致）、跳板机 `-J`（参数注入与 Telnet 组合拒绝）、登录提示前连接重试（refused/no route/timeout，认证失败零重试，取消即停）；修正"无路由/连接超时"被误报为 authentication_error。

验证：`GOWORK=off go test -race -skip TestInteractiveDeclinedOptionalEnvContinues ./...` PASS；`go vet ./...` PASS；golangci-lint v2.15.0 0 issues；`check-architecture.py`/`test_architecture.py` PASS；`go mod tidy -diff` 无差异；`deadcode ./cmd/...` 仅剩脱敏方法与供下游复用的公开 API。模拟传输覆盖：阶段拦截、认证阶段失败、三种策略、取消、归档上传→报告→移动后导出、deploy 拒绝后不再发送、保存未确认为 partial、exec 写命令零连接、Juniper 导出命令字节与回执解析、diff 退出码、重试边界与跳板机参数。未在真实设备、真实跳板机或远端 SCP/FTP 服务器上验证。

发布准备：版本定为 v0.7.0；更新 `scripts/verify-consumer.py` 至新 API（平台即 Driver、`StaticDriver`、`Pool.Prepare(ctx, nil)`）并覆盖新命令帮助，本地 PASS；三个 fuzz 目标各 20000 次 PASS。

## 2026-11-11 shared Go standard

Adopt go-standards v1.0.2 through standard.mk, the distributed lint configuration and architecture.yaml. Keep the project architecture test, installed archive-worker, PostgreSQL, bounded fuzz and independent consumer checks under make check. Extract phase helpers without changing failure receipts, cache-only password handling, cancellation or cleanup order. The release uses published go-expect v0.6.0 and migrates the internal MatchOption call; the connector public API is unchanged.

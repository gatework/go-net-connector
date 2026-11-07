# Implementation progress

## 2026-11-07：借用会话与清理所有权

新增 `WithBorrowedOperation`，在已连接设备上持有完整回调租约，禁止隐式连接、重连或命令重放。`Operation.Retire` 在取消后仍可清理，拒绝重入、重复及过期使用；`Connected` 限定为活动作用域的会话快照。借用作用域分别返回操作错误和首次清理错误，回调忽略清理失败或返回 nil、作用域退出取消并等待已接纳调用时都保留清理证据；退出不自动重试清理，后续显式 `Device.Close` 可以重试失败的 transport owner。

单命令 `PreserveSessionOnRejection` 仅在借用作用域写前校验通过、完整严格解码响应和提示符、预算未越界、context 未取消、所有 observer/logger 完成后只剩纯设备拒绝时保活。失败响应只进入 `Attempts`，不会变成已接受 `Steps` 或触发 receipt/step 回调。原有普通 Device、WithOperation 和终止式 Close 语义保留。

真实 `net.Pipe` 回归覆盖完整拒绝后的显式恢复命令、未连接零打开、多命令零写入、无效编码/部分输出/输出预算、晚 observer/logger/取消、清理失败的独立错误树、重入/过期/取消后 retirement、活动 callback 中终止 Close、配置失败的嵌套清理，以及 scope 退出时取消并等待调用。补充回归先复现最终 observer 取消后仍报告成功，以及其 panic 被业务回调 recover 后遗留连接的问题；最终收尾检查取消，异常退出兜底清理，已接受的响应仍保留 Steps。

验证环境：Go 1.27.1 / darwin arm64。`GOWORK=off go test ./... -count=1`、`go test -race -shuffle=on ./... -count=1`、`go vet ./...`、`go mod verify`、`go mod tidy -diff`、变更 Go 文件的 `goimports -l` 和 `git diff --check` 全部通过。`GOWORK=off python3 scripts/verify-consumer.py` 通过，覆盖独立源码消费者、命令构建、checkout 外 CLI 和嵌入模板，确认正式固定 `go-expect v0.4.0`、无外部 replace。未修改 VERSION 或发布脚本，未推送、发布或连接实机；源码消费者通过不代表正式版本已被消费。

The latest local quality pass is complete; see [2026-10-29 verification](2026-10-29-quality-verification.md). The following delivery notes are historical and do not authorize publication of the new work.

Implementation and final scoped review are complete. The user authorized a private GitHub commit/push and first release on 2026-10-26. Release preparation is in progress; deployment remains outside scope.

| Stage | Status |
| --- | --- |
| P0 frozen references | Recorded manifests and working-source snapshots; Ruby baseline suite passes |
| P1 expect compatibility | Implemented; independent review accepted after one repair round |
| P2 connector lifecycle | Implemented; lifecycle/accounting/logging/pager repair independently accepted |
| P3 vendors/parser/storage | 12 profiles, 19 templates and private atomic storage implemented and reviewed |
| P4 topology | Implemented; formatter error propagation repair accepted |
| P5 transfer/archive | TFTP/Junos FTP/SCP, local/SSH worker and pool implemented; five archive fixes independently accepted |
| P6 inventory/Fleet/report | HTTP/PG/cache/planning/report implemented; source, unchanged receipt and diagnostic repairs accepted |
| P7 config/CLI | Config and CLI accepted; initialization, output, policy and report boundary repairs independently reviewed |
| P8 acceptance | Darwin/Linux Go 1.26/1.27 gates, four target builds, consumers, fuzz and benchmarks pass; all six final review repairs independently accepted |

Reference go-expect source files were not edited. Its working directory already contains parallel implementation changes; those remain intact. `dependencies/go-expect-source.tar.gz` plus `../go-expect/patches/go-expect-compat.patch` reproduces the isolated candidate without overwriting that workspace.

A real localhost sshd and installed worker validated two foreground master connections and verified upload/archive/cleanup. Synthetic device conversations, HTTP/TLS and temporary PostgreSQL checks remain distinct from real-device evidence. Details and limits are recorded in [verification.md](verification.md).

Review artifacts and source diffs remain under `.superpowers/sdd/implementation-plan/` to preserve the complete review process separately from the source commit.

## 2026-10-26: automatic device output encoding

- Reproduced H3C/H3C wireless GB2312/GBK Chinese configuration failing with `invalid_output_encoding`. Added pinned `github.com/saintfish/chardet` at `v0.0.0-20230101081208-5e3ef4b5456d` and conversion through the existing `golang.org/x/text` dependency.
- Every connector command now automatically checks its complete response and returns UTF-8 text, without new CLI flags, configuration settings or connection options. Valid UTF-8 takes priority; non-UTF-8 detection is bounded to 64 KiB from the first non-ASCII byte and constrained to the GB18030 family. Full-response conversion rejects malformed/truncated sequences rather than silently inserting replacement characters. Detection remains heuristic when bytes have multiple valid interpretations.
- Raw responses remain available. Legacy terminal edits run on original bytes before visible text conversion; decoded output retains the byte budget. Backups use UTF-8 bytes and SHA-256, and failed decoding leaves previous backups intact. Logs convert complete lines and redact UTF-8 and encoded secret variants before and after rendering. The existing Fleet invalid-encoding error and failed-step semantics remain unchanged.
- Regression evidence covers default API and CLI use, ASCII/UTF-8/GBK/GB18030, one-byte transport fragments, paging, Chinese after a large ASCII prefix, native-byte terminal edits, corrupted input, previous-backup preservation, decoded-size limits, secret redaction and backup hashes.
- Final local checks passed with Go 1.27.3: `sh scripts/check` (workspace tests, complete race suite, vet, frozen expect race suite and command builds), `go mod verify`, independent consumer/outside-checkout CLI verification with `GOTOOLCHAIN=local`, and `govulncheck v1.9.0` (`No vulnerabilities found`). Module tidy was checked using a temporary modfile with the existing frozen expect candidate; no replacement was added to the published module file.
- These checks did not contact a production switch or deploy to a device. The original go-expect working tree was not edited.

## 2026-10-26 unification

Plan: docs/2026-10-26-unification-plan.md. Paired worktrees at ../go-expect and this checkout; bases c66cbd2/a25b345.
Ruling: work in new paired feature worktrees; do not commit or publish. Preserve original compatibility worktree.
Ruling: a published compatible dependency cannot be created without publication authorization; retain the release gate and verify explicit local paired consumers, never label them published-dependency evidence.
Preflight: compatibility port must preserve standalone core fixes; canonical connector consumes the port before its API migration.

User correction: consume github.com/gatework/go-expect directly; no local replace or cross-repository go.work. Temporary setup workspace removed before implementation. Resolve GitHub dependency availability before migrating consumer APIs.

Generic-core milestone ready in sibling worktree: compatibility regressions RED on absent APIs; close-handler and cleanup-cause regressions RED then GREEN; full script/ci (race/vet/fuzz/bench/example/self-source consumer), lint and vulnerability scan passed. Fresh review found SendAll suffix reconversion; allocation regression failed at 4112 allocations and passed after one-buffer fix, followed by full CI. No other Important/Critical findings.
Independent net-connector changes verified without any local module replacement: archive blocked Send/Expect/Receive cancellation tests RED then GREEN using selected actual archive sources; CRLF parsing and Longest/POSIX snapshot tests RED then GREEN using actual connector/types.go. Full consumer build awaits a GitHub-accessible compatible dependency.

Archive channel ownership extracted into channel.go. Actual-source tests first reproduced lost primary+cleanup causes and unclosed partial launcher return, then passed under race after safe wrapping and cleanup fixes.
Awaiting explicit answer to GitHub dependency delivery question. No local dependency replacements/workspace remain in this worktree, no GitHub mutations performed. Remaining plan work: canonical Device cancellation/terminal Close/recovery and panic guards; attempt evidence; key authentication/line ending migration; shared save confirmation; policy/status types and naming migration; consumer docs/tooling/full integrations and final consumer review. These are NOT complete.

## 2026-10-27: authorized GitHub delivery

The user explicitly authorized committing and pushing both repositories. This supersedes the earlier no-commit/no-push ruling for the paired unification worktrees; the original nested compatibility worktree remains preserved as historical WIP.

The current connector changes implement terminal Close/cancellation and explicit recovery, failed-attempt evidence, key authentication and line endings, shared save validation, API naming and typed policies, archive channel cleanup, and migration documentation. go-expect resolves directly from the immutable GitHub version pinned in go.mod with GOWORK=off and no dependency replacement.

Fresh local verification passed: connector full race/shuffle suite, go vet, goimports, go mod verify, and the independent consumer/outside-checkout CLI check. The paired go-expect script/ci and actionlint checks also passed. These checks do not establish remote CI, full cross-platform integration, final implementation review, real-device behavior, or a formal release.

## 2026-10-29: foundation quality pass

Approved plan: [2026-10-29-quality-plan.md](2026-10-29-quality-plan.md). Worktrees relative to `/Users/crate/Works/go`: `.worktrees/connector-unification/go-expect` (base `3d3b1ef`) and `.worktrees/connector-quality/go-net-connector` (base `c68c42b`). Existing fleet migration and archive/validation WIP was preserved and completed; original checkouts remain separate.

Lifecycle fixes own partial factory transports, retain primary and cleanup causes, stop mutating borrowed AfterLogin metadata, and make archive Prepare cancellation responsive. The public surface now includes pure validation and immutable rendered response access; `vendors.Open` was removed and fleet orchestration moved to `fleet`. Rendering, disabled logging, rule compilation and bounded embedded TextFSM compilation were optimized. go-expect receives OS scratch reuse, incremental literal-All matching and public Redactor improvements without changing arbitrary regexp matching or the polling architecture.

Independent review found and resolved rendered-prompt/configuration evidence mismatch, overlapping parent-cancellation/Goexit cause loss, and a quadratic long-secret redaction boundary path. Regression checks and independent source re-review are complete; no source-review blockers remain. Both repositories passed Go 1.26/1.27 macOS/Linux race/vet checks and fixed-count fuzzing. Connector full CI also covered the installed worker, temporary PostgreSQL, public source consumer and current firemon adapter contracts. Go 1.27.3 vulnerability scans passed after the older host toolchain was identified separately. The [verification report](2026-10-29-quality-verification.md) records raw benchmark artifacts, slower cases, repeated output measurements and delivery limits. No current authorization to commit, push, publish or deploy is inferred from the historical delivery section above.

## 2026-10-29 second refinement

Completed the [seven-task refinement](2026-10-29-refinement-plan.md): admitted
callback contexts and typed login state; immutable failed/accepted response
evidence; preserved simultaneous errors and primary diagnostics; reusable vendor
definitions and plan capabilities; output sharing; and connector forwarding for
expect's runtime read deadlines/PTY normalization. The paired expect worktree
adds the runtime read pump, owned Poll fence, receive-tail release and incremental
long-rule redaction. No dependency replacement or version change was made.

Both independent scoped reviews and final integration review passed. All final
platform gates, race/vet, fuzz, lint, vulnerability and source-consumer checks are
recorded in the [refinement verification](2026-10-29-refinement-verification.md).
Ten-sample serial comparisons passed the final performance gate after correcting
an intermediate redactor allocation regression and a heap measurement baseline.
The full ten-thousand-device plan is about 97% faster; generic command timing is
unchanged, configuration allocation drops about one full normalized output copy.
Expect's idle resource, PTY, redaction and receive-storage results are reported
separately. Original WIP is preserved; changes remain local and uncommitted.

## 2026-10-30 third refinement and release authorization

Implemented the [third-round plan](2026-10-30-refinement-plan.md): contextual
transport authentication, partial process cleanup ownership, complete write-error
trees, fleet outcome allocation, direct inventory mapping and compiled TFTP
evidence. Paired expect sources add incremental private filtering, bounded
retention/no-sink handling and deferred captures. Independent review identified
and verified a sink-failure storage-release fix. Serial ten-sample benchmarks
passed the performance gate; detailed raw results accompany each module.

The user authorized commits, GitHub pushes and releases for both modules, plus
temporary public connector visibility if billing prevents private Actions, with
private restoration after verification. Remote publication evidence will be
recorded separately from the local [verification](2026-10-30-refinement-verification.md).

## 2026-11-02：备份文本保真与中文提示

用户已批准 [实施方案](2026-11-02-backup-text-plan.md)。当前在现有干净工作区分工修改核心编码、厂商配置采集与中文展示；不提交、推送或发布。最终门禁、实机结果与历史文件审计将在验证后补记。

实施结果：备份专用容错视图、哈希原始旁车、独立文件回执、编码及有界告警元数据已实现；常规命令/RunningConfig 仍严格校验。PAN 带尾部空格的提示符及多行引用空白保留已修复；Radware 登录仅增加阅读确认。

实机只读验证：`/Users/wenwu.yan/Backups/go-net-connector/verified-final/2026-11-02_15-44-00/summary.json`，原六台补齐目标加一台对照共 7/7 成功，退出 0；两台各有一个未知字节告警，10.156.9.4 保留 144 个正常中文字符。独立核验 14 个阅读/原文文件的大小、SHA-256 与 0600 权限。未执行 save/apply 或设备配置变更。

历史文件审计：原 953 文件 UTF-8/哈希核验通过且未检出明显乱码；缺少原文不作语义正确性保证。三个历史补充阅读视图从保留原文生成独立修正副本，每份仅转义一个坏字节，原文及旧视图未改写。证据目录 `/Users/wenwu.yan/Backups/go-net-connector/audits/20261102-153823`。

最终验证：显式 `NC_TEST_POSTGRES_BIN=/opt/homebrew/opt/postgresql@18/bin sh scripts/check` 通过，覆盖完整 race/shuffle 测试、临时 PostgreSQL、已构建归档 worker、vet、三项各 100000 次 fuzz、独立源码消费者与仓库外 CLI 验证。`python3 scripts/test_release.py` 11 项通过；`git diff --check` 通过；全部命令已重新构建到 bin/ 并实测中文帮助。归档进程清理测试此前一次超时，独立连续三次复核及最终完整门禁均通过。自动 PostgreSQL 探测曾选中仅含客户端的 libpq，最终显式选择完整 PostgreSQL 安装。

额外历史辅助检查限制：`python3 -m unittest discover -s scripts -p 'test_*.py'` 的旧工作区准备测试因 `.work/go-expect` 历史输入缺失报错；未重建已淘汰的本地依赖替换工作区，也未将其冒充通过。依赖仍为 go.mod 固定的 GitHub 模块，GOWORK=off。

## 2026-11-03：两库联动重构

用户批准 [联动方案](2026-11-03-joint-refinement.md)。本轮直接迁移 Go 接口、不保留别名，保留已有 WIP，不提交或发布。go-expect 候选与 connector 使用隔离源码配对验证；旧固定 v0.1.2 尚不包含新增 API，正式依赖采纳不在本轮完成声明内。实施曾因服务容量/额度中断，已按当前实际文件继续，未覆盖或重置已有改动。

## 2026-11-03 CLI 统一入口

开始实施用户确认的 cloudflare-go 风格聚合方案。独立任务覆盖命令树/中文帮助、类型化工作流、批次导出；构建与发行清单已改为 net-connector 和 worker，打包机制 11 项测试通过。实施及最终验收见 docs/2026-11-03-cli-unification.md。正式 expect 依赖保持不变，当前验收采用隔离源码配对；无提交、发布或真实设备操作。

CLI 统一入口最终候选实现完成：Go126/127 全包配对 race/shuffle/vet、三个交叉构建、目录外消费者和11项发行机制测试通过；详情与局限见 docs/2026-11-03-cli-unification.md。

## 2026-11-04 进度与严格文本备份

补齐 Ruby 风格阶段、计划、设备开始/结果、耗时和定时刷新；默认 CLI 仅保存严格解码的文本配置，不生成raw旁车。新增安全会话元数据观察接口，输出故障保留报告与完成证据。修复失败事件、跳过/待核实计数及TTY刷新。实施记录见 docs/2026-11-04-progress-text-backup.md。

## 2026-11-04 原地进度与 inventory 表格

修复正常事件逐行刷屏，普通/verbose 均刷新固定面板；异常立即保留。统一警告输出、提示暂停、停止清理和 Unicode 显示宽度。inventory 展示匹配异常并优先排列，固定五列表格；JSON 与任务选择不变。验证记录：docs/validation/2026-11-04-terminal-panel.md。未执行真实设备批量操作。

## 2026-11-04 TFTP 归档预检失败诊断

用户从 bin 执行 backup tftp，976 台计划生成后出现泛化错误。读取有效配置确认 backup.yml 覆盖统一配置，归档目标为 ssh://root@10.132.12.51/srv/tftp。通过现有密码进行只读 SSH 检查：服务器 Linux x86_64；默认 /usr/local/bin/net-connector-archive 不存在。批次仅生成空 tftp 目录，尚未进入设备上传阶段。

增加归档预检阶段提示和安全的中文诊断，保留原始错误链及取消语义。回归验证覆盖预检失败不创建设备、诊断不泄露底层错误。全量候选源码配对 race/shuffle/vet 和定向测试通过。重编译本机 CLI 及 Linux amd64 静态归档程序到 bin/linux-amd64，附构建指纹；尚未安装到远端服务器。

## 2026-11-04 TFTP 慢任务与每日最新归档

实测 10.203.20.3 登录与 display startup 均成功，TFTP 命令未返回提示符，最终 command_timeout，耗时 183824ms；Ruby 参考同样固定 180 秒。CLI 新增 tftp.upload_timeout / NC_TFTP_UPLOAD_TIMEOUT，默认 60 秒；底层 API Timeout 零值仍保持 180 秒，不自动重发上传命令。进度区分登录、上传、归档核验，预检结束后切换执行阶段，按阶段计时。

用户明确选择日期/设备名-IP.cfg，同一天只保留最新。修正每个 SSH client 随机分目录的问题，本地及 SSH pool 统一 UTC+8 日期。新增 daily_latest 能力协商，在上传前拒绝旧 worker；旧批次协议继续支持。每日模式在校验复制内容并同步后 rename 原子更新；失败不覆盖旧归档，拒绝符号链接目标；旧历史目录保留。报告输出服务器每日归档目录。

验证：双版本 Go1.26/1.27 候选源码配对全量 race/shuffle/vet 通过（GOMAXPROCS=2 限制测试进程并发；高并发全包运行曾触发既有 SSH 假进程测试的 5 秒期限，未改长测试期限）。覆盖静默上传超时、API 默认值、CLI 配置覆盖、同日重复成功更新、失败保留、SSH pool 同目录、旧协议兼容。作者复查未做独立审查。

重编译本机二进制并用原子替换发布，避免 macOS 原地覆盖可执行文件导致缓存签名失效。远端助手升级前保留旧文件，校验新 SHA-256 与能力，按日期预检 /srv/tftp/archive/2026-11-04 成功。证据 docs/validation/2026-11-04-daily-archive-deploy.json；没有触发设备批量任务或迁移历史文件。上传超时上限不代表已修复该设备的 TFTP 故障。

## 2026-11-04 配置约定、统一展示与报告差异

两报告选中同一 976 台；CLI 成功951、TFTP成功953，真正的成功/失败差异仅10.156.9.4、10.203.0.8。两台 description 均包含设备原生截断 UTF-8（孤立0xE2），在已 server_verified 的 TFTP 原文件与独立只读 CLI 捕获中一致；不是传输丢字节。共同23个连接/认证失败保持原状，历史报告不改写。

CLI 默认保留不能无损解码的原配置字节，明确 original-bytes 与字节位置警告；不生成.raw旁车、不替换为U+FFFD、不猜测为GBK。库API TextOnly默认仍严格，新增显式PreserveInvalidEncoding。补充防止截断UTF-8被错误识别为GBK的回归。新版只读重采两台：2成功0失败2编码警告，异常描述原字节与TFTP文件一致，哈希通过且无.raw。证据 docs/validation/2026-11-04-encoding-preservation-live.json。

backup.yml 由重复默认项精简为17行，仅保留本站TFTP地址、单根目录及Hillstone路由域例外，原文件以0600备份。默认值在程序中集中声明，976台TFTP纯计划逐项比较完全一致。CLI与TFTP共用renderBatch，文本报告/JSON报告路径明确，质量警告与失败分开。全量候选源码配对Go1.26/1.27 race/shuffle/vet通过，GOMAXPROCS=2。没有真实设备写入或全量重跑，没有发布或提交。

## 2026-11-04 CLI 二进制命名与 expect 补丁归属

用户要求主 CLI 改名 `netman`，归档 worker 改名 `tftp-archive`；命令树、帮助、完成脚本、构建与发行清单同步更新，模块导入路径与历史证据保留。go-expect 兼容补丁从本仓库移至同级 `../go-expect/patches/go-expect-compat.patch`，工作区准备脚本和隔离测试改为引用该位置。go-expect 当前脏源码已有补丁 API，因此没有重复应用或覆盖源码。

默认 NX-OS/Hillstone VRF 从同一驱动默认函数同时解析到计划和实际命令；修复后与此前 976 台 TFTP 目标逐项相同，SHA-256 为 `72411d56ccf90bf8d4333a64088c82edad85fcff2bf67f77d2a0d2a22b6f8793`。四平台交叉构建成功，本机 `bin/netman`、`bin/tftp-archive` 与 `bin/linux-amd64/` 已原子更新；中文帮助、版本、消费者、发行机制 11 项和工作区准备 6 项通过。Go1.26/1.27 候选源码配对的全包 race/shuffle/vet 均通过，`go mod tidy -diff` 与 `git diff --check` 通过。

普通 `go build -o bin/ ./cmd/...` 仍解析正式依赖 v0.1.2，因 `expect.Line`、`Pattern.IsZero/ValidateText` 缺失而失败。已构建二进制明确标记 `local-paired source`，不是发布依赖验收。go-expect 兼容版本目前尚未发布；更新固定依赖之前，标准命令无法独立构建。

## 2026-11-05 Go 模块改进实施

按模块审查方案修复进度阶段统计，并将连接器命令执行、会话日志从设备生命周期文件拆分；拓扑报告与持久化从任务编排拆分。Fleet 内部改用私有状态类型，导出的字符串状态与 JSON 字段保持原值。Bootstrap 在服务构造时解析常用配置并缓存编译后的 TFTP 策略；保留零值 helper 的默认上传超时语义。

发现工作流进度器与 CLI 分别在 stderr/stdout 渲染最终结果，交错后会重复显示“执行结果”。先加入失败回归，再让进度器 `Finish` 清理面板并只输出一行总耗时，结果状态与设备统计统一由 CLI 输出。另将默认非 verbose 事件的阶段详情格式化移出热路径。Apple M5 / Go 1.27.1 上十组样本：当前进度事件 133.6–136.7 ns/op、0 B/op、0 allocs/op；合成的旧详情格式化工作 313.4–315.4 ns/op、208 B/op、7 allocs/op。该比较只代表事件路径开销，不推断设备批次总耗时。

为配置加载、1000 邻居解析与计划、64 KiB/1 MiB/32 MiB 稳定归档读取建立基准；本轮未对这些路径做无 workload 证据的算法改写。归档大文件数据来自页缓存，不代表物理磁盘性能。

验证通过：`go test ./...`、`go test -race ./...`、`go vet ./...`、`git diff --check`；使用用户要求的本地 `../go-expect` replace 将 `cmd/netman` 与 `cmd/tftp-archive` 构建到 `/tmp`。独立只读复查没有发现本轮范围内可操作的问题。无真实设备批次、远端操作、提交或发布；改动仍在原有脏工作区。

## 2026-11-05 go-expect 正式依赖升级

Go 模块代理确认 `github.com/gatework/go-expect v0.1.3` 已发布并要求 Go 1.26。将 `go.mod` 固定到 v0.1.3，移除临时本地 replace，并更新校验和。`GOWORK=off go list -m` 确认解析到模块缓存中的正式版本。

基于该版本通过全包普通测试、race 测试、vet、`go mod verify` 和 `go mod tidy -diff`。原子更新 macOS arm64 的 `bin/netman` 与 `bin/tftp-archive`；两者中文帮助及版本均可运行，构建信息确认依赖 v0.1.3。SHA-256：`netman` `31d40192384b7050575d90b1848e31c85c30125cbccdc94f8c19f7467c539c2a`；`tftp-archive` `dc6ffb3dda25a64532142a6abf57f8a52a3a3e29c466eedbbe6ce1b6236ef7ec`。二进制标记为 development revision unknown，因为当前工作区尚未提交；这不是发行构建。没有安装远端归档程序或运行设备批次。

## 2026-11-05 厂商方言与显式 Registry

完成 [厂商方言重构记录](2026-11-05-vendor-dialect-refactor.md)：12 个内建平台改为具体 Dialect 对象；提示符、分页、异常、自动交互、配置/保存脚本按平台文件归属；配置清理、拓扑模板/验证与 TFTP 命令/回执拆为共享执行骨架和平台策略。`topologyparser` 不再按厂商名 switch，接口规范化共用内部包。Fleet、Bootstrap、Topology、Transfer 均注入显式 Registry，移除包级 `vendors.Profile/Names`。

只读复查后继续将固定文件名复用、来源文件和校验根要求、拓扑写入受限错误码以及 Juniper 远程配置导出迁入方言能力。新增测试捕捉并修复了 TFTP 预检未注入 Registry，以及无拓扑能力时 `BuildAudit` 的 nil 解引用。验证 `go test ./...`、`go vet ./...`、两个 CLI 构建、`git diff --check` 通过；无真实设备操作、无提交或发布。

验证：`go test ./...` 全仓通过；Ruby profile/TFTP 契约与已有连接会话、配置、拓扑、传输夹具均覆盖。未运行真实设备批次；没有生成仓库内二进制、提交或发布。性能只确认固定完成签名由每次调用编译调整为进程初始化编译，未推断设备批次耗时收益。

### 家族包重构跟进

为便于 Go 工程师按业务职责理解和新增平台，将原单层厂商文件迁入 `vendors/internal/platform/<family>`，文件名改为 `dialect.go`、`configuration.go`、`topology.go`、`tftp.go`、`inventory.go` 和 `remote_backup.go`。跨厂商策略骨架集中到 `vendors/internal/driver`；通用 TextFSM 引擎不再包含厂商匹配表达式和解析 spec。Registry 增加可选内建清单识别能力，fleet 覆盖顺序通过回归用例固定；新增保存成功签名与未确认边界测试。全仓 `go test ./...`、`go vet ./...`、`cmd/netman` 与 `cmd/tftp-archive` 构建及 `git diff --check` 均通过。独立只读复查没有发现 Critical、Important 或 Minor 问题。本轮没有连接设备、提交或发布。

## 2026-11-05 go-expect v0.2.0 依赖迁移与重建

`go.mod` 固定到正式 GitHub 模块 `github.com/gatework/go-expect v0.2.0`，`go.sum` 更新为对应校验和并移除旧版校验。该版本移除了旧的继续动作及匹配顺序 API；connector 迁移到 `ContinueKeepTimer` 和 `WithMatchOrder(expect.Declaration)`，保持计时与候选优先级语义不变。

验证：`GOWORK=off go test ./...`、`go vet ./...`、`go mod verify`、`go mod tidy -diff` 与两个 CLI 的中文帮助检查通过。macOS arm64 `bin/netman`、`bin/tftp-archive` 已先构建、校验模块版本后原子替换；二进制构建信息均为 go-expect v0.2.0，SHA-256 分别为 `5e86053b8058abf16e8af4bd6c4f8a36a7690f24aebf44848ce48a3fcb73e0b5` 和 `ab052172cef835c39e0a33827abd4dde8ce9425d4c941b135b8995791dc1f69f`。工作区仍有未提交改动，二进制标记为 development revision unknown；未连接设备、部署远端程序或提交代码。

## 2026-11-06 TFTP 预检与共享进度改进

TFTP 归档预检原先逐个连接 13 个 SSH 归档客户端，且没有自己的进度计数，设备进度因此停留在 `0/976`。改为最多 4 个并发预检 worker，向共享 Reporter 报告归档池阶段、完成数、活动数及预检并发上限；设备工作仍在所有归档预检通过后开始。界面分别标出“预检并发 4/4”和“设备并发 50”，避免混淆两种并发。完成阶段也与设备任务计数分开。

修正 TTY 面板相同布局下重复重绘标题的问题：进度刷新只改状态行，只有面板创建或布局变化时才重画标题。PTY 回归覆盖配置备份、TFTP 预检和 topology 应用，并向终端持续回送回车。普通终端也会显示预检阶段切换。

厂商能力审查确认 12 个内建方言的能力位与可选接口匹配，增加契约测试；另外修复档案能力位与实际缺少 topology 接口不一致时仍会打开设备会话的问题，改为离线返回“不支持”。

验证：`GOWORK=off go test -p 1 -count=1 ./...`、`GOWORK=off go test -race -p 1 -count=1 ./...`、`GOWORK=off go vet ./...` 与 `git diff --check` 通过。一次并行全仓运行中既有 `TestSSHMasterExitReapsDescendant` 偶发触及其 5 秒上下文期限；单测隔离运行与串行全包门禁通过，未调整其期限。独立审查代理遇到用量限制，作者完成限定范围的只读自查；未发现其他需修复的问题。未运行真实设备批次、未部署远端归档程序、未构建仓库内二进制、未提交或发布。

## 2026-11-06 厂商目录收敛与源码快照退役

将厂商家族包直接放在 `vendors/<family>`，删除 `internal/platform` 中间层；家族包保留独立的方言、清单匹配和可选能力，并增加包文档。共享拓扑解析目录改为 `vendors/internal/topology`，共享接口名称规范化包改为 `internal/ifname`，移除拓扑解析器内的名称转发，并将 `ConfigurationName` 精简为 `ConfigName`。保存回执验证并入 `vendors/internal/driver`，删除单调用方的 `saveproof` 包。

go-expect 已由 `go.mod` 固定到正式 `v0.4.0`，因此移除 `dependencies` 下的旧源码快照、校验清单，以及只用于重建该快照的脚本和专用测试。历史质量记录保留。厂商目录当前生产 Go 代码约 2,635 行、测试代码约 1,244 行；删除已退役流程的测试，没有为目录迁移堆叠重复用例。

验证：`GOWORK=off go test ./...`、`GOWORK=off go vet ./...`、`GOWORK=off go build ./cmd/...`、`GOWORK=off go mod verify`、`GOWORK=off go mod tidy -diff`、依赖版本检查与 `git diff --check` 均通过。未连接设备、未构建仓库内二进制、未提交或发布。

## 2026-11-06 厂商能力接口与诊断元数据

Topology、Transfer、Fleet 改为消费端拥有可选行为接口，删除 Profile/Device 重复能力布尔值；Radware 只暴露配置描述读取和接口身份。写入命令由平台 writer 生成，公共编排仍负责命令顺序、计划重验、读回和保存。内建目录用单个有序平台列表派生键查找及清单识别顺序。Fleet TFTP 预检直接断言 TFTP policy，不再复制 Profile。

独立审查指出解析器从旧 topology 错误类型迁出后，错误码退化为通用 StandardError。新增回归覆盖 malformed neighbor、重复接口描述和 Fortinet 版本/VDOM 检查，先复现失败再以 `dialect.DiagnosticError` 跨层保留 code/phase；确认输出不进入错误文本。全包 test/race、vet、命令构建、tidy-diff、diff-check 通过。Apple M5 基准为本轮迁移后数据，没有迁移前同机基线，不作性能提升结论。未连接设备、提交或发布。

## 2026-11-07 Go 命名一致性收敛

按全仓 API 与包内标识符审查结果收敛重复限定词：拓扑接口及写入限制类型、方法统一采用消费包已有上下文；配置采集策略使用 `Config`；H3C 构造函数改为 `h3c.New`。家族包私有实现移除重复厂商前缀，保留 IOS/NX-OS、Wireless 等业务区分。没有增加兼容转发别名；CLI、配置、报告和协议字面量未改。

更新拓扑/厂商现有调用方、外部包契约检查、消费者检查脚本及 API 迁移文档；厂商 README 增加命名规则。验证通过：`GOWORK=off go test -p 1 -count=1 ./...`、`GOWORK=off go vet ./...`、`GOWORK=off go build ./cmd/...`、`GOWORK=off python3 scripts/verify-consumer.py`（外部消费者、内嵌模板和 CLI 检查均通过，依赖解析为 go-expect v0.4.0）、`git diff --check`。旧 API 名称在 Go 源码中无残留。当前工作区仍包含之前的未提交改动；未提交、未发布。

### 生产 API 与包内命名复核

将 `topology.Topology` 改为 `topology.Client`，将 `topology.ConfigInterface` 函数及 `InterfaceIdentity.ConfigInterface` 改为 `ConfigName`；将 `dialect.Dialect.ConnectorProfile()` 改为 `dialect.Platform.Profile()`；Fleet 内建识别契约改为 `PlatformResolver.ResolvePlatform`。调用方直接迁移，没有旧名兼容别名。私有 `transferPattern` 改为 `pattern`。这些是源码命名迁移；CLI、配置键、报告字段、协议值、设备命令和运行逻辑保持不变。

逐目录审查生产代码，结论如下：

| 目录 | 审查结论 |
| --- | --- |
| `connector` | 保留 `ConnectionConfig`、`Profile`、`Operation` 及结果/回执名；各自区分连接、平台规则、操作所有权和证据。 |
| `config` | 保留 `Options`、`Snapshot`、`FieldError`；包上下文明确，且分别代表输入、加载状态和校验错误。 |
| `inventory` | 保留 `Device`、`Selection`、`Decision`、`AddressSet`，它们对应清单选择的不同领域对象。 |
| `netdisco` | 保留 `HTTPOptions`、`PostgresOptions`、`CacheOptions`，来源类型明确且配置边界不同。 |
| `transfer` | 保留 TFTP 与远程配置导出的策略、选项和回执名称，避免混淆两种协议流程。 |
| `archive` | 保留独立生命周期所有者 `Client`、`Pool` 及匹配的选项类型；`ErrArchive` 是跨协议、客户端和池的统一失败哨兵，没有更窄且准确的替代名。 |
| `storage` | 保留 `FileState`、`Receipt`、`BatchArtifact`、`WriteStream`，分别表示持久化状态、写入证据、批次产物和流式写入。 |
| `fleet` | 当时保留 `Outcome`、`RunOptions`、`Rules`、`Plan` 和 `ErrInvalidPlan`；本轮全模块复核进一步确认命名，见后文。 |
| `topology` | 完成 Client 与 ConfigName 收敛；保留 `Neighbor`、`Plan`、`Audit`、`Result`，表达发现、规划、审核和执行结果。 |
| `vendors` | 完成平台合约迁移；复核 registry、方言 DTO、所有厂商家族包及共享 driver/parser，厂商命令和可选能力名保留各自业务语义。 |
| `internal` | 复核 `bootstrap`、`buildinfo`、`cli`、`display`、`ifname`、`progress`、`terminal`、`textcodec`、`workflow`；短名称依赖包和接收者上下文，`ifname` API 为 `Canonical/Key/Short/ConfigName`。 |
| `parallel` | 保留 `Map`、`Result`、`ErrWorkerExit`，与 worker pool 操作和错误语义匹配。 |
| `textfsm` | 保留 `Parse`、`ParseTemplate`，包和参数类型已提供解析器上下文。 |
| `cmd` | 命令入口保持私有，二进制及命令词汇不变。 |

验证：`GOWORK=off go test -p 1 -count=1 ./...`、`GOWORK=off go vet ./...`、`GOWORK=off go build ./cmd/...`、`GOWORK=off python3 scripts/verify-consumer.py` 和 `git diff --check` 均通过；迁移前后的工作区状态项均为 232。没有为命名迁移新增行为测试；现有调用方、包测试和外部消费者契约检查覆盖 API 对齐。未连接设备、未暂存、未提交或发布。

### 2026-11-07 全模块生产命名深度复核

本节覆盖当前模块全部 36 个包、161 个非测试 Go 源文件，并作为本轮命名状态的记录。将 `fleet.ErrPlan` 改为 `ErrInvalidPlan`；共享厂商实现从 `ProfileDialect` / `NewProfileDialect` 改为 `ProfiledPlatform` / `NewProfiledPlatform`；`topology.Client` 使用 `client` 接收者名，避免与 `Change` 局部变量冲突。旧 error 文本 `netdisco: invalid plan` 保持原样。包名和 import path 均通过审查，无需调整。

| 包 | 审查结论 |
| --- | --- |
| `archive` | 保留 `Client`、`Pool` 和相应选项名，表示不同资源所有者；保留 `ArchiveDirectory`，以区别 `PoolOptions.Directory`；`ErrArchive` 是跨协议与生命周期的统一失败哨兵。 |
| `config` | 保留 `Options`、`Snapshot`、`FieldError`，由包上下文和类型职责确定含义。 |
| `connector` | 保留连接、Profile、Operation、响应、步骤与回执术语；方法按持有者命名，无额外包名前缀。 |
| `fleet` | `ErrPlan` 改为 `ErrInvalidPlan`；保留 `Outcome`、`RunOptions`、`Rules`、`Plan` 等不同批次阶段名称。 |
| `inventory` | 保留 `Device`、`Selection`、`Decision`、`AddressSet`，分别表达清单实体、筛选和地址集合。 |
| `netdisco` | 保留 `HTTP`、`Postgres`、`Cache` 及各自 Options，明确区分清单来源和缓存。 |
| `parallel` | 保留 `Map`、`Result`、`ErrWorkerExit`，匹配并发映射和 worker 退出语义。 |
| `storage` | 保留 `FileState`、`Receipt`、`BatchArtifact`、`WriteStream`，表示不同持久化状态与证据。 |
| `textfsm` | 保留 `Parse`、`ParseTemplate`，包名和参数提供解析器上下文。 |
| `topology` | 保留 `Client`、能力接口、`Plan`、`Audit`、`Result`；Client 方法统一使用 `client` 接收者名。 |
| `transfer` | 保留 TFTP 与远程备份的独立策略、选项及回执名称，避免混淆协议流程。 |
| `vendors` | 保留 `Registry`、`NewBuiltinRegistry` 和 `ResolvePlatform`，集中表达内建目录及解析职责。 |
| `vendors/dialect` | 保留 `Platform`、`Registry`、`InventoryIdentity`、`Neighbor` 等跨包契约和 DTO 名称。 |
| `vendors/centec` | 保留包内 `dialect`、TFTP 策略和 `New`；包上下文已明确厂商。 |
| `vendors/cisco` | 保留 IOS/NX-OS 变体名称，避免共享家族中平台身份混淆。 |
| `vendors/fortinet` | 保留包内平台与 TFTP 策略名称；共享 Profile 包装使用 `ProfiledPlatform`。 |
| `vendors/h3c` | 保留 `dialect` 与 `wirelessDialect` 区分交换机和无线控制器。 |
| `vendors/hillstone` | 保留包内 `dialect`、配置和 TFTP 策略名称；厂商由包名提供。 |
| `vendors/huawei` | 保留包内 `dialect`、配置和拓扑策略名称；厂商由包名提供。 |
| `vendors/juniper` | 保留包内 `dialect` 与远程备份/TFTP 策略名称。 |
| `vendors/paloalto` | 保留包内 `dialect`、配置与 TFTP 策略名称。 |
| `vendors/radware` | 保留包内 `dialect`、配置与 TFTP 策略名称。 |
| `vendors/topsec` | 保留包内单一 `dialect` 类型，平台身份由包名和 Profile 提供。 |
| `vendors/internal/driver` | `ProfileDialect` / `NewProfileDialect` 改为 `ProfiledPlatform` / `NewProfiledPlatform`；其余能力策略名保持职责化。 |
| `vendors/internal/topology` | 保留 `Parser`、`Pattern`、`Field`、`Count` 等解析操作名，由包路径限定语义。 |
| `internal/bootstrap` | 保留 `Services`、`DeviceFactory`、依赖注入构造与配置解析名称。 |
| `internal/buildinfo` | 保留 `Identity`，其值包含版本与提交身份。 |
| `internal/cli` | 保留 `Runtime`、`Run` 及命令构造职责名称。 |
| `internal/display` | 保留 `Status`、`Section`、`Table`、`Text`、`Clip` 等显示操作名。 |
| `internal/ifname` | 保留 `Canonical`、`Key`、`Short`、`ConfigName`，已由包名提供接口名称上下文。 |
| `internal/progress` | 保留 `Reporter`、`Event`、`Phase`、`Outcome` 等进度域名称。 |
| `internal/terminal` | 保留 `Renderer` 及终端渲染错误与方法名。 |
| `internal/textcodec` | 保留 `Decode`、`BackupView`、`PlanView`，分别表达转换和呈现视图。 |
| `internal/workflow` | 保留 `Runner`、`Backup`、`TFTP`、`Topology` 及其选项/结果名，区分业务流程。 |
| `cmd/netman` | 保留 `main` 私有入口，目录名与可执行命令一致。 |
| `cmd/tftp-archive` | 保留 `main` 私有入口，目录名与服务端可执行命令一致。 |

现有测试文件没有 `ErrPlan`、`ProfileDialect` 相关引用需要同步；本轮未审查或运行测试用例。验证计划仅覆盖生产构建、活跃源码旧名扫描、字面量保护检查与差异格式检查。

## 2026-11-07：审查改进项实施

- 批次报告 schema 版本与设备记录选择移入 `internal/batchreport`，由 Fleet 写入、storage 导出共同使用；storage 保留目录锚定、安全读取、大小限制与内容哈希核验。
- 报告导出保留 IPv4-mapped IPv6 地址族，与 Fleet 计划身份规则一致；同一报告中的 IPv4 与 mapped IPv6 记录不会再被导出层折叠为重复主机。
- PostgreSQL pgx 连接设置协议消息体上限，按 `min(MaxResponseBytes, MaxInventoryBytes)` 加 1 MiB 行与列元数据余量计算，避免只在完整接收后才检查应用预算。
- Archive 在接受 `archived` 帧前先校验字节数和摘要；`Receive` 校验传入 proof 与保存帧一致，失败清除 `Verified` 并禁止重放已消费的数据流。worker 批次名固定正则改为包级预编译。
- 实施方案统一为组合根显式构造注入；命名记录中的 `ErrPlan` 旧写法改为 `ErrInvalidPlan`。
- TFTP 证据路径改为定长分块渲染，不再复制整段输入做 CR 替换；失败检测直接扫描原文与渲染文本，不保留多份逐行切片。完成判定只保存最终证据行，超过 65,536 行就返回未确认错误，避免极端短行输入导致内存随行数增长；失败词使用无分配的 ASCII 扫描，远程拒绝提示按整段文本匹配并忽略命令回显。终端渲染器通过 `io.WriteString` 输出换行，消除每行临时字节切片。

性能基准：在 Apple M5 / darwin arm64 上运行 `GOWORK=off go test ./transfer -run='^$' -bench='^BenchmarkEvidence$' -benchtime=200ms -benchmem`。16 MiB 场景当前为 69.5 ms、1,386 B/op、19 allocs/op；历史基线为 160.5 ms、374,361,292 B/op、2,349,030 allocs/op。当前基准额外包含流式失败词判定，历史基线只测行证据抽取，数字仅供趋势参考，不作严格同负载比较或实机性能保证。

验证：`GOWORK=off go build ./...`、`GOWORK=off go vet ./...`、`git diff --check` 通过。完整测试未运行；未连接设备，未提交、推送或发布。

### 2026-11-07 私有命名范式跟进

- 将配置读取 helper 从 `str/number/boolean/duration/list/mapping` 收敛为 `stringValue/integerValue/boolValue/durationValue/stringList/stringMap`；路径、集合、读取和校验 helper 使用 `absolutePath/uniquePaths/readFile/hasPath/setPath/mergeMaps/validateValues` 等职责名。YAML 节点递归、深拷贝和 map 合并分别改为 `parseNode/cloneValue/mergeValues`；环境变量路径及整数转换 helper 也使用完整语义名称。
- 将 `invError/fmtError` 等缩写错误构造器改为 `newInventoryError/configurationError`；connector、topology、transfer 内部统一使用 `newError`。拓扑能力判断由含义不明的布尔参数改为传入 `mode`；同一状态判定改用 `succeeded/partiallySucceeded/isSuccessfulStatus`。
- 统一可选字符串、诊断清理、显示回退、文本清理和私有文件名判定等 helper 名称；删除无调用的 `transfer.pattern()`，远程拒绝规则使用 `pattern`，判断函数命名为 `remoteRejected`。报告字面量、配置键、错误码、CLI 和协议行为未改。
- 复核还发现诊断 allowlist 在每次清理时重复建表，改为包初始化时构造只读集合；读取文件的布尔参数改为 `missingOK/includeContents`。设备会话的 `read/send` 改为 `readResponse/sendLine`，归档协议的 `receive/send/persist/lock` 改为 `readRequest/writeFrame/persistArchive/lockArchiveTarget`；CLI 参数处理器明确为 `noArguments/bindTFTPFlags`。Palo Alto 配置正文清理和 TFTP 证据入口也使用职责化名称。失败词边界先过滤再定位行，避免不匹配短语对超长单行输出反复回扫。

验证：`GOWORK=off go build ./...`、`GOWORK=off go vet ./...`、`git diff --check` 通过。未运行完整测试，未连接设备，未提交、推送或发布。

## 2026-11-07 Go 模块改进实施续

修正架构图的依赖方向：CLI 作为组合入口构造 bootstrap 依赖并注入业务工作流；workflow 不再被文档画成依赖组合根。同步旧实施账本中已被 2026-11-03 决策取代的多 CLI 二进制/默认值条款，明确统一 netman 命令树与独立 tftp-archive worker。

基线检查发现两处归档测试夹具不符合协议：阻塞 I/O receive 子用例没有匹配的 archived proof；deadline 用例缺少必需的 archive_path，并使用过紧时间窗。仅修正测试输入和时序，未改生产协议实现。定向 deadline 测试重复 5 次通过；修正夹具后 `GOWORK=off go test ./...` 全包通过。未连接设备或执行远端操作。

### 配置默认值与 bootstrap 解析

将 batch parallel、inventory 资源限制、数据库端口/连接超时和 per-vendor limit 的应用默认值收敛到 `config.Defaults`；bootstrap 类型化解析 helper 不再重复默认字面值。TFTP 离线预检现在先解析同一 `resolvedSettings`，避免构造未初始化的 `Services` 后上传超时误报为 0。新增默认覆盖与缺少必需并发默认值测试。

验证：新增默认测试先失败后通过；`GOWORK=off go test ./config ./internal/bootstrap -count=1` 通过，目标文件 `git diff --check` 通过。保留 `netdisco.Limits.normalize` 对直接库调用的零值默认，不让通用 config 包依赖 netdisco。未执行设备操作。

### workflow 组合根依赖倒置

移除 `internal/workflow` 对 `internal/bootstrap.Services` 的生产依赖。工作流现在接收本包定义的运行时输入、设备打开器和 session observer；CLI 组合根将已解析的 Fleet 规则、Registry、并发、目录与设备构造适配后注入。TFTP 和 topology 的运行配置分别转换为类型化设置，workflow 不再通过字符串路径读取这两类配置。TFTP 归档密码仍在预检阶段按需读取；设备连接、日志同步与关闭仍由原有资源所有者完成。

验证：`GOWORK=off go test ./internal/workflow ./internal/cli -count=1` 通过；`go list` 检查 workflow 的生产导入不含 bootstrap；`git diff --check` 通过。回归覆盖 TFTP 归档预检失败时不创建设备，以及 topology 计划保存失败时保留已采集结果。未执行网络设备操作。

### Fleet 报告契约收敛

`internal/batchreport` 现在定义 schema v2 的类型化 `Summary`、coverage、verification、diagnostic 和设备 DTO；Fleet 通过同一 DTO 写 JSON，安全导出也解码这份契约。`Report.Summary()` 和 `Statistics()` 继续提供原有通用 map 入口，避免 CLI/调用方因 DTO 改造而改写。backup/TFTP 设备行由 DTO 按模式保留既有字段、省略规则和 null 语义；导出仍要求 TFTP 具备 `server_verified`、可信本地归档路径、字节数和 SHA-256。

验证：新增 schema round-trip、backup/TFTP 字段形状和 TFTP 未核验拒绝用例；`GOWORK=off go test ./internal/batchreport ./fleet ./storage ./internal/cli -count=1` 通过。测试发现并修正了 DTO map 视图的 `encoding` 类型和空路径/摘要 nullability，使其与既有行为一致。未执行设备操作。

### Vendor capability 边界复核

按 12 个规范平台检查 Profile 配置/保存脚本及拓扑读写、TFTP、Juniper 原生远程备份能力。家族包直接实现具体命令与回执策略；`vendors/internal/driver` 只承载共享 Profile、配置策略、正向保存证明和通用传输校验；Topology/Transfer 能力由消费包定义。现有家族构造与能力接口已经符合此方向，本阶段未为目录整齐或单次复用增加包装层。`vendors/README.md` 新增声明能力矩阵与源码证据边界，明确不代表实机兼容性。

验证：`vendors/profiles_test.go` 已覆盖全部 12 平台配置命令、保存脚本与各能力接口矩阵；`GOWORK=off go test ./vendors/... ./fleet ./topology ./transfer -count=1` 通过。矩阵仅来自源码声明和离线测试，没有连接设备。

### Topology、Transfer 与 Archive 生命周期

TFTP `upload_timeout` 现在同时限制平台的源文件发现和上传命令；原先每条命令可各自使用完整超时，导致总耗时超出配置值。为整次 `Device.WithOperation` 增加共享 context deadline，平台命令期限仍保留但不能越过总 deadline。Topology 保留已有的写前计划校验、Apply 时重采证据、单租约、读回验证和保存回执；Archive Pool/Client 已有取消、并发关闭与目标事务所有权，本轮未重复引入阶段状态副本。架构文档增加 Pool→Client→Protocol→worker 的资源顺序、Close 逆序和 `device_reported`/`server_verified` 证据边界。

验证：`TestUploadTimeoutCoversSourceDiscoveryAndTransfer` 先以旧逻辑失败（上传阶段又获得 501ms），加总期限后重复 5 次通过；`GOWORK=off go test ./transfer ./topology ./archive -count=1` 通过。全部为离线测试，没有访问真实设备或远端归档服务器。

### 清单、缓存、解析与存储边界

复核确认 `inventory` 只保存设备值和筛选规则；HTTP/PG source 在 `netdisco` 内持有请求/连接行为，缓存只在完整 source 成功后写入，分页或数据库事务中途失败都不返回部分清单。PostgreSQL 缓存身份已排除密码并包含连接和查询参数；HTTP 缓存身份改为类型化描述，包含规范来源 URL、认证方式、非敏感用户名/账户命名空间及 endpoint/fields/page size/fallback 查询形状，不再把 API key 原文参与身份哈希。API key 轮换而来源与查询不变时身份稳定；API-key-only 启用缓存时必须配置非敏感 `ND_USERNAME` 或 `ND_CACHE_NAMESPACE`，否则在读取/执行清单前报错，防止不同主体复用缓存。缓存状态字符串集中为 `CacheStatus*` 常量；错误仍走 Go `error`，`only` 是离线读取模式，返回的 freshness 与现有契约一致，不增加重复的 offline/error 状态字段。

地址核对结果：任务目标和持久化 artifact 都要求 IP 地址，不接受 hostname 或带 zone 的 link-local IPv6；筛选器现也在构造时拒绝这两种输入。IPv4-mapped IPv6 保持自身地址族，Fleet 的重复主机检查仍在计划阶段统一执行；HTTP/PG source 仅合并完全相同的设备行，保留同一 IP 的不同记录供计划显式标为重复。TextFSM 的模板编译结果只读共享、每次解析克隆可变 Values 并创建独立 ParserOutput；输出 token 上限保持 32 MiB。storage 只承担单次安全文件事务和批次目录锚定，没有跨调用根目录状态，因此保留函数式 API，不增加 Store 包装对象。

命名复核把 connector 的配置采集内部 `backup/preserve` 双布尔参数改为 `configTextMode`，区分普通呈现、原始备份和保留无效编码三种实际路径，排除了不可能的布尔组合；其余已检查名称在包上下文中清楚，公开 API、配置键、JSON/CLI 字面量和设备行为没有为缩写而改名。

性能基线：`go1.27.1 darwin/arm64` / Apple M5；离线 fixture，`-benchmem -count=3`。Fleet 计划 1,000 台为 1.15–1.33 ms、1.99–2.02 MB/19,096–19,099 allocs；10,000 台为 12.13–12.23 ms、27.65–27.67 MB/约 190,205 allocs。TextFSM 索引模板单条约 1.59 µs/6.51 KB/20 allocs，1,000 行约 0.84 ms/1.24 MB/14,017 allocs。HTTP JSON 映射 100,000 台为 129.3–131.0 ms、88.64 MB/1,501,783 allocs；这是观察到的后续内存优化候选，当前无同负载历史基线，未冒险改写保持大小写敏感和 last-key-wins 语义的 JSON 解码器。Transfer 证据扫描 16 MiB 为 67.0–69.9 ms、1,386 B/19 allocs；进度成功事件约 137 ns、0 allocs；Archive 本地稳定读取/哈希 32 MiB 为 11.25–11.38 ms、约 65.9 KB/10 allocs。该数据只用于本机同版本后续比较，不代表实机批次耗时。

验证：HTTP 身份和 API-key 命名空间回归先失败后通过；覆盖 key/password 轮换、不同账户隔离、缺少身份时报错、`ND_CACHE_NAMESPACE` 环境变量及 CLI 中文提示。地址规则回归也先失败后通过。完整 `go build ./cmd/...`、`go vet ./...`、`go test ./...`、相关包 race 检查、`go mod verify` 与 `git diff --check` 全部通过。没有运行设备、真实库存服务或远端归档。

### 独立审阅与交付验收

独立 reviewer 发现 API-key-only 不同主体可能复用同一清单缓存。按“缓存身份不包含 secret”约束，新增非敏感 `ND_CACHE_NAMESPACE`；如 `ND_USERNAME` 已提供则可直接用作主体标识。启用 API key 与缓存时，未提供任一标识会在读取清单前停止，并提示如何设置；不同 namespace 划分缓存身份，API key 轮换不改变身份。空 namespace 作为可选配置有效，不写入密钥或报告。

复审确认身份碰撞在不同主体使用不同非敏感标识时已解决，未发现其他 Critical/Important 问题。最终仅完成本地改动，保留工作区其余未提交内容；未提交、推送、发布或连接真实设备/库存服务。

### 2026-11-07 完成审计补齐

再次对照模块改进方案检查当前工作区时，补齐了两处边界：批次报告中的原始备份来源、文件回执、来源分段和文本编码告警改为报告包拥有的显式 DTO，并由 Fleet 显式映射，字段和值与原 schema v2 相同；`Report.Summary()` 继续返回既有备份元数据 Go 类型，并通过克隆避免 map 调用方修改报告状态。Fleet 内部将 backup/TFTP 模式改为私有类型，计划选项、报告和 JSON 仍使用既有字符串契约。Archive worker 按协议帧、目录与文件描述符安全、单次事务编排拆分到 `worker_protocol.go`、`worker_files.go` 和 `worker.go`；同时删除未调用的 `persistArchive` 转发函数，保留实际事务与协议顺序。规范化 `vendors/paloalto/topology.go` 的 import 排序。

验证：`GOWORK=off go build ./cmd/...`、`go vet ./...`、`go test -p 1 -count=1 ./...`、核心资源/回执包 `go test -race -p 1 -count=1 ./archive ./connector ./fleet ./internal/batchreport ./internal/bootstrap ./internal/workflow ./storage ./topology ./transfer`、`go mod verify`、`go mod tidy -diff` 和 `python3 scripts/verify-consumer.py` 通过；Linux/Darwin × amd64/arm64 四组 `CGO_ENABLED=0` 命令交叉构建通过。首次 `goimports -l .` 找到 Palo Alto 拓扑文件一处 import 排序，修正后复查通过。全程离线 fixture 和临时消费者环境，没有访问网络设备、真实库存源或远端归档服务。

独立复核又覆盖了报告与 JSON 的混合产物形状，发现 TFTP 失败结果带有 backup 元数据时泛用汇总会多出两项字段。新增该组合用例，确认旧实现先失败；按模式限制兼容字段后通过。复核者确认本轮 DTO 与 Archive 文件拆分无 Critical/Important 问题。

## 2026-11-07：归档 v2、默认值打印与单文件配置

本轮按确认方案实现归档事务阶段期限与完整回执，并根据后续要求收敛配置入口。

- 归档协议直接升级到 v2；版本及阶段预算在文件操作前验证。准备/排队、设备上传、核验及本地持久化、清理默认分别为 240/180/100/10 秒。Pool 排队与 Client 准备沿用同一绝对期限，ready 成功交付后才开始完整上传窗口；正文、尾帧及本地提交共用 Verify 期限，cleanup 收到后才切入清理窗口。移除 Protocol.Expect 隐式刷新期限与固定 280 秒。阶段取消仍受父 context 约束，借用回调与 Sync/Fsync 保持合作式取消。
- `archive.Result` 分开保存设备报告与 `archive.Receipt`。后者保留服务器证明、本地 storage 回执、清理确认状态；只有服务器核验、本地 durable 且没有前序错误才清理。上传已报告但返回错误时仍可核验；清理失败保留成果且批次不成功。Fleet、事件与 schema v2 报告统一映射这些事实，保留既有投影字段并新增 archive 详情。
- 修正 TFTP 实际存放于批次 `tftp/` 而导出只查根目录的问题。由报告模式选择固定位置，目录 FD 与 NOFOLLOW 同时约束中间子目录和最终文件；不信任报告绝对目录，支持整体搬移后的导出。
- 增加 `netman config defaults [--for backup|tftp|topology] [--json]`，默认纯 YAML，不读取配置、环境覆盖、凭据、标准输入或清单。初始化与自动发现只使用平铺 `net-connector.yml`；旧 backup.yml/topology.yml 自动发现、common 容器及用途重复嵌套移除。现有文件不被自动删除或改写，`.env` 仍用于凭据。新设置的默认值和 worker 路径由 archive 定义，config.Defaults 与库零值复用；工作流只传递一份 ArchiveTimeouts，移除重复 UploadTimeout 字段。
- 补充 GLOSSARY、迁移、归档协议、架构、README、同源命令参考与实施账本。源码消费者检查现在覆盖公开归档 v2 返回类型、实际本地事务及 defaults 输出。

失败边界验证覆盖：同名目标锁等待后的完整上传窗口、Pool 队列与目标准备共用期限、600 秒上传预算、ready 慢发送及已交付控制帧跨期、启动器超时保留取消与 I/O 错误树、设备已报告但上传截止后继续核验、本地提交前后失败、delivered 后慢本地提交、清理超时/丢确认、主错误不被次要持久化错误覆盖、内部取消不冒充用户中断、协议超时诊断保留 Timeout::Error、真实 Upload→Report→搬移→Export、目录/文件符号链接与根目录同名产物。独立只读复核发现的上述错误分类和阶段交接问题均已修正并由回归用例固定；最终复核未发现其他实质问题。

本地验证环境：Go 1.27.3 / linux amd64。起始根分区已满，清理可再生 Go 构建缓存后完成验证，未删除仓库资料；使用 /dev/shm 与独立构建缓存控制磁盘用量。

通过的检查：

- `GOWORK=off go build -p 1 -o /tmp/netconnector-v2-bin/ ./cmd/...`
- `GOWORK=off NC_TEST_ARCHIVE_WORKER=/tmp/netconnector-v2-bin/tftp-archive go test -p 1 -count=1 ./...`
- `GOWORK=off go test -race -p 1 -shuffle=on -count=1 ./...`
- `GOWORK=off go vet -p 1 ./...`
- `GOWORK=off go run golang.org/x/tools/cmd/goimports@v0.52.0 -l .` 无输出；`git diff --check` 通过。
- `GOWORK=off python3 scripts/verify-consumer.py` 通过，仍消费正式固定 go-expect v0.4.0，无外部依赖替换。

本轮仅有离线 fixture、本地管道、临时文件及安装后的本地 worker 证据；未访问生产设备、真实清单或远端归档，未提交、推送、发布或部署。协议 v2 需要客户端与 worker 同步升级；旧用途 YAML 需要一次性人工迁移。

收尾回归补齐阶段 deadline 设置失败的退出路径：切换阶段时先清空已停止的回调，避免设置新 deadline 失败后再次等待旧回调；取消后的错误帧不再尝试写入。失败注入用例及 `go test -race -p 1 -count=1 ./archive ./fleet` 通过。

## v0.2.0 发布准备

用户明确授权发布到 GitHub。因协议、配置格式和公开 API 变更，版本从 v0.1.0 升为 v0.2.0，并补充发行及迁移说明。保留无关的未跟踪 `docs/superpowers/` 草案，不纳入发布。计划推送本地 main 到同名新分支，不修改仓库可见性或默认分支；等待该提交完整 CI 和不可变打包通过后，校验并发布同一份 CI 产物，最后读回标签与附件身份。此段只记录发布准备，不预先声明发布成功。

首轮远端质量检查发现历史重构后未使用的函数、字段、正则与测试辅助函数，以及 CLI 两处无效赋值。删除死代码，`--all` 在环境参数转换前设置，省去重复转换及被覆盖的错误；交互式配置直接声明实际加载结果。按 CI 原有质量门禁修复，未降低检查标准。

Linux Go 1.26 race 运行进一步暴露测试夹具的 20/80ms 总准备期限会被真实目录 fsync 和调度消耗。无 deadline 能力的通道拒绝用例恢复正常准备预算，仍以独立 watchdog 检查退出；已交付帧跨期限用例保留跨期条件，扩大为 1s 阶段/1.2s 延迟，留出真实文件同步空间。未调整生产默认期限或放宽成功断言。额外本地发布机制测试因 /dev/shm 缓存已满首次失败，换用根分区独立缓存后 11 项全部通过。

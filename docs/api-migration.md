# Go API migration

The generic engine is maintained in go-expect; this module owns the only device
layer. Resolve the pinned GitHub dependency with GOWORK=off. Do not run the old
workspace reconstruction scripts for current builds; retained baselines/patches
are historical audit material.

## Pending: shared inventory collection and evidence ownership

The CLI arguments, configuration fields, report schema and device commands remain
unchanged. Inventory cache identity has an explicitly approved migration: refresh
the source under the new identity before depending on cache-only operation.

Callers that currently assemble `NewHTTP`/`NewPostgres`, identity JSON and
`NewCache` can replace that assembly with one collection call:

```go
result, err := netdisco.FetchHTTP(ctx, netdisco.HTTPOptions{
    URL: url, Username: username, Password: password, APIKey: apiKey,
    RequireHTTPS: true, Limits: limits,
}, netdisco.FetchOptions{
    Mode: "prefer",
    Cache: &netdisco.CachePolicy{Directory: cacheDir, Namespace: account},
})
// Retain result.Status, FetchedAt, RefreshErrorCode and WriteErrorType with Rows.
// Successful fresh rows can coexist with a cache-write error.
if errors.Is(err, netdisco.ErrCacheRefreshRequired) {
    // Explain that prefer/refresh must collect this source before only can work.
}
```

`FetchPostgres` accepts the existing `PostgresOptions` and the same `FetchOptions`.
An empty mode means `prefer`; a nil cache policy disables caching. Source limits
also bound cached inventory. The existing low-level constructors and custom
`Source` interface remain supported; they retain caller-owned identity behavior.

The shared entry points compute one versioned digest from source kind, canonical
URL or effective PostgreSQL connection, principal, namespace and query shape.
The HTTP query shape includes fields, page size and fallback behavior. PostgreSQL
includes effective endpoints, database/user, session parameters, query and bound
parameters; passwords are excluded. Only the final opaque digest is persisted.

- `prefer` and `refresh` collect under the new identity; they do not relabel,
  copy, delete or fall back to legacy entries. Normal fresh/stale fallback applies
  after an entry has been collected under the new identity.
- `only` never connects or queries a source. A missing or invalid identity entry
  remains `InventoryError.Code == "cache_unavailable"`; use `errors.Is` with
  `ErrCacheRefreshRequired` to distinguish the actionable refresh condition from
  unsafe cache permissions or links. PostgreSQL still parses effective connection
  settings, including its environment and local TLS/service configuration.
- For API keys, an explicit namespace identifies the account first; otherwise a
  username does. If neither is present the library privately derives the principal
  from the key, preserving callers that already supported key-only caches. Key
  rotation then invalidates that cache. Raw keys and separate key fingerprints are
  never persisted or formatted. `netman` retains its existing username/namespace
  requirement; callers with stricter policy should keep it at their input boundary.
- `FetchHTTP` closes its owned idle transport on every return. An injected client
  or custom default transport is borrowed and is never closed. Low-level `NewHTTP`
  users can call `CloseIdleConnections` to release an owned default transport.

Backup consumers should use `HasSavedEvidence`, `Complete`, `HasUnverifiedPair`
and `SourceEvidence` instead of reinterpreting storage receipts. Saved evidence
does not imply a complete backup. `HasUnverifiedPair` applies only to paired
raw/text receipts; source-less legacy receipts keep their existing Fleet behavior.
Progress and file reports now use the same source-evidence interpretation.

Internal workflows retain typed topology state, diagnostics and statistics.
`TopologyResult.Summary()` returns a presentation snapshot; modifying it cannot
change apply eligibility or later report persistence. Text and JSON derive from
the same typed report. CLI action validation produces one concrete request;
configuration cannot promote a planning command to apply.

## Borrowed session operations

`Device.WithBorrowedOperation` holds the device lease across a sequential callback
and requires a session established by an earlier successful `Connect`. It never
connects, reconnects or replays commands, including through `RunningConfig` and
`SaveConfig`. Use the callback's context for all nested work:

```go
operationErr, cleanupErr := device.WithBorrowedOperation(ctx,
    func(ctx context.Context, op *connector.Operation) error {
        result, err := op.ExecuteCommand(ctx, command, connector.ExecuteOptions{
            PreserveSessionOnRejection: true,
        })
        if errors.Is(err, connector.ErrDevice) && op.Connected() {
            // A complete, verified rejection is evidence, not an accepted step.
            response := result.Attempts()[0].Response()
            output, textErr := response.Output()
            if textErr != nil { return textErr }
            _ = output // Inspect explicitly; normal diagnostics omit raw output.
            return nil // The caller has handled the rejection.
        }
        return err
    })
// Always inspect both results. Cleanup can fail even when operationErr is nil.
return errors.Join(operationErr, cleanupErr)
```

`PreserveSessionOnRejection` is opt-in and only accepted by a borrowed operation
executing one command. Ordinary `Device`/`WithOperation` command calls and empty
or multi-command scripts reject it before I/O with `ErrInvalid`; configuration
save does not accept it. Preservation requires exactly one attempted command,
a complete prompt, valid decoded/rendered text, intact output budgets and a live
context. The final error must be only the device rejection after all command and
operation observers and logging have finished. Any other failure retires the
session. Rejected commands remain in `Attempts`, never `Steps`; receipt and step
callbacks run only for accepted responses. A handled rejection can be followed by
an explicit recovery command in the same lease. Returning an error from the
borrowed callback retires the session even if the individual rejection was safe.

`Operation.Retire` explicitly retires the session while leaving the `Device`
available for a later explicit `Connect`. It works after cancellation, rejects
observer reentry with `ErrBusy`, and rejects a retired or expired operation with
`ErrScope`. `Operation.Connected` is a snapshot and becomes false after retirement
or scope expiry. An operation cannot execute more commands or collect/save
configuration after retirement. Commands are never retried implicitly.

The borrowed scope returns operation and cleanup errors separately. The first
cleanup error survives callback cancellation, admitted-call draining and ignored
`Retire` errors, including a callback that returns nil. Scope exit never retries
retirement. `Device.Close` remains terminal, cancels active I/O without waiting
for application callbacks, and can explicitly retry a failed transport cleanup
after the scope returns. Ordinary execution and `WithOperation` retain their
existing default failure and connection behavior.

## Archive v2 and one configuration document

- `archive.ClientOptions.Timeout` becomes `Timeouts`; `PoolOptions` also accepts the same `archive.Timeouts`. `archive.DefaultTimeouts()` supplies the only archive defaults, also used by config output. Zero option fields select defaults; explicit configuration values must be positive and representable.
- `NewProtocol(input, output)` no longer accepts a timeout. Every Send/Expect/Receive requires a deadline-bearing context; the transaction owner passes one context through each entire stage.
- Client.Upload and Pool.Upload return `(archive.Result, error)`. Check both `result.Transfer` (device report) and `result.Receipt` (Server proof, Local storage receipt, Cleanup state), including on errors. `Receipt.Clone()` detaches the mutable proof pointer.
- `transfer.Receipt.Verified`, `VerificationServerVerified`, `ServerSHA256`, `ServerBytes`, `ArchivePath`, and `LocalPath` are removed. Device-only transfers cannot establish server verification. Read those facts from `archive.Receipt.Server` and `.Local`; use `.Verified()` for independent server delivery and `.Complete()` for verified/durable/cleanup-complete.
- `fleet.Artifact.Archive` carries archive evidence alongside `.Transfer`; callers using Pool.Upload map both returned values. Strict/selected policies fail incomplete archive transactions too. Report schema v2 keeps its existing projected fields and adds optional `archive` detail.
- Wire protocol is v2 only: client and worker must upgrade together. Preflight rejects old workers before device commands. No automatic upload retry or installation is introduced.

Only `net-connector.yml` is automatically discovered. `backup.yml` and `topology.yml` discovery, the `common` container, and nested scope documents are removed. Merge their environment-specific sections into one flat document:

```yaml
netdisco:
  url: https://netdisco.example.invalid
tftp:
  server: 192.0.2.9
  verification_root: ssh://archive@192.0.2.9/srv/tftp
backup:
  directory: ./backups
# Optional: topology.parallel overrides shared backup.parallel.
```

From an old combined document, lift `common.netdisco/inventory/ssh`, `backup.backup/tftp`, and `topology.topology` to their top-level groups, resolving repeated settings deliberately. YAML paths resolve relative to the unified file. Existing files are never deleted or automatically rewritten. Explicit `--config` still allows a custom filename containing the same flat structure. `.env` remains a separate credential input, not another YAML configuration.

`netman config defaults [--for backup|tftp|topology] [--json]` prints built-in defaults (YAML by default) without loading files, environment overrides, credentials or inventory. `config show` prints effective loaded configuration instead. Initializers now create `~/.config/net-connector/net-connector.yml` and never overwrite an existing file. Avoid copying all defaults into site policy: keep only actual overrides, especially since TFTP defaults to verified while ordinary backup defaults to selected.

## 2026-11-07 Go naming cleanup

The project uses package and capability context instead of repeating those
words in identifiers. These source names were migrated directly; no forwarding
aliases remain. CLI flags, configuration keys, report fields and protocol
values are unchanged.

| Previous API | Current API |
| --- | --- |
| `topology.ConfigurationInterface` | `topology.ConfigName` |
| `topology.InterfaceDescriptionReader/Writer` | `topology.DescriptionReader/DescriptionWriter` |
| `topology.TopologyWriteDenial` | `topology.WriteRestriction` |
| `InterfaceIdentity.ConfigurationInterface` | `InterfaceIdentity.ConfigName` |
| `DescriptionWriter.TopologyWriteMode` | `DescriptionWriter.WriteMode` |
| `WriteRestriction.TopologyWriteUnavailableCode` | `WriteRestriction.UnavailableCode` |
| `topology.Topology` | `topology.Client` |
| `dialect.Dialect` | `dialect.Platform` |
| `Platform.ConnectorProfile()` | `Platform.Profile()` |
| `fleet.InventoryResolver.ResolveInventory` | `fleet.PlatformResolver.ResolvePlatform` |
| `fleet.ErrPlan` | `fleet.ErrInvalidPlan` |
| `driver.ProfileDialect` / `NewProfileDialect` | `driver.ProfiledPlatform` / `NewProfiledPlatform` |
| `h3c.H3C()` | `h3c.New()` |
| `driver.ConfigurationPolicy` / `NewConfigurationStrategy` | `driver.ConfigPolicy` / `driver.NewConfigStrategy` |
| `driver.BaseConfigurationPolicy/Factory` | `driver.BaseConfigPolicy/BaseConfigFactory` |
| `driver.InterfaceDescriptionPolicy/Writer` | `driver.DescriptionReader/DescriptionWriter` |
| `driver.NewInterfaceDescriptionWriter` | `driver.NewDescriptionWriter` |
| `driver.TopologyWriteDenialPolicy` | `driver.WriteRestriction` |

| Previous API | Current API |
| --- | --- |
| Device.Name / Operation.Name | ProfileName (the profile key, not a hostname) |
| Interaction.Response | Reply (verbatim bytes) |
| hook Executor.Get / Set | Value / SetValue |
| Command.Timeout pointing to zero to poll | Command.Poll = true |
| Interaction.Limit = 0 for repetition | Unlimited = true |
| untyped protocol/trust/state strings | Protocol, HostKeyPolicy, storage.FileState, transfer.Verification |

Command.Timeout nil inherits the configured default; zero disables the relative
command timer. Poll is an explicit immediate readiness operation and rejects a
positive Timeout. Parent context deadlines still apply. Sends remain bounded by
the configured write timeout. Limit zero permits one reply; positive values bound
replies; negative values and Unlimited combined with a positive Limit are invalid.
Built-in vendor rules explicitly preserve their former repetition and reply bytes.

Close is terminal. It cancels the admitted operation and its callback contexts,
interrupts I/O and rejects subsequent valid operations with ErrClosed. It does
not wait for cooperative user callbacks. A failed operation retires its session;
ordinary commands return ErrNotConnected until an explicit Connect succeeds.
No device command is automatically replayed. Constructors are required; do not
copy Device, Session or other owners of synchronization/resource state.

Result.Steps keeps its accepted-main-command meaning and existing JSON format.
Result.Attempts separately records main and hook-issued writes, exact Written
progress, whether a prompt arrived, raw response evidence and Err. Complete is
not device acceptance or persistence. Raw is explicit sensitive access; implicit
formatting and JSON contain metadata only. Snapshot accessors detach mutable
patterns/slices; caller closures remain borrowed and must synchronize shared state.

Profile.ValidateSave(ctx, result) supplies affirmative save evidence. SaveConfig
and topology share it. Missing or negative evidence returns ErrUnconfirmed while
retaining completed steps. Built-in profiles delegate to platform-owned save
confirmation rules; unsupported firmware evidence is never guessed.
Confirmation is not a claim about physical flash durability after power loss.

## 2026-11-05 vendor dialect API

The built-in catalog is `vendors.NewBuiltinRegistry()`. Resolve a platform with
`Lookup`; the immutable `Platform` owns its CLI policy and returns a detached
`connector.Profile` through `Profile`. `vendors/dialect` contains only
the leaf `Platform`/`Registry` contracts and shared data types. Consumers own the
optional behavior interfaces: topology owns neighbor discovery, description
reading, interface identity and writing; transfer owns TFTP and remote-backup
policies; fleet owns platform resolution. The built-in registry owns its
inventory matcher and derives lookup and matching order from one platform list.
`connector.Profile.Capabilities` and `Device.Capabilities()` have been removed;
platform support is represented by the behavior method sets instead.
Platform-specific TFTP source-file, verification-root and destination-reuse
requirements travel with the matching policy. `fleet.NewRules`, `topology.New`,
pure topology planning, TFTP, and remote-backup operations take an explicit registry.
The package-level `vendors.Profile` and `vendors.Names` lookups have been
removed. The built-in catalog is static; it has no runtime plugin loader.
Vendor policies may return `dialect.DiagnosticError` to preserve a stable error
code and phase across the consumer boundary; raw device output remains absent
from formatted diagnostics.

ConnectionConfig adds IdentityFile, KeyPassphrase and LineEnding (default LF).
Key passphrases are sent only after an explicit matching prompt, are redacted,
and never enter argv. Command/credential line endings are configurable; declared
interaction Reply values remain verbatim. Copying compiled regexps retains
POSIX/Longest settings; CRLF scripts preserve command bytes and source line numbers.

Archive errors preserve cancellation, primary I/O failures and cleanup causes.
Pool.Close retries retained failed owners. A direct Client.Close should follow
completed/canceled transactions; active work stays owned by its context and Close
returns ErrArchive until that work has returned. Borrowed writers must cooperate
and must not reenter their owning stream/log sink.

## 2026-10-29 quality changes

- `vendors.Open`, `vendors.Profile` and `vendors.Names` are removed. Resolve a
  `Platform` through `Registry.Lookup`, then use its detached
  `Profile` with `connector.New`, register `Close`, and call `Connect`.
  A connection error never transfers cleanup responsibility away from the
  caller. Repeated `Close` retries failed cleanup; successful cleanup is not
  repeated. Commands are never replayed.
- `Profile.Validate`, `ConnectionConfig.Validate` and `ValidateOptions` perform
  pure checks. The last checks reusable options without requiring a bound Host.
  Constructors validate, normalize defaults and detach mutable configuration.
- `StepResult.Rendered() (string, error)` exposes strict terminal-visible UTF-8.
  Completed steps share immutable derived text. `Raw`, `Output`, failed Attempts,
  accepted Steps and metadata-only formatting keep their existing contracts.
- Planning, batch execution, reports and diagnostics moved from `netdisco` to
  `fleet`; `netdisco.FleetOptions` becomes `fleet.RunOptions`. Inventory sources
  and cache stay in `netdisco`. CLI/YAML/JSON vocabulary is unchanged.
- `parallel.ErrWorkerExit` reports callbacks terminating with `runtime.Goexit`.
  The batch stops admitting work and joins admitted workers. Fleet preserves
  completed outcomes before invoking observers and records abnormal observers.
  Panics still propagate; cancellation and callbacks remain cooperative.

## 2026-10-29 callback and response refinement

Three pre-v1 callback signatures change. Always pass the callback's supplied
context to nested work; it includes cancellation from `Device.Close`:

```go
device.WithOperation(ctx, func(ctx context.Context, op *connector.Operation) error {
    _, err := op.ExecuteCommand(ctx, command, connector.ExecuteOptions{})
    return err
})

profile.AfterLogin = func(ctx context.Context, step connector.StepResult) (connector.LoginState, error) {
    // Parse the explicit response accessors as needed.
    return connector.LoginState{HAState: "active"}, ctx.Err()
}

// ConfigStrategy implementations now use:
// Finish(context.Context, connector.Result) (string, error)
```

`AfterLogin` returns a value instead of a borrowed metadata map. Its errors stop
login. `Finish` receives the admitted operation context. Callbacks remain
cooperative: Close interrupts owned I/O without waiting for application callbacks.

`StepResult.Response()` and `Attempt.Response()` expose an immutable `Response`.
`Raw`, `RawPrompt`, and `Encoding` return explicit evidence. `Output`, `Rendered`,
and `Prompt` return `(string, error)`; the latter two provide terminal-visible
UTF-8. Missing conversion returns `ErrIncomplete`, and invalid conversion retains
its original cause. Accessors never retry detection or decoding. A device-rejected
completed response can therefore retain verified text even though it has no
accepted Step. `Attempt.Complete` still means a prompt arrived, not acceptance.

```go
view := attempt.Response()
text, err := view.Output()
if err != nil {
    text = view.Raw() // explicitly incomplete/invalid text evidence
}
```

Existing Step convenience accessors remain; `Step.Prompt` preserves its historical
UTF-8 wire form. Prefer `Response.Prompt` for verified visible text. Login callbacks
also receive verified response snapshots. Formatting and JSON remain metadata-only.
`Result.Steps` returns an independent outer slice, while immutable responses are
shared; `Command()` still detaches mutable fields. Simultaneous operation, receipt,
logging and cleanup failures remain discoverable with `errors.Is/As`, with the
primary failure controlling fleet diagnostic classification. No command is replayed.

`Registry.Lookup` returns immutable dialect declarations; each
`Profile` call returns independent mutable connector data. Device
snapshots and fixed matchers are private. `NewPlan` resolves capabilities once
per vendor per plan; custom lookup state is not cached across plans. Each
configuration collection still gets its own strategy instance.

Optional source probes are `scripts/verify-firemon-source.py ADAPTER_PATH` and
`scripts/verify-expect-source.py EXPECT_PATH`. They migrate or rebind imports only
inside temporary source copies, without `replace` or `go.work`. The real modules
remain pinned; passing these probes does not establish published adoption.

Example lifecycle (the deferred Close also runs on a failed Connect):

```go
registry := vendors.NewBuiltinRegistry()
declaration, err := registry.Lookup("cisco_ios")
if err != nil { return err }
profile := declaration.Profile()
device, err := connector.New(profile, cfg)
if err != nil { return err }
defer device.Close()
if err := device.Connect(ctx); err != nil { return err }
```

`archive.Client.Close` still returns `ErrArchive` while transactions or Prepare
are active; their caller contexts own cancellation. Pool performs cancellation
and joining. Prepare waiters can cancel without waiting for another network
preflight. Canceled/closed calls do not take the prepared fast path or begin
local setup. Channel endpoints must be non-nil, own a Close function and support
working deadlines; deadline failures preserve the original cause.

## 2026-10-30 transport authentication and cleanup

Custom transports may implement the optional authentication hook with this
signature:

```go
func (t *Transport) Authenticated(ctx context.Context) error {
    return t.persistTrust(ctx)
}
```

`Connect` invokes it after login using the admitted operation context. Propagate
that context to subprocesses, locks and persistence. A transport implementing
the old `Authenticated() error` hook is rejected before login I/O with
`invalid_transport` wrapping `ErrInvalid`; its acquired resources still receive
cleanup. Transports without either hook continue to work.

A `TransportFactory` must return a non-nil owner together with an initialization
error whenever resources still require cleanup. This includes cancellation just
after starting a process. `Device` retains that owner until `Close` succeeds;
failed cleanup remains retryable. Preserve primary and cleanup errors with
`errors.Join` instead of dropping ownership when returning an error.

The built-in process transport closes trust-commit admission, cancels an active
commit and interrupts I/O before joining its internal commit. Temporary trust
files are removed only after that commit exits. `Device.Close` still does not
join application callbacks. Cancellation is cooperative: a cancellation racing
with storage's final check and rename can accompany a committed file. Committed
storage is never rolled back, and a later `storage.WriteError` keeps its receipt.

Command and interaction failures preserve their complete original error tree
under safe connector metadata. Use `errors.As` for `expect.WriteError` and its
actual write progress, and `errors.Is` for secondary causes. An interaction's
reply progress remains distinct from the main command's `Attempt.Written`.

Inventory field mapping, fleet scheduling and TFTP success vocabulary have no
API changes. HTTP keys and PG column names remain case-sensitive; optional nulls
become empty strings and IP is required. PG byte budgets include ignored columns.
Cancellation or validation failure returns no partial inventory. PG output owns
its strings before the next driver row can reuse buffers.

## 2026-11-03：源码候选接口收敛

本节适用于隔离源码配对候选，尚未由正式固定的 go-expect v0.1.2 提供；不以源码验收冒充已发布依赖采纳。

- Profile、Interaction、Command 的提示符/交互字段使用不可变 `expect.Pattern`。使用 `expect.Compile`、`FromRegexp` 或字节级 `Line`；可选声明用零值，连接前经 `ValidateText` 检查。固定匹配器不再通过可变 regexp 指针映射转换。
- Device/Operation 使用 `RawPrompt()` 获取线缆字节，使用 `Prompt()` 检查解码结果。删除混合表示的 CurrentPrompt。ConfigStrategy 通过 MatchPrompt 返回 Pattern，精确终端提示符用 Line，不能将 GB18030 原文直接拼进 Go 正则。
- StepResult/Attempt 的响应统一经 Response() 读取。原 Raw、Output、Prompt、Rendered、OutputEncoding 转发删除。Response.Output/Rendered/Prompt 的错误必须处理；Result.Config 用于配置正文，JoinedOutput 只连接已接受的命令文本，不自动替换为配置或原文字节。
- BackupSource.Verified、BackupReceipt.HasSavedEvidence/Complete 集中判断双文件状态。原文单独提交不属于完整成功；迟到的文件或清理错误不抹除已经提交的证据。
- transfer.Target 统一计划与执行目标，替代 Effective 和 fleet.Target。完整目标在落盘前冻结，固定文件名和碰撞策略维持原规则。
- bootstrap 使用直接构造注入和每厂商的无秘密模板，移除空 Services.Close 生命周期；凭据仍在逐设备构造时解析。
- go-expect 的 WithTranscript 重复别名移除，使用 WithOutput；Send、SendAll、Poll 的不同语义保留。

本地消费者应直接迁移，不增加旧接口包装；发现无法保持原业务语义的历史借用会话接口时，必须保留失败边界并报告，不能删除测试或暗中改变恢复行为。

## 2026-11-04 可观察阶段与纯文本备份

`ConnectionConfig.OnSessionEvent` 接收不可变的 `SessionEvent{Host,Name,Status}`，同步在操作租约下调用，与日志级别独立；不包含命令或设备响应。观察回调应配合context，错误保留原原因及已有完成步骤。

`BackupOptions.TextOnly` 严格采集解码后的配置并省略raw旁车；`BackupReceipt.TextOnly` 记录该模式，完成状态仍依据采集和持久化证据。库默认保真行为保留，CLI默认启用TextOnly。未知编码不会被猜测或转义成成功配置。显式设置 `PreserveInvalidEncoding` 后，严格解码失败的配置保留原字节、标记 `original-bytes` 和 `TextWarning`，仍不创建旁车；CLI 采用此模式。普通 `RunningConfig`、命令和结构化解析保持严格。

## 2026-11-07：清单配置与应用层组织

新增两个公开的纯配置入口，供 VXLAN、Wireless 等应用复用清单字段规则：

```go
func InventoryEnvironment(values map[string]string, higher map[string]any) (map[string]any, error)
func ValidateInventory(document map[string]any) error
```

`InventoryEnvironment` 只投影 `netdisco` / `inventory` 的公开环境变量，忽略其他模块参数和凭据。`higher` 是调用方已经确定的高优先级文档：按字段存在性屏蔽环境值后才解码，不按真值或非空判断。false、null、空列表及已替换的父节都能屏蔽无效的低优先级环境值。返回值是环境层，不会合并或修改 `higher`；调用方按自己的层级顺序合并后，再调用 `ValidateInventory`。

`ValidateInventory` 复用 config 的字段与类型规则，拒绝这两个节之外的内容，不改变输入；它不会补默认值、发现文件、展开路径、连接清单源或读取凭据。来源连接和请求语义仍由相应 Netdisco API 校验。环境投影与文档校验沿用有限资源预算，被屏蔽或不相关的环境值不消耗投影预算。其他应用无需构造虚假的 Connector 配置快照来复用这些规则。

内部应用代码按责任组织：bootstrap 统一加载策略、校验拓扑配置并构造类型化工作流设置；workflow 只消费这些设置和注入的资源工厂，通过类型化事件反馈进度。CLI 负责终端文字和 Reporter，`internal/topologyreport` 负责 CLI 与持久化报告共用的类型化拓扑呈现。CLI 参数、配置键、JSON schema、中文输出和退出码保持既有契约；这些内部包不是下游可导入的公开 API。晚到的进度或输出失败仍降低报告成功状态，并保留已经确认的设备和存储证据。

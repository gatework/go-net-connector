# Go net-connector implementation

Approved plan: the conversation plan of 2026-10-25. This file is its implementation ledger specification; no reduction of parity scope is authorized.

## Global constraints

Single module github.com/gatework/go-net-connector, Go >=1.26, Linux/macOS amd64/arm64. Idiomatic Go API, preserve built-in Ruby behavior, configuration, CLI, report schema and backup formats. Use go-expect plus system SSH/Telnet. Minimal opt-in generic additions to go-expect must preserve defaults. Use samber/do v2 only at composition root. Reimplement the remote archive worker in Go, administratively installed, preflight before device I/O. Do not commit, publish or deploy. Preserve original source workspaces.

Reference includes dirty/untracked source, not just Git HEAD. Store manifests and frozen copies. Domain constructors, capability checks, selection and offline mode must not open devices or resolve credentials.

## Task 1: go-expect compatibility

Implement opt-in declaration priority; bounded streaming fallback consumes first32KiB at64KiB only when no ordinary match and retains32KiB, no timer reset; explicit Poll distinct from zero timeout, sole-reader coordinated OS readiness, unsupported transports fail; explicit SendAll with unsent-suffix progress, no replay, write failure closes, context owns deadline and queued input stays bounded without bidirectional deadlock; standalone streaming redactor with custom replacement and scoped rules, preserve existing defaults. Read go-expect contracts and Ruby expect implementation. Tests: real PTY readiness/pump scheduling, priority, stream boundaries, partial writes, cancellation, backpressure and race. Existing API tests stay green.

## Task 2: connector core

Device/Profile/Command/Script/Result/errors and hooks, operation lease (Busy concurrent/reentry), validate entire script before connect/send, controlled lazy connect. Shared absolute command deadline across write/read/pager/replies; login10s/write10s/command25s plus vendor overrides; response cap32MiB, optional cumulative script budget counts all raw bytes/internal queries. Raw vs rendered output separate. Complete receipt only after prompt and no rejection; record before hooks/log/callback/budget failure. OnReceipt once/main step, OnStep only postprocessed success. Preserve partial results and primary error. No operation replay. Hook executor shares lease and budget. Process groups TERM/KILL/Wait bounded cleanup, retain cleanup ownership. Operation-local secrets. Strict/accept_new/replace known_hosts with crossprocess locked atomic post-auth commit. Interactive handoff restores terminal. All user callbacks/sinks cooperative; no abandoned goroutines.

## Task 3: vendors, parsing and local storage

12 vendors: h3c,h3c_wireless,cisco_ios,cisco_nxos,huawei,hillstone,radware,palo_alto,topsec,fortinet,juniper,centec. Preserve exact commands/prompts/replies/diagnostics and capability matrix. PAN/Junos no implicit commit; Topsec no save/TFTP; Junos FTP/SCP. H3C rule-ID hook, Huawei privilege, Radware HA, IOS banner, PAN before/after candidate diff, FortiOS version/current authorized VDOM, Junos committed config.
Embed19 TextFSM templates and index, independent FSM state each parse,32MiB line cap, strict encoding and explicit missing/invalid/incomplete errors. Offline saved config parsing.
Stable sidecar flock before connection, canonical NFC/case-fold path key, NOFOLLOW0600 and FD identity/type/owner/link checks. SHA256 created/changed/unchanged and previous hash, skip only same entry/content0600, preserve mtime. temp/fsync/rename/dirfsync receipt distinguishes not committed/committed/durable. Preserve direct backup vs Fleet symlink policies.

## Task 4: topology

Discovery/descriptions/plan/audit/apply/sync. Writes only H3C/wireless,IOS/NXOS,Hillstone. Huawei/Fortinet/Junos/PAN read-only; Radware description only; Centec/Topsec unsupported. Pure plan full command preflight; explicit Apply confirmation; discover/revalidate/change/readback/save under one lease, stale neighbor/description refuses before write, failed readback no save, no-op no writes, no auto rollback.

## Task 5: transfers and archive

Device TFTP commands with exact completion and failure priority, receipt separates device_reported/server_verified, source/kind/format/requested/actual destination. Junos FTP/SCP exact destination/positive Wrote, no implicit trust. Per server-target lock, preserve previous file, freshness via serverclock and dev/ino/size/mtime/ctime stable read+SHA256. Go net-connector-archive binary default /usr/local/bin/net-connector-archive and tftp.ssh_worker_path. Admin installs. Probe version1/capabilities/dirs before devices. ready->archived->binary->delivered->complete, control16KiB/payload64MiB/chunk64KiB, one body/trailer deadline. SSH ordinary pipes, pinned dirfds/openat/NOFOLLOW0700dirs/0600files/owner/link/inode/flock/fsync. Disconnect preserves unconfirmed staging. Pool4channels/master bounded by batch. Diskfail drain/hash body preserve proof, no reupload, cleanup only after both archives confirmed.

## Task 6: inventory and Fleet

HTTP basic login->APIkey, pagination and Missing-query400 fallback q=%, bounded500/page,10000pages,16MiB/page,128MiBtotal,100kdevices,300s,total validation, duplicates/stall. pgx parameterized read-only cursor extended protocol query, owns connection; context through all phases. Fields ip,name,dns,vendor,os,model,os_ver,serial. Cache schema1 Ruby ordered identity doubleSHA, exclude secrets; fresh preferred/offline validated stale no network, refresh failure stale permitted, cachewrite failure keeps fresh plus diagnostic. Selection sole pure IPv4/v6 interval/CIDR/range filtering (no expansion); within-field union/across intersection/excludes win; casefold contains/prefix/glob. Snapshot cwd/home/settings; per-device dynamic credentials. Wholebatch preflight before credentials/device. Pool4 default500max, stable inventory order, isolate device failures; canceled stops dispatch, completed/failed/interrupted/not_started. Report schema2 strict/selected/verified, unknown/skips/no-device not success, errors/receipts retained separately.

## Task 7: configuration and CLI

CLI > original env > YAML > .env > defaults. Merge /etc/net-connector,/usr/local/etc/net-connector,XDG config/default ~/.config,programdir,cwd; common+namespace/tool YAML, explicit filelast; no parents; relative paths by source; no env mutation; arrays replace/null preserve. YAML rejects aliases/duplicates/multidoc/nonstring,depth128/nodes100000. Safe dotenv including quotes/#/$ interpolation. Passwords from env/explicitCLI/stdin resolver, never YAML. --help offline/no configIO; show-config public only; init creates missing0600 and exits; noninteractive no stdin; inventory-only no devices; exportlocal. stdin JSON then RUN execution/apply, explicit topology Apply. CLI binaries net-backup,net-backup-batch,net-backup-tftp,net-topology retain differing root vs examples defaults (strict/sample5 vs selected/full). Exit0/1/2/130, JSONstdout/progressstderr. Preserve reports plan.json/events.jsonl/text/json/sessionlogs field names/null/timeformats/error types/filenames, whitespace not byteidentical.

## Task 8: acceptance

Shared Ruby/Go fixtures compare outgoing exact bytes/order/count, output/config/hash/error/partialreceipt/report. Real PTY/process/localHTTP/TLS/tempPG/localsshd+worker integration, no production devices. Race/vet/leak/fuzz; Linux/macOS Go1.26+1.27; amd64/arm64 builds; independent consumer/outside checkout CLI embeds templates. Benchmarks64KiB/1/8/32MiB,1000pagedchunks,1000/10000 mockdevices record allocations/RSS/time, no networkspeed claims. Complete docs, compatibility matrix, evidence. No automatic publication.

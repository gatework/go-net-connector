# 验证记录

整体审查的六项问题均已修复并通过限定范围复审。当前记录来自本地测试，不代表生产设备或远端 CI；发布验证另行记录。

## 冻结来源与差分

`baseline/*.json` 记录 Ruby connector、expect-ruby、ctx_cli 和 Go expect 的 Git HEAD、工作区状态及逐文件 SHA-256。Ruby 基线为 0.18.0，包含未提交修改。隔离的 Ruby 4.0.6 / Minitest5 运行：1,225 tests、12,020 assertions、0 failures/errors/skips；未运行独立加载、发布和示例入口套件，见 `baseline/ruby-tests.log`。

`testdata/ruby-contracts.json` 由 `scripts/reference-fixtures.rb` 直接调用冻结 Ruby 实现生成。Go 测试比较 12 个厂商的命令/超时/提权、10 个 TFTP 策略的命令及回复、终端渲染字节与 SHA-256、包含成功/失败/跳过/部分保存的完整 schema2 报告。其他协议分支通过包级故障注入和对话夹具测试。19 个模板及索引与冻结 Ruby 字节相同。

## 本地验收

| 检查 | 证据 |
| --- | --- |
| Go 1.27.1 Darwin arm64 全模块 race、vet | `baseline/final-race.log`、`final-vet.log` |
| Go 1.26.0 Darwin arm64，包含 worker / PostgreSQL 集成 | `baseline/go126-tests.log` |
| Go 1.26.0 Linux arm64 race、vet、expect race | `baseline/linux-go126-tests.log` |
| Go 1.27.2 Linux arm64 race、vet、expect race | `baseline/linux-arm64-tests.log` |
| Darwin/Linux × amd64/arm64 的全部五个二进制交叉构建 | `baseline/cross-builds.log` |
| go-expect additive APIs、默认行为和 goroutine 回收 | `baseline/expect-race.log` |
| 独立源码重建、GOWORK=off 消费者、内嵌模板、目录外 CLI | `baseline/consumer-tests.log` |
| 候选源码额外输入、符号链接、内容漂移与独立重建 | 历史检查；本地快照重建流程已随 go-expect v0.4.0 正式依赖而退役 |
| YAML、上传目标随机输入测试 | `baseline/fuzz-config.log`、`fuzz-transfer.log` |
| 64 KiB/1/8/32 MiB、1,000 页、1,000/10,000 mock devices | `baseline/go-benchmarks.log`、`go-benchmarks-rss.log` |

connector、archive、parallel 和 go-expect 使用 goleak 检查测试退出后的 goroutine；设备模拟端及 worker 显式加入。PTY 测试验证终端、进程退出和已失效组回收。临时 PostgreSQL 使用本地 Unix socket，测试结束停止服务；HTTP/TLS 使用本地 fixture。默认测试会跳过需外部二进制的 PostgreSQL 和已安装 worker 用例，本次 Darwin 最终 race 验收显式设置了两项环境变量：

```sh
go build -o bin/ ./cmd/...
NC_TEST_ARCHIVE_WORKER="$PWD/bin/tftp-archive" \
NC_TEST_POSTGRES_BIN=/opt/homebrew/opt/postgresql@18/bin \
go test -race ./... -count=1 -timeout 120s
```

其他机器应将 PostgreSQL 路径改为其 `initdb`、`pg_ctl` 所在目录。Linux 验收运行容器内的测试夹具，不含 PostgreSQL 外部二进制和真实 SSH 服务。

归档验证包含真实安装后的 Go worker 标准管道，以及 localhost sshd、临时主机/客户端密钥和 known_hosts 的完整上传。Parallel=5 分配两条 SSH 主连接，上传后核对本地内容、服务器归档 URI、哈希及关闭后无主进程。密码 askpass 路径有进程参数/辅助文件/取消夹具，未做真实密码登录。

## 性能证据

初次规模基准发现 Renderer 顺序文本每字节调用 `bytes.Repeat`。内存 profile 将约 96.55% 的抽样分配对象归因到该路径。修改为顺序追加，仅在光标跳跃留空时补空格。

同一 Go 1.27.1 工具链、每组 6 次观测：64 KiB 的分配次数约减少 98.34%，1 MiB 约减少 99.88%；分配字节分别减少 27.48% 和 23.27%，benchstat p=0.002。耗时比较没有统计显著性，不宣称吞吐提升。原始记录及比较位于 `baseline/render-before.log`、`render-after.log`、`render-comparison.log`。

基准是内存对话/本地计算测试，不是设备或网络速度。首次总进程最大 RSS 包含构建和多个基准；不能当作每设备内存。32 MiB 基准显式提高响应上限以容纳正文、换行和提示符，默认 32 MiB 上限仍单独测试。

## 交付边界

用户于 2026-10-26 授权将本模块提交、推送到 GitHub 私有仓库并发布首版；部署仍不在范围内。原 go-expect 工作区在冻结后出现其他修改，重建采用本项目内置的已校验快照和补丁，未覆盖这些修改。正式发布前需要把兼容 API 纳入可发布的 expect 版本，并移除本地依赖占位。

本地审查报告与修复差分保留在 `.superpowers/sdd/implementation-plan/`，用于保存未纳入源码提交的完整复审过程。

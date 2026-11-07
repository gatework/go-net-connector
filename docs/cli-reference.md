# netman 命令参考

由命令树生成。维护时运行 `go test ./internal/cli -run TestChineseCommandReference -args -update-cli-reference`；不手工修改以下命令说明。

## netman

```text
统一管理配置采集、原生上传、设备清单和拓扑；帮助、版本和补全不读取配置或凭据。

用法：
  netman [子命令] [选项]

子命令：
  backup             配置备份、目标规划与批次导出
  completion         生成无需配置或凭据的命令补全脚本
  config             离线初始化、查看和检查本地配置
  help               显示指定命令的中文帮助
  inventory          查看设备清单，不连接设备
  topology           发现、规划和重新核验后应用拓扑变更
  version            显示版本与源码修订号

常用选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境
  -h, --help                 显示当前命令的中文帮助
  -v, --version              显示版本和源码修订号

使用 netman help <命令路径> 查看详细帮助。
```

## netman backup

```text
配置备份、目标规划与批次导出

用法：
  netman backup [子命令] [选项]

子命令：
  export             从明确指定的批次导出配置，不访问网络
  plan               预览备份目标，不连接设备或归档服务
  run                采集配置并保存为设备名称-IP.txt
  tftp               原生 TFTP 上传、文件核验和归档

常用选项：
  -h, --help                 显示当前命令的中文帮助

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

使用 netman help <命令路径> 查看详细帮助。
```

## netman backup export

```text
从明确指定的批次导出配置，不访问网络

用法：
  netman backup export <IP> [选项]

常用选项：
  --batch <目录>               明确的批次目录，不自动查找最新批次
  -h, --help                 显示当前命令的中文帮助
  --json                     以 JSON 输出导出文件的元数据；需指定 --output
  --output <文件>              导出目标文件；未指定时输出原文件正文

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

示例：
  netman backup export 192.0.2.1 --batch ./backups/2026-11-03_10-00-00 --output ./saved.txt

使用 netman help <命令路径> 查看详细帮助。
```

## netman backup plan

```text
预览备份目标，不连接设备或归档服务

用法：
  netman backup plan [选项]

常用选项：
  --directory <目录>           备份输出目录
  -h, --help                 显示当前命令的中文帮助
  --json                     以 JSON 输出结果
  --method <方式>              备份方式：cli 或 tftp（默认：cli）
  --non-interactive          禁止读取交互输入
  -p, --parallel <数量>        设备并发数量，范围 1 至 500；未指定时使用配置
  --quiet                    隐藏进度提示
  --tftp-root <目录>           服务器文件核验和归档的本地或 SSH 根目录
  --tftp-server <地址>         设备上传目标服务器地址

设备筛选：
  --all                      选择全部就绪设备并取消配置中的抽样限制
  --exclude-hosts <范围>       排除的设备地址或范围；可重复
  --exclude-model <条件>       排除设备型号条件；可重复
  --exclude-name <条件>        排除设备名称条件；可重复
  --exclude-vendor <名称>      排除厂商名称；可重复
  --host <地址>                精确设备 IP 地址；可重复
  --hosts <范围>               设备 IP、CIDR 或地址范围；可重复
  --model <条件>               设备型号筛选条件；可重复
  --model-match <方式>         设备型号匹配方式：contains、prefix 或 glob
  --name <条件>                设备名称筛选条件；可重复
  --name-match <方式>          设备名称匹配方式：contains、prefix 或 glob
  --sample <数量>              每厂商抽样数量，范围 1 至 5；未指定时使用配置
  --vendor <名称>              厂商筛选名称；可重复

连接与认证：
  --ask-netdisco-password    在终端隐藏输入清单服务密码
  --ask-password             在终端隐藏输入设备密码
  --enable-password <密码>     设备权限提升密码
  --host-key-policy <策略>     主机密钥策略：strict、accept_new 或 replace
  --known-hosts <文件>         SSH 主机密钥信任文件
  --netdisco-password <密码>   清单服务登录密码
  --netdisco-username <用户名>  清单服务登录用户名
  --password <密码>            显式设备登录密码，优先于环境变量
  --protocol <协议>            设备连接协议：ssh 或 telnet
  --stdin-credentials        先读取一行凭据 JSON；备份执行或拓扑应用前再读取 RUN
  --username <用户名>           显式设备登录用户名

高级选项：
  --cache-mode <模式>          清单缓存模式：prefer、only 或 refresh
  --cache-only               仅使用已校验的清单缓存
  --layout <布局>              输出目录布局：batch（批次）或 flat（平铺）（默认：batch）
  --max-script-output-bytes <上限> 脚本响应累计字节上限
  --netdisco-url <地址>        清单 HTTP 服务地址
  --query <语句>               PostgreSQL 清单查询语句
  --query-params <数组>        查询参数 JSON 数组
  --refresh-inventory        刷新设备清单
  --source <类型>              清单来源类型：http 或 postgres
  --success-policy <策略>      成功策略：strict、selected 或 verified；默认 CLI 为 selected，TFTP 为 verified

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

示例：
  netman backup plan --method cli --json
  netman backup plan --method tftp --tftp-server 192.0.2.9

使用 netman help <命令路径> 查看详细帮助。
```

## netman backup run

```text
采集配置并保存为设备名称-IP.txt

用法：
  netman backup run [选项]

常用选项：
  --directory <目录>           备份输出目录
  -h, --help                 显示当前命令的中文帮助
  --json                     以 JSON 输出结果
  --non-interactive          禁止读取交互输入
  -p, --parallel <数量>        设备并发数量，范围 1 至 500；未指定时使用配置
  --quiet                    隐藏进度提示

设备筛选：
  --all                      选择全部就绪设备并取消配置中的抽样限制
  --exclude-hosts <范围>       排除的设备地址或范围；可重复
  --exclude-model <条件>       排除设备型号条件；可重复
  --exclude-name <条件>        排除设备名称条件；可重复
  --exclude-vendor <名称>      排除厂商名称；可重复
  --host <地址>                精确设备 IP 地址；可重复
  --hosts <范围>               设备 IP、CIDR 或地址范围；可重复
  --model <条件>               设备型号筛选条件；可重复
  --model-match <方式>         设备型号匹配方式：contains、prefix 或 glob
  --name <条件>                设备名称筛选条件；可重复
  --name-match <方式>          设备名称匹配方式：contains、prefix 或 glob
  --sample <数量>              每厂商抽样数量，范围 1 至 5；未指定时使用配置
  --vendor <名称>              厂商筛选名称；可重复

连接与认证：
  --ask-netdisco-password    在终端隐藏输入清单服务密码
  --ask-password             在终端隐藏输入设备密码
  --enable-password <密码>     设备权限提升密码
  --host-key-policy <策略>     主机密钥策略：strict、accept_new 或 replace
  --known-hosts <文件>         SSH 主机密钥信任文件
  --netdisco-password <密码>   清单服务登录密码
  --netdisco-username <用户名>  清单服务登录用户名
  --password <密码>            显式设备登录密码，优先于环境变量
  --protocol <协议>            设备连接协议：ssh 或 telnet
  --stdin-credentials        先读取一行凭据 JSON；备份执行或拓扑应用前再读取 RUN
  --username <用户名>           显式设备登录用户名

高级选项：
  --cache-mode <模式>          清单缓存模式：prefer、only 或 refresh
  --cache-only               仅使用已校验的清单缓存
  --layout <布局>              输出目录布局：batch（批次）或 flat（平铺）（默认：batch）
  --max-script-output-bytes <上限> 脚本响应累计字节上限
  --netdisco-url <地址>        清单 HTTP 服务地址
  --query <语句>               PostgreSQL 清单查询语句
  --query-params <数组>        查询参数 JSON 数组
  --refresh-inventory        刷新设备清单
  --source <类型>              清单来源类型：http 或 postgres
  --success-policy <策略>      成功策略：strict、selected 或 verified；默认 CLI 为 selected，TFTP 为 verified
  --verbose                  在原地进度面板中显示连接和采集详情，不显示设备原始回显

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

示例：
  netman backup run --all --json
  netman backup run --sample 2 --success-policy strict

使用 netman help <命令路径> 查看详细帮助。
```

## netman backup tftp

```text
原生 TFTP 上传、文件核验和归档

用法：
  netman backup tftp [选项]

常用选项：
  --directory <目录>           备份输出目录
  -h, --help                 显示当前命令的中文帮助
  --json                     以 JSON 输出结果
  --non-interactive          禁止读取交互输入
  -p, --parallel <数量>        设备并发数量，范围 1 至 500；未指定时使用配置
  --quiet                    隐藏进度提示
  --tftp-root <目录>           服务器文件核验和归档的本地或 SSH 根目录
  --tftp-server <地址>         设备上传目标服务器地址

设备筛选：
  --all                      选择全部就绪设备并取消配置中的抽样限制
  --exclude-hosts <范围>       排除的设备地址或范围；可重复
  --exclude-model <条件>       排除设备型号条件；可重复
  --exclude-name <条件>        排除设备名称条件；可重复
  --exclude-vendor <名称>      排除厂商名称；可重复
  --host <地址>                精确设备 IP 地址；可重复
  --hosts <范围>               设备 IP、CIDR 或地址范围；可重复
  --model <条件>               设备型号筛选条件；可重复
  --model-match <方式>         设备型号匹配方式：contains、prefix 或 glob
  --name <条件>                设备名称筛选条件；可重复
  --name-match <方式>          设备名称匹配方式：contains、prefix 或 glob
  --sample <数量>              每厂商抽样数量，范围 1 至 5；未指定时使用配置
  --vendor <名称>              厂商筛选名称；可重复

连接与认证：
  --ask-netdisco-password    在终端隐藏输入清单服务密码
  --ask-password             在终端隐藏输入设备密码
  --enable-password <密码>     设备权限提升密码
  --host-key-policy <策略>     主机密钥策略：strict、accept_new 或 replace
  --known-hosts <文件>         SSH 主机密钥信任文件
  --netdisco-password <密码>   清单服务登录密码
  --netdisco-username <用户名>  清单服务登录用户名
  --password <密码>            显式设备登录密码，优先于环境变量
  --protocol <协议>            设备连接协议：ssh 或 telnet
  --stdin-credentials        先读取一行凭据 JSON；备份执行或拓扑应用前再读取 RUN
  --username <用户名>           显式设备登录用户名

高级选项：
  --cache-mode <模式>          清单缓存模式：prefer、only 或 refresh
  --cache-only               仅使用已校验的清单缓存
  --layout <布局>              输出目录布局：batch（批次）或 flat（平铺）（默认：batch）
  --max-script-output-bytes <上限> 脚本响应累计字节上限
  --netdisco-url <地址>        清单 HTTP 服务地址
  --query <语句>               PostgreSQL 清单查询语句
  --query-params <数组>        查询参数 JSON 数组
  --refresh-inventory        刷新设备清单
  --source <类型>              清单来源类型：http 或 postgres
  --success-policy <策略>      成功策略：strict、selected 或 verified；默认 CLI 为 selected，TFTP 为 verified
  --verbose                  在原地进度面板中显示连接和采集详情，不显示设备原始回显

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

示例：
  netman backup tftp --tftp-server 192.0.2.9 --tftp-root /srv/tftp --success-policy verified --json

使用 netman help <命令路径> 查看详细帮助。
```

## netman completion

```text
生成无需配置或凭据的命令补全脚本

用法：
  netman completion [子命令] [选项]

子命令：
  bash               生成 bash 补全脚本
  fish               生成 fish 补全脚本
  zsh                生成 zsh 补全脚本

常用选项：
  -h, --help                 显示当前命令的中文帮助

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

使用 netman help <命令路径> 查看详细帮助。
```

## netman completion bash

```text
生成 bash 补全脚本；补全只使用静态命令、枚举和文件路径。

用法：
  netman completion bash [选项]

常用选项：
  -h, --help                 显示当前命令的中文帮助

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

示例：
  netman completion bash

使用 netman help <命令路径> 查看详细帮助。
```

## netman completion fish

```text
生成 fish 补全脚本；补全只使用静态命令、枚举和文件路径。

用法：
  netman completion fish [选项]

常用选项：
  -h, --help                 显示当前命令的中文帮助

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

示例：
  netman completion fish

使用 netman help <命令路径> 查看详细帮助。
```

## netman completion zsh

```text
生成 zsh 补全脚本；补全只使用静态命令、枚举和文件路径。

用法：
  netman completion zsh [选项]

常用选项：
  -h, --help                 显示当前命令的中文帮助

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

示例：
  netman completion zsh

使用 netman help <命令路径> 查看详细帮助。
```

## netman config

```text
离线初始化、查看和检查本地配置

用法：
  netman config [子命令] [选项]

子命令：
  check              仅检查本地配置，不验证远程访问或设备凭据
  init               创建缺失的私有配置文件后退出，不访问清单或设备
  show               显示有效的非敏感配置及其来源

常用选项：
  -h, --help                 显示当前命令的中文帮助

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

使用 netman help <命令路径> 查看详细帮助。
```

## netman config check

```text
仅检查本地配置，不验证远程访问或设备凭据

用法：
  netman config check [选项]

常用选项：
  --for <用途>                 配置用途：backup、tftp 或 topology（默认：backup）
  -h, --help                 显示当前命令的中文帮助
  --json                     以 JSON 输出结果

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

示例：
  netman config check --for backup

使用 netman help <命令路径> 查看详细帮助。
```

## netman config init

```text
创建缺失的私有配置文件后退出，不访问清单或设备

用法：
  netman config init [选项]

常用选项：
  --for <用途>                 配置用途：backup、tftp 或 topology（默认：backup）
  -h, --help                 显示当前命令的中文帮助
  --interactive              在终端使用中文配置向导
  --json                     以 JSON 输出结果
  --non-interactive          禁止读取交互输入

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

示例：
  netman config init --for backup

使用 netman help <命令路径> 查看详细帮助。
```

## netman config show

```text
显示有效的非敏感配置及其来源

用法：
  netman config show [选项]

常用选项：
  --for <用途>                 配置用途：backup、tftp 或 topology（默认：backup）
  -h, --help                 显示当前命令的中文帮助
  --json                     以 JSON 输出结果

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

示例：
  netman config show --for backup

使用 netman help <命令路径> 查看详细帮助。
```

## netman help

```text
按命令层级查询帮助，不读取配置文件或凭据。

用法：
  netman help [命令路径]

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

示例：
  netman help backup run

使用 netman help <命令路径> 查看详细帮助。
```

## netman inventory

```text
查看设备清单，不连接设备

用法：
  netman inventory [子命令] [选项]

子命令：
  list               预览筛选后的设备清单

常用选项：
  -h, --help                 显示当前命令的中文帮助

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

使用 netman help <命令路径> 查看详细帮助。
```

## netman inventory list

```text
预览筛选后的设备清单

用法：
  netman inventory list [选项]

常用选项：
  --for <用途>                 清单用途：backup 或 topology（默认：backup）
  -h, --help                 显示当前命令的中文帮助
  --json                     以 JSON 输出结果
  --non-interactive          禁止读取交互输入
  -p, --parallel <数量>        设备并发数量，范围 1 至 500；未指定时使用配置
  --quiet                    隐藏进度提示

设备筛选：
  --exclude-hosts <范围>       排除的设备地址或范围；可重复
  --exclude-model <条件>       排除设备型号条件；可重复
  --exclude-name <条件>        排除设备名称条件；可重复
  --exclude-vendor <名称>      排除厂商名称；可重复
  --host <地址>                精确设备 IP 地址；可重复
  --hosts <范围>               设备 IP、CIDR 或地址范围；可重复
  --model <条件>               设备型号筛选条件；可重复
  --model-match <方式>         设备型号匹配方式：contains、prefix 或 glob
  --name <条件>                设备名称筛选条件；可重复
  --name-match <方式>          设备名称匹配方式：contains、prefix 或 glob
  --vendor <名称>              厂商筛选名称；可重复

连接与认证：
  --ask-netdisco-password    在终端隐藏输入清单服务密码
  --ask-password             在终端隐藏输入设备密码
  --enable-password <密码>     设备权限提升密码
  --host-key-policy <策略>     主机密钥策略：strict、accept_new 或 replace
  --known-hosts <文件>         SSH 主机密钥信任文件
  --netdisco-password <密码>   清单服务登录密码
  --netdisco-username <用户名>  清单服务登录用户名
  --password <密码>            显式设备登录密码，优先于环境变量
  --protocol <协议>            设备连接协议：ssh 或 telnet
  --stdin-credentials        先读取一行凭据 JSON；备份执行或拓扑应用前再读取 RUN
  --username <用户名>           显式设备登录用户名

高级选项：
  --cache-mode <模式>          清单缓存模式：prefer、only 或 refresh
  --cache-only               仅使用已校验的清单缓存
  --max-script-output-bytes <上限> 脚本响应累计字节上限
  --netdisco-url <地址>        清单 HTTP 服务地址
  --query <语句>               PostgreSQL 清单查询语句
  --query-params <数组>        查询参数 JSON 数组
  --refresh-inventory        刷新设备清单
  --source <类型>              清单来源类型：http 或 postgres

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

示例：
  netman inventory list --for topology --hosts 192.0.2.0/24 --json

使用 netman help <命令路径> 查看详细帮助。
```

## netman topology

```text
发现、规划和重新核验后应用拓扑变更

用法：
  netman topology [子命令] [选项]

子命令：
  apply              生成计划、重新核验并显式应用变更
  discover           只读发现并审计拓扑
  plan               读取设备邻居和描述，生成变更计划，不下发修改

常用选项：
  -h, --help                 显示当前命令的中文帮助

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

使用 netman help <命令路径> 查看详细帮助。
```

## netman topology apply

```text
生成计划、重新核验并显式应用变更

用法：
  netman topology apply [选项]

常用选项：
  --directory <目录>           拓扑报告输出目录
  -h, --help                 显示当前命令的中文帮助
  --json                     以 JSON 输出结果
  --non-interactive          禁止读取交互输入
  -p, --parallel <数量>        设备并发数量，范围 1 至 500；未指定时使用配置
  --quiet                    隐藏进度提示

设备筛选：
  --exclude-hosts <范围>       排除的设备地址或范围；可重复
  --exclude-model <条件>       排除设备型号条件；可重复
  --exclude-name <条件>        排除设备名称条件；可重复
  --exclude-vendor <名称>      排除厂商名称；可重复
  --host <地址>                精确设备 IP 地址；可重复
  --hosts <范围>               设备 IP、CIDR 或地址范围；可重复
  --limit <上限>               拓扑设备数量上限，未指定时使用配置
  --model <条件>               设备型号筛选条件；可重复
  --model-match <方式>         设备型号匹配方式：contains、prefix 或 glob
  --name <条件>                设备名称筛选条件；可重复
  --name-match <方式>          设备名称匹配方式：contains、prefix 或 glob
  --vendor <名称>              厂商筛选名称；可重复

连接与认证：
  --ask-netdisco-password    在终端隐藏输入清单服务密码
  --ask-password             在终端隐藏输入设备密码
  --enable-password <密码>     设备权限提升密码
  --host-key-policy <策略>     主机密钥策略：strict、accept_new 或 replace
  --known-hosts <文件>         SSH 主机密钥信任文件
  --netdisco-password <密码>   清单服务登录密码
  --netdisco-username <用户名>  清单服务登录用户名
  --password <密码>            显式设备登录密码，优先于环境变量
  --protocol <协议>            设备连接协议：ssh 或 telnet
  --stdin-credentials        先读取一行凭据 JSON；备份执行或拓扑应用前再读取 RUN
  --username <用户名>           显式设备登录用户名

高级选项：
  --cache-mode <模式>          清单缓存模式：prefer、only 或 refresh
  --cache-only               仅使用已校验的清单缓存
  --description-format <格式>  接口描述格式，支持 %{neighbor} 与 %{port}
  --full-port-names          保留完整端口名称
  --lowercase-ports          端口名称转为小写
  --max-script-output-bytes <上限> 脚本响应累计字节上限
  --netdisco-url <地址>        清单 HTTP 服务地址
  --query <语句>               PostgreSQL 清单查询语句
  --query-params <数组>        查询参数 JSON 数组
  --refresh-inventory        刷新设备清单
  --source <类型>              清单来源类型：http 或 postgres
  --verbose                  在原地进度面板中显示连接和采集详情，不显示设备原始回显

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

示例：
  netman topology apply --hosts 192.0.2.1 --json

使用 netman help <命令路径> 查看详细帮助。
```

## netman topology discover

```text
只读发现并审计拓扑

用法：
  netman topology discover [选项]

常用选项：
  --directory <目录>           拓扑报告输出目录
  -h, --help                 显示当前命令的中文帮助
  --json                     以 JSON 输出结果
  --non-interactive          禁止读取交互输入
  -p, --parallel <数量>        设备并发数量，范围 1 至 500；未指定时使用配置
  --quiet                    隐藏进度提示

设备筛选：
  --exclude-hosts <范围>       排除的设备地址或范围；可重复
  --exclude-model <条件>       排除设备型号条件；可重复
  --exclude-name <条件>        排除设备名称条件；可重复
  --exclude-vendor <名称>      排除厂商名称；可重复
  --host <地址>                精确设备 IP 地址；可重复
  --hosts <范围>               设备 IP、CIDR 或地址范围；可重复
  --limit <上限>               拓扑设备数量上限，未指定时使用配置
  --model <条件>               设备型号筛选条件；可重复
  --model-match <方式>         设备型号匹配方式：contains、prefix 或 glob
  --name <条件>                设备名称筛选条件；可重复
  --name-match <方式>          设备名称匹配方式：contains、prefix 或 glob
  --vendor <名称>              厂商筛选名称；可重复

连接与认证：
  --ask-netdisco-password    在终端隐藏输入清单服务密码
  --ask-password             在终端隐藏输入设备密码
  --enable-password <密码>     设备权限提升密码
  --host-key-policy <策略>     主机密钥策略：strict、accept_new 或 replace
  --known-hosts <文件>         SSH 主机密钥信任文件
  --netdisco-password <密码>   清单服务登录密码
  --netdisco-username <用户名>  清单服务登录用户名
  --password <密码>            显式设备登录密码，优先于环境变量
  --protocol <协议>            设备连接协议：ssh 或 telnet
  --stdin-credentials        先读取一行凭据 JSON；备份执行或拓扑应用前再读取 RUN
  --username <用户名>           显式设备登录用户名

高级选项：
  --cache-mode <模式>          清单缓存模式：prefer、only 或 refresh
  --cache-only               仅使用已校验的清单缓存
  --description-format <格式>  接口描述格式，支持 %{neighbor} 与 %{port}
  --full-port-names          保留完整端口名称
  --lowercase-ports          端口名称转为小写
  --max-script-output-bytes <上限> 脚本响应累计字节上限
  --netdisco-url <地址>        清单 HTTP 服务地址
  --query <语句>               PostgreSQL 清单查询语句
  --query-params <数组>        查询参数 JSON 数组
  --refresh-inventory        刷新设备清单
  --source <类型>              清单来源类型：http 或 postgres
  --verbose                  在原地进度面板中显示连接和采集详情，不显示设备原始回显

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

示例：
  netman topology discover --hosts 192.0.2.1 --json

使用 netman help <命令路径> 查看详细帮助。
```

## netman topology plan

```text
读取设备邻居和描述，生成变更计划，不下发修改

用法：
  netman topology plan [选项]

常用选项：
  --directory <目录>           拓扑报告输出目录
  -h, --help                 显示当前命令的中文帮助
  --json                     以 JSON 输出结果
  --non-interactive          禁止读取交互输入
  -p, --parallel <数量>        设备并发数量，范围 1 至 500；未指定时使用配置
  --quiet                    隐藏进度提示

设备筛选：
  --exclude-hosts <范围>       排除的设备地址或范围；可重复
  --exclude-model <条件>       排除设备型号条件；可重复
  --exclude-name <条件>        排除设备名称条件；可重复
  --exclude-vendor <名称>      排除厂商名称；可重复
  --host <地址>                精确设备 IP 地址；可重复
  --hosts <范围>               设备 IP、CIDR 或地址范围；可重复
  --limit <上限>               拓扑设备数量上限，未指定时使用配置
  --model <条件>               设备型号筛选条件；可重复
  --model-match <方式>         设备型号匹配方式：contains、prefix 或 glob
  --name <条件>                设备名称筛选条件；可重复
  --name-match <方式>          设备名称匹配方式：contains、prefix 或 glob
  --vendor <名称>              厂商筛选名称；可重复

连接与认证：
  --ask-netdisco-password    在终端隐藏输入清单服务密码
  --ask-password             在终端隐藏输入设备密码
  --enable-password <密码>     设备权限提升密码
  --host-key-policy <策略>     主机密钥策略：strict、accept_new 或 replace
  --known-hosts <文件>         SSH 主机密钥信任文件
  --netdisco-password <密码>   清单服务登录密码
  --netdisco-username <用户名>  清单服务登录用户名
  --password <密码>            显式设备登录密码，优先于环境变量
  --protocol <协议>            设备连接协议：ssh 或 telnet
  --stdin-credentials        先读取一行凭据 JSON；备份执行或拓扑应用前再读取 RUN
  --username <用户名>           显式设备登录用户名

高级选项：
  --cache-mode <模式>          清单缓存模式：prefer、only 或 refresh
  --cache-only               仅使用已校验的清单缓存
  --description-format <格式>  接口描述格式，支持 %{neighbor} 与 %{port}
  --full-port-names          保留完整端口名称
  --lowercase-ports          端口名称转为小写
  --max-script-output-bytes <上限> 脚本响应累计字节上限
  --netdisco-url <地址>        清单 HTTP 服务地址
  --query <语句>               PostgreSQL 清单查询语句
  --query-params <数组>        查询参数 JSON 数组
  --refresh-inventory        刷新设备清单
  --source <类型>              清单来源类型：http 或 postgres
  --verbose                  在原地进度面板中显示连接和采集详情，不显示设备原始回显

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

示例：
  netman topology plan --hosts 192.0.2.1 --json

使用 netman help <命令路径> 查看详细帮助。
```

## netman version

```text
显示构建身份，不读取配置或凭据。

用法：
  netman version [选项]

常用选项：
  -h, --help                 显示当前命令的中文帮助

全局选项：
  --config <文件>              最后加载的配置文件；相对路径从启动目录解析
  --env-file <文件>            最后加载的环境文件；不修改进程环境

示例：
  netman version

使用 netman help <命令路径> 查看详细帮助。
```

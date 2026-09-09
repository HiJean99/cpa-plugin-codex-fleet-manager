# Codex Fleet Manager

简体中文 | [English](README.md)

`codex-fleet-manager` 是 CLIProxyAPI（CPA）的动态库插件，为 Codex 账号提供
额度感知调度、账号健康监控、重置窗口激活和账号注释管理。

Codex Fleet Manager 基于 Jeffery Zhang 的 Codex Quota Scheduler 改进，
由独立维护者发布，采用 MIT License，并保留原项目的版权和许可证声明。它不是
原项目的官方继任版本，也未获原作者背书。

### HiJean99 fork 状态

本 fork 基于上游提交 `ca828d1`，保留原有 scheduler、credential fencing、WAL
和崩溃恢复机制，只对 Codex 新周期激活增加可配置能力，并把上游行为保留为兼容
默认值：

- 激活模型改为可配置；fork 默认使用 `gpt-5.6-terra`，不再写死陈旧的
  `gpt-5.4-mini`；
- 新增固定时间表 5h 模式，可按 `Asia/Shanghai` 的 07:00、12:00、17:00
  预检查并按需激活，不会在夜间继续滚动五小时窗口；
- 5h 探测观察间隔、resetAt 滑动阈值、同账号最小激活间隔、失败冷却均可配置；
- 可选 `reset_probe_require_reset_at_slide` 模式：首次观察只记录 resetAt，必须等
  后续 resetAt 明显向后滑动才允许激活，采用与 9Router-style Codex auto-ping
  相同的保守判据；
- 可单独关闭插件调度接管，让 CPA 原生 `fill-first` 继续负责选账号，插件只负责
  监测和激活延迟启动的五小时窗口。

新周期激活仍然**默认关闭**。开启后会发送真实 Codex 请求、消耗少量额度，并在
成功时启动下一轮五小时窗口。

## v0.1.0 主要更新

- 使用独立插件身份：专属 CPA API 路径、浏览器存储、状态目录、动态库名和
  发布压缩包。
- 根据真实账号可用性和额度压力执行优化版 Fill First 调度，而不是依赖静态
  账号顺序。
- 支持可选的重置窗口激活和截止时间驱动的额度刷新。
- 提供中英文 Management UI、账号注释、优先级和 JSON 备份。

## 内置调度能力

- 真实可用性优先于插件优先级：不可用的高优先级账号不会再排到可用账号前面。
- 不可用账号按预计恢复时间从早到晚显示，无法确定恢复时间的账号放在最后。
- 五小时额度窗口改为可选。OpenAI 未返回该窗口时，只要周额度或月额度的
  长周期额度有效，账号仍可参与调度。
- 长周期额度缺失或无效时，账号保持未知/不可用，由 CPA fallback 接管，避免
  插件在证据不足时选择账号。
- Codex 账号列表从 CPA 的权威认证列表同步，并且只接管当前已确认的最高 CPA
  账号优先级层。
- 新周期激活使用可持久化的 single-flight 操作序列，只发送一次极小 Codex
  请求并在之后验证结果，且仍然需要用户主动开启。
- 管理界面的账号队列与生产调度使用相同的可用性分类和排序规则。

## 调度逻辑

调度器依次执行四层判断。每一层都会筛选或排序账号，再把结果交给下一层。

### 1. 只接管当前 CPA 最高优先级层

- 只考虑 provider 为 `codex` 的候选账号，忽略其他 provider。
- 没有显式 CPA 账号优先级的 Codex 账号按优先级 `0` 处理。
- 插件接管当前已确认的最高 CPA 账号优先级层中的全部 Codex 账号。较低 CPA
  优先级层仍由 CPA 自己的 fallback 逻辑处理，不会进入插件队列。
- 如果希望所有 Codex 账号一起参与调度，应为它们设置相同的 CPA 账号优先级。
  最简单的推荐配置是全部使用优先级 `0`。

CPA 账号优先级与插件自己的账号优先级是两个独立设置。插件不会从 CPA 读取
插件优先级，也不会把插件优先级写回 CPA。

### 2. 先判断账号是否真的可用

进入调度范围的账号会被分成三个实际类别：

1. **可直接使用：**额度信息新鲜或仍在允许范围内，而且账号当前可用。
2. **可安全试用：**额度证据未知或已经过期，但能够在使用前进行安全验证。
3. **不可用：**长周期额度已耗尽、认证被阻止、熔断器已打开、临时耗尽反馈仍
   有效，或当前无法安全验证账号。

可直接使用的账号先于可安全试用的账号。插件不会选择不可用账号。

账号必须拥有有效的长周期额度（周额度或月额度）。五小时额度窗口是可选的：
如果 OpenAI 暂时不返回五小时额度，只要长周期额度有效，账号仍然可以使用。
如果长周期额度缺失或无效，账号保持未知/不可用，由 CPA fallback 接管。

如果账号同时存在长周期额度耗尽和较早记录的临时耗尽反馈，周额度或月额度
耗尽是权威原因，管理界面也会显示这个真实原因。

### 3. 对可用账号排序

只有完成可用性分类之后，插件优先级才参与排序，而且只在同一个可选择类别
内部生效。插件优先级较高的账号会先被考虑，但插件优先级不能把不可用账号
移动到可用账号前面。

插件优先级相同时：

1. 如果 `monthly_mode` 为 `priority`，月度账号排在周度账号前面。
2. 额度压力更高的账号排在前面。额度压力为“长周期剩余额度百分比 ÷ 距离
   长周期重置的小时数”，并使用 30 分钟作为最小除数。
3. 额度压力相同或无法计算时，依次按更早到期、更多剩余额度打破平局。
4. 最后使用稳定的账号 ID 顺序打破平局。

默认的 `monthly_mode: expiry_order` 会让周度和月度账号共同按“剩余额度和
重置时间组合出的额度压力”排序。可选的 `priority` 模式会先明确优先使用
月度账号，再比较额度压力。

### 4. 对不可用账号排序

账号不可用后会忽略插件优先级，因为优先级无法让一个已耗尽的账号恢复可用。

管理界面把不可用账号按预计恢复时间从早到晚排列。无法确定恢复时间的账号
放在最后。这样，队列反映的是哪个账号真正可能最先恢复，而不是保留已经没有
调度意义的优先级顺序。

如果“可直接使用”和“可安全试用”两组中都没有安全选择，插件不会返回账号，
并允许 CPA fallback。

### 排序示例

假设当前最高 CPA 优先级层中有四个 Codex 账号，实际显示和调度顺序如下：

| 顺序 | 状态 | 插件优先级 | 恢复时间 | 排序原因 |
| --- | --- | ---: | --- | --- |
| 1 | 可直接使用 | 10 | — | 在可直接使用的账号中优先级最高 |
| 2 | 可直接使用 | 0 | — | 账号仍然可用，因此排在所有不可用账号之前 |
| 3 | 周额度耗尽 | 100 | 2 小时后 | 账号不可用，忽略高优先级；它是最早恢复的不可用账号 |
| 4 | 已耗尽或未知 | 1000 | 更晚或未知 | 账号不可用，而且预计最后恢复或无法确定恢复时间 |

## 额度刷新与新周期激活

### 额度刷新

额度刷新从 ChatGPT 读取当前 Codex 额度状态。它不会发送普通模型请求，也不需要
一直打开管理页面。

最近观察到 Codex 活动时，各账号只在自己的刷新期限到达后才会刷新；后台不会
按固定全局间隔反复扫描全部账号。活跃窗口结束并进入空闲后，普通后台刷新会
休眠，直到 Codex 请求、管理操作或到期的新周期操作将其唤醒。

收到 `usage_limit_reached` 响应后，插件会立即把刚才选择的账号标记为临时耗尽，
直到响应中的重置时间；如果响应没有重置时间，则临时阻止两分钟。额度耗尽不会
计入熔断失败。重复出现的非额度错误才由熔断器处理。

### 新周期激活

OpenAI 有时会显示额度重置时间已经到达，但在账号再次发送 Codex 请求之前不会
生成新的额度周期。开启自动新周期激活后，插件会：

1. 再次检查当前额度；
2. 只有确认新周期仍未生成时，才发送一次极小的 Codex 请求；
3. 请求后重新读取额度并验证结果；
4. 持久化操作状态，发生重启时先验证，再决定是否重试。

该功能默认关闭，因为激活请求可能消耗少量额度。多个并发触发会共享同一个操作，
不会重复发送激活请求。

本 fork 新增可选的 resetAt 滑动保护。启用
`reset_probe_require_reset_at_slide: true` 后，首次观察只能记录当前 resetAt，不能
直接授权模型请求；后续观察必须确认 resetAt 至少向后移动
`reset_probe_drift_threshold`，才把它视作仍未真正启动的五小时窗口。

### CPA 暂时无法确认账号列表时

CPA 无法确认当前 Codex 账号列表和优先级时，普通刷新和新周期激活会停止。插件
可以继续提供安全的管理信息，同时重试账号列表同步。

高风险设置 `probe_on_provisional_roster` 允许在这种情况下，使用最近一次保存的
账号列表尝试新周期激活。每次尝试前都会重新验证账号凭据，但插件仍无法保证
账号没有被删除，也无法保证账号没有被移动到其他 CPA 优先级层。除非明确理解并
接受该风险，否则应保持关闭。

## 功能

- 面向 CPA Codex 账号的优化版 Fill First 调度。
- 生产选择与管理队列都按真实可用性优先排序。
- 支持周额度和月额度，五小时额度窗口可选。
- 处理 `usage_limit_reached` 额度耗尽反馈。
- 账号级故障熔断器。
- 按期限驱动的额度刷新，以及可选的新周期激活。
- 支持浏览器语言检测的中英文管理界面。
- 账号别名、备注、标签、分组和插件优先级。
- 调度设置与账号标注的 JSON 导入和导出。
- Linux、macOS、Windows 和 FreeBSD 发布包。

## 安装

在 Codex Fleet Manager 被 CPA 插件商店收录前，请从
[HiJean99 fork Releases](https://github.com/HiJean99/cpa-plugin-codex-fleet-manager/releases)
下载对应平台的压缩包：

```text
codex-fleet-manager_<version>_<goos>_<goarch>.zip
```

从压缩包根目录解压动态库，并放入 CPA 对应平台的插件目录：

- macOS：`codex-fleet-manager.dylib`
- Linux 和 FreeBSD：`codex-fleet-manager.so`
- Windows：`codex-fleet-manager.dll`

示例：

```bash
mkdir -p /path/to/CLIProxyAPI/plugins/darwin/arm64
cp codex-fleet-manager.dylib /path/to/CLIProxyAPI/plugins/darwin/arm64/
```

## CPA 配置

全局启用插件，并启用本插件：

```yaml
plugins:
  enabled: true
  configs:
    codex-fleet-manager:
      enabled: true
      priority: 1 # CPA plugin registration/load priority
```

这里的注册/加载 `priority` 不是 CPA 账号优先级，也不是插件自己的单账号调度
优先级。账号标注和调度设置通过插件页面管理，不使用 CPA 的通用插件表单。

默认调度设置：

```yaml
handle_enabled: true
quota_refresh_interval: 30m
stale_after: 5h
refresh_active_window: 1h
refresh_after_reset_delay: 1m
refresh_retry_delays: 1m,5m,15m
refresh_on_startup: false
monthly_mode: expiry_order
fallback: fill-first
enable_usage_feedback: true
enable_reset_probe: false
reset_probe_model: gpt-5.6-terra
reset_probe_mode: reset_at
reset_probe_timezone: Asia/Shanghai
reset_probe_schedule: 07:00,12:00,17:00
reset_probe_schedule_grace: 15m
reset_probe_all_accounts: false
reset_probe_require_reset_at_slide: false
reset_probe_observation_interval: 30m
reset_probe_drift_threshold: 2m
reset_probe_min_interval: 10m
reset_probe_failure_cooldown: 10m
probe_on_provisional_roster: false
max_refresh_concurrency: 1
quota_endpoint: https://chatgpt.com/backend-api/wham/usage
circuit_failure_threshold: 5
circuit_open_duration: 30m
circuit_half_open_success_threshold: 2
max_log_entries: 200
log_retention: 24h
```

`reset_probe_mode` 可选值：

- `reset_at`：按额度 resetAt 动态观察；可配合 `reset_probe_require_reset_at_slide`。
- `schedule`：仅按 `reset_probe_timezone` 与 `reset_probe_schedule` 的固定时点处理五小时窗口。
  每个时点先读取额度；旧窗口仍剩几秒或几分钟时，会在 `reset_probe_schedule_grace`
  范围内等到 resetAt 成熟后补查。超过宽限则等下一个配置时点，不在夜间滚动。

`monthly_mode` 可选值：

- `expiry_order`：周度账号和月度账号共同按到期时间排序。
- `priority`：在同一个可选择类别和插件优先级中，月度账号排在周度账号前面。

`quota_endpoint` 被限制为预期的 ChatGPT 额度端点，不能改为任意主机。

如果只想把插件当作 **5 小时 warmer**，继续让 CPA 原生 `fill-first` 负责调度，
本 fork 推荐：

```yaml
handle_enabled: false
enable_reset_probe: true
reset_probe_model: gpt-5.6-terra
reset_probe_mode: schedule
reset_probe_timezone: Asia/Shanghai
reset_probe_schedule: 07:00,12:00,17:00
reset_probe_schedule_grace: 15m
reset_probe_all_accounts: true
reset_probe_min_interval: 10m
reset_probe_failure_cooldown: 15m
probe_on_provisional_roster: false
```

`reset_probe_model` 必须是目标 Codex OAuth 当前确实可用的模型。开启
`reset_probe_all_accounts: true` 后，schedule 模式会为每个未禁用的 Codex OAuth 独立
维护暖窗，不受 CPA priority 高低影响；临时 cooldown/unavailable 的账号也保留资格，
以便额度重置后能够被激活。该选项不会修改 CPA priority，也不会接管原生 `fill-first`。
每个北京时间时点仍会先读取额度，已经存在有效窗口就不会发模型请求。
`reset_probe_schedule_grace` 用于服务重启或秒级漂移后的短时间补触发；schedule 模式
同时停用长周期主动激活，避免夜间额外发送 probe。

## 管理界面

从 CPA Management Center 打开 **Codex Fleet Manager**，或者访问：

```text
/v0/resource/plugins/codex-fleet-manager/status
```

页面提供：

- 与生产调度一致的账号队列和下一账号预览；
- 分别显示 CPA 优先级和插件优先级；
- 额度条、重置时间、不可用原因和熔断状态；
- 两种额度窗口的额度条统一按剩余比例显示：60% 及以上为绿色，30%（含）至 60% 以下为橙色，低于 30% 为红色；
- 带有通俗安全说明的调度设置；
- 别名、备注、标签、分组和单账号插件优先级编辑；
- 额度刷新、日志查看/导出以及配置导入/导出；
- 中英文界面切换。

嵌入 CPA Management Center 时，插件初次会跟随 CPA 当前语言：中文 locale 使用
中文，其余 locale 默认使用英文。如果曾在插件内手动选择语言，该选择会被记住，
后续访问时优先使用。CPA 侧边栏统一显示英文名称
**Codex Fleet Manager**。

受保护的数据和操作需要 CPA 管理密钥。默认情况下，密钥只保留在当前浏览器页面
会话中。可选的「在此浏览器中记住管理密钥」设置会以未加密形式将密钥保存到
浏览器本地存储，并在以后访问时自动加载受保护数据。请仅在受信任的设备上启用。
密钥不会写入插件状态、导出文件或日志；取消勾选会删除浏览器中保存的副本。

## 隐私与数据说明

插件在 CPA 进程内部运行，使用 CPA host callback 和插件自己的 CPA Management
API 路由。插件不运行外部服务，也不会向插件作者发送数据。

插件可能使用已经配置在 CPA 中的 Codex 凭据，向以下端点发送认证请求：

```text
GET https://chatgpt.com/backend-api/wham/usage
GET https://chatgpt.com/backend-api/wham/rate-limit-reset-credits
```

开启新周期激活后，插件还可能发送前文说明的极小 Codex 激活请求。

本地插件状态可能包含调度设置、额度快照、操作状态、日志、别名、备注、标签和
分组名称。不要在备注、别名、标签或分组标注中填写秘密。管理界面避免渲染
access token、Authorization header、Cookie 和其他凭据字段。

Resource 路由只提供界面资源。账号数据和受保护操作通过 Management 路由处理，
并要求 CPA 管理密钥。

## 构建

要求：

- `go.mod` 中声明的 Go 1.26 或更高版本。
- CGO 支持，以及用于 `-buildmode=c-shared` 的 C 编译器。
- 用于跨平台发布流程的 `make`。

运行测试：

```bash
make test
```

构建当前平台的动态库：

```bash
make build
```

构建发布压缩包和校验文件：

```bash
make package VERSION=0.1.3-mingli.1
make checksums VERSION=0.1.3-mingli.1
```

Windows 用户可以用以下命令构建 `dist/codex-fleet-manager.dll`：

```powershell
.\build.ps1
```

## GitHub Release

推送 `v0.1.3-mingli.1` 这类 `v*` 标签后，GitHub Actions 会运行发布流程。流程会测试
仓库，并发布各平台压缩包和 `checksums.txt`：

```bash
git tag -a v0.1.3-mingli.1 -m "v0.1.3-mingli.1"
git push origin v0.1.3-mingli.1
```

发布包使用以下命名方式：

```text
codex-fleet-manager_<version>_<goos>_<goarch>.zip
```

## Management API

界面资源路由：

```text
GET /v0/resource/plugins/codex-fleet-manager/status
```

受保护操作需要 CPA 管理密钥：

```text
GET  /v0/management/plugins/codex-fleet-manager/status?format=json
GET  /v0/management/plugins/codex-fleet-manager/logs
GET  /v0/management/plugins/codex-fleet-manager/export
PUT  /v0/management/plugins/codex-fleet-manager/settings
POST /v0/management/plugins/codex-fleet-manager/refresh
POST /v0/management/plugins/codex-fleet-manager/refresh/account
POST /v0/management/plugins/codex-fleet-manager/import
PUT  /v0/management/plugins/codex-fleet-manager/annotations
PATCH /v0/management/plugins/codex-fleet-manager/annotations/account
PATCH /v0/management/plugins/codex-fleet-manager/annotations/group
```

## 许可证

MIT License。参见 [LICENSE](LICENSE)。

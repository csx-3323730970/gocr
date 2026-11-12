# gocr 代码学习路线

目标：在 2~3 天内能从「跑起来」到「讲清楚每个模块为什么这样写」，并为面试准备好可深挖的点。

建议先通读一遍，再按阶段精读；每阶段末尾的「自测问题」能答上来再进入下一阶段。

## 0. 建立全局图（30 分钟）

读：

- `README.md` 的架构图与设计原则
- `docs/adr/0001` ~ `0004`

先记住一个事实：**两个入口共享一条管线**。

```
gocr review ──► internal/pipeline ──► source/selector/rules/bundle/agent/anchor/report
gocr serve  ──► job/worker ──► 同一条 pipeline ──► MySQL
```

自测：

- CLI 模式为什么不连数据库？
- serve 模式相比 CLI 多了哪些工程能力？

## 1. 入口与配置（1~2 小时）

文件：

- `cmd/gocr/main.go`：只做错误与退出码转换
- `internal/cli/root.go`、`review.go`、`serve.go`
- `internal/config/config.go`

关注：

- `ErrBlockingFindings` 如何让 CI 得到退出码 1，而不打印多余错误
- 配置优先级：defaults < YAML < env < flag
- `review.go` 中 dry-run 与实际评审的分叉点

自测：

- 如果新增一个 `--fail-on` flag，应该改哪几处？
- 为什么 `serve` 需要 DSN 而 `review` 不需要？

## 2. 数据契约（1 小时）

文件：

- `internal/report/model.go`：`Finding`、`Anchor`
- `internal/source/diff.go`：`DiffLine`、`Hunk`、`FileDiff`
- `internal/store/store.go`：`Job`、`Store`、`Cache`

关注：

- 为什么行号有 old/new 两套，什么行是「可评论」的
- `Anchor` 的 `confidence` / `method` / `original` 分别解决什么问题

自测：

- 删除行为什么不能作为评论锚点？
- 模型给的行号错位 2 行时，最终数据里怎么体现？

## 3. diff 解析（2~3 小时，最值得投入）

文件：

- `internal/source/diff.go`
- `internal/source/render.go`
- `internal/source/diff_test.go`、`git_test.go`

方法：

- 先看测试用例，再对照解析器实现，最后自己写一个新的 diff 输入跑测试
- 运行 fuzz：`go test ./internal/source -fuzz=FuzzParseFileDiffs -fuzztime=30s`

关注：

- `-U0` 时 `OldLines == 0` 的循环如何工作
- `+++ b/...`、`rename from/to`、`Binary files`、CRLF 的处理
- 计数驱动的 hunk 终止条件，为什么比「读到下一行 @@ 为止」更稳

自测：

- 新增文件、删除文件、重命名文件、二进制文件各会得到什么 `FileDiff`？
- 如果 diff 被截断，解析器会报什么错？

## 4. 选择、规则、分组与并发（2 小时）

文件：

- `internal/selector/filter.go`
- `internal/rules/layer.go` + `docs/*.md`
- `internal/bundle/group.go`、`run.go`

关注：

- 默认忽略列表、大小上限、allowlist 的优先级
- 规则为什么是 append-only（ADR-0004）
- `errgroup.SetLimit` 的并发上限与 `context` 取消传播

自测：

- 一个 100 文件的 PR 会被分成几个 bundle？为什么按顶层目录分？
- 单个 bundle 失败时，管线为什么继续？

## 5. LLM 层（3 小时）

文件：

- `internal/llm/provider.go`、`openai.go`
- `retry.go`、`cache.go`、`schema.go`、`llm_test.go`

关注：

- 领域结构 `Message/ToolCall` 与 OpenAI wire 结构为什么要转换
- HTTP 状态码到 `ErrRateLimited / ErrTransient / ErrFatal` 的分类
- 指数退避 + jitter + `context` 取消
- 内容寻址缓存 key 由哪些字段组成
- `ValidateFindings` 为什么是「校验 + 归一化」，而不是直接透传

自测：

- 429 和 400 分别重试几次？
- 缓存命中时还会不会经过限流和重试？
- 模型输出多包了一层 ```json 时谁负责剥离？

## 6. Agent（3 小时）

文件：

- `internal/agent/loop.go`、`prompt.go`
- `internal/agent/tools/sandbox.go`、`read_file.go`、`grep.go`、`context.go`

关注：

- 循环状态机：`tool_calls` → 执行工具 → 回填 tool 消息 → 下一轮；否则校验 JSON
- 校验失败后的 repair 消息
- 每 bundle 的 token 预算与最大轮次
- `SafePath` 如何防 `../`、绝对路径、盘符路径、symlink 逃逸

自测：

- 模型连续 2 轮调用未知工具，会发生什么？
- 工具返回错误时，为什么是以 tool 消息回填而不是直接终止？

## 7. 行号锚定（2 小时）

文件：

- `internal/anchor/anchor.go`、`similarity.go`、`anchor_test.go`

关注：

- exact → fuzzy（±10 行）→ hunt → downgrade → drop 的优先级
- 归一化（空白折叠、去行尾注释）与 Levenshtein 相似度
- `confidence` 如何随匹配方式变化（1.0 / 0.9·score / 0.6 / 0.5）
- 删除行候选为何被排除

自测：

- 模型行号错 5 行、snippet 正确，走哪条路径？
- snippet 为空但行号合法，最终 line 是多少？

## 8. 管线编排（2 小时）

文件：

- `internal/pipeline/pipeline.go`

关注：

- 从 patch 到 findings 的完整顺序
- 过滤：binary / deleted / 无评论行 / glob / 大小
- 并发下 usage 与失败计数的锁
- 模型文件名清洗（`a/`、`b/`、`./`）
- 去重与排序
- 失败降级策略与 ctx 取消的区别

自测：

- 模型报了一个不在 diff 里的文件，会怎样？
- 为什么 `pipeline.Provider` 可以被任意包装（限流/缓存/重试）？

## 9. 存储、队列与 worker（3 小时）

文件：

- `internal/store/mysql/mysql.go`
- `internal/store/mysql/migrations/0001_init.sql`
- `internal/job/worker.go`、`processor.go`、`scheduler.go`

关注：

- `FOR UPDATE SKIP LOCKED` 的认领 SQL 与索引 `(state, created_at)`
- 认领事务只覆盖「标记 running + 设置 lease」，LLM 调用不在事务内
- `lease_until` 过期 → `RequeueExpired`；attempts ≥ 3 → failed
- `Complete` 的事务边界：状态、findings、缓存
- `cacheAdapter` 为什么存在（Go 方法不能重载）
- worker 的 claim → process → complete/fail 循环与优雅退出

练习：

- 用真实 MySQL 跑：`GOCR_TEST_DSN=... go test -tags=integration ./internal/store/mysql/ -v`
- 手动把某 job 的 `lease_until` 改到过去，观察 worker 日志与状态

自测：

- 服务崩溃时正在跑的任务会怎样？
- 重复提交同一个 diff 会发生什么？幂等键包含哪些内容？

## 10. HTTP 服务（2 小时）

文件：

- `internal/api/server.go`、`server_test.go`
- `internal/cli/serve.go`

关注：

- `202 Accepted` + `Location` 的异步语义
- 幂等键 = sha256(repo + diff)，重复提交返回同一 job id，并返回真实状态
- 队列满 → 429 + `Retry-After`；请求体上限
- provider 包装顺序（由内到外）：OpenAI → 限流 → 缓存 → 重试
- 优雅停机：停止接收 → 排空 HTTP → 等待 worker

自测：

- 为什么幂等入队要用 `ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`？
- 请求体太大时返回什么？在哪一层拦截？

## 11. 评测（2 小时）

文件：

- `evals/runner/main.go`、`metrics.go`、`metrics_test.go`
- `evals/cases/0001-example/`

关注：

- 预测与 gold 的匹配规则：同文件 + 行号 ±2 + 一对一贪心
- precision / recall / F1 / anchor accuracy / tokens / P95 的定义
- baseline 的字段与对比输出
- 为什么 anchor accuracy 只统计 matched 的预测

自测：

- 模型多报 3 条真实但不匹配 gold 的问题，precision 会怎样？
- 评测为什么要固定 temperature、模型和 prompt 版本？

## 12. 测试与工程化（1 小时）

- 表驱动测试：`source`、`anchor`、`llm`
- httptest：`llm`、`api`
- fuzz：`FuzzParseFileDiffs`
- goleak：`bundle.Run`
- 集成测试：`//go:build integration` + 真实 MySQL
- CI：`.github/workflows/ci.yml`；本地 `-race` 需要 CGO/gcc

自测：

- 哪些测试不需要网络和数据库？哪些必须真实依赖？
- CI 里 MySQL 服务容器的健康检查为什么先于测试？

## 13. 进阶专题：熔断、TPM、压测与评测门禁（设计讨论，当前未实现）

这一节是「从能跑到生产可用」的差距，也是面试里最容易被追问的部分。**截至当前版本，这些能力尚未实现**，目标是能讲清设计、取舍和落点，而不是背名词。

### 13.1 熔断器（Circuit Breaker）

要解决的问题：供应商持续 429/5xx 时，重试 + 高并发会形成**重试风暴**：每个失败请求又制造更多请求，把等待超时和连接耗尽，故障被放大。

状态机：

```
closed ──(失败计数/失败率超过阈值)──► open
  ▲                                     │
  │                              冷却期（如 30s）
  │                                     ▼
  └────(探测成功)──── half-open ◄──(冷却结束，只放一个探测)
                          │
                          └──(探测失败)──► open
```

设计落点（放在 `internal/llm`）：

- 建议 provider 链为：`retry( cache( breaker( rateLimit( openai ) ) ) )`
  - 熔断器紧贴真实 provider，因此每次重试的失败都会被记账；
  - 熔断器 open 时快速失败，不经过限流等待；
  - 缓存命中不经过熔断器（没有真实调用，不应影响状态）。
- 计数口径要想清楚：5xx / 超时计入失败；429 是否计入要单独决定（它是限流信号，可以计入也可以只做退避）；400/401 是本地配置问题，**不应**让熔断器打开。
- 半开状态只允许一个探测请求并发通过，否则会瞬间恢复全量流量。
- open 时返回专门的 `ErrCircuitOpen`，pipeline 将其视为 bundle 失败并降级（当前已有单 bundle 降级逻辑）。
- 指标：`gocr_circuit_state{provider}`（0 closed / 1 open / 2 half-open）、`gocr_circuit_rejected_total`、`gocr_circuit_transitions_total`。

面试追问：

- 熔断和重试是什么关系？——重试解决瞬时抖动，熔断阻止持续性故障被放大；两者叠加必须防止重试风暴。
- 连续失败计数和滑动窗口失败率怎么选？——低流量用连续失败，高流量用滑动窗口；要能设置最小请求数避免小样本误判。
- 半开为什么只放一个探测？——防止恢复瞬间把仍在故障的服务再次打挂。

练习：用 `sync.Mutex` + 可注入的时钟实现状态机；用 httptest 连续返回 500，断言 closed → open → half-open → closed；跑 `go test -race`。

### 13.2 TPM 限流（Tokens Per Minute）

RPM 限制每分钟**请求数**，TPM 限制每分钟 **token 吞吐**（输入 + 输出）。供应商通常两者同时限制，而 token 维度更接近真实成本。

难点：请求发出前不知道输出 token 数。两种可行策略：

1. **预扣估算**：按 prompt 估算输入 token（粗略可用字符数 / 4，中文/代码单独校准），再加上 `max_tokens` 作为预留；请求返回后按真实 usage 校正（多退少补）。
2. **事后记账**：请求完成后按 usage 扣减，下一次请求前等待额度；实现简单，但可能已经超发。

`golang.org/x/time/rate` 支持 `WaitN(ctx, n)`，可以按 token 数预扣：

```go
type Limiter struct {
    rpm *rate.Limiter // 每分钟请求数
    tpm *rate.Limiter // 每秒 token 数，burst 设为单请求最大 token 或一分钟额度
}

func (l *Limiter) Wait(ctx context.Context, estTokens int) error {
    if err := l.rpm.Wait(ctx); err != nil {
        return err
    }
    return l.tpm.WaitN(ctx, estTokens)
}
```

注意点：

- `WaitN` 的 n 不能超过 burst；burst 的设置是「最大单请求 token」与「允许的突发额度」之间的取舍。
- 真实的「预扣 + 校正」需要自己维护差额（reservation/debt），因为 `rate.Limiter` 没有内置退款；也可以在 Agent 层记录实际 usage，用于下一轮估算校准。
- 当前代码只实现了 RPM（`internal/job/scheduler.go`），以及 per-bundle 的 **总量**预算（`agent.Agent.TokenBudget`）。TPM 是**速率**限制，和预算不是一回事。
- 指标：`gocr_llm_tokens_total` 已有；新增 `gocr_tpm_wait_seconds` 可观测限流造成的排队时间。

面试追问：

- TPM 和全局 token 预算的区别？——一个是单位时间速率，一个是总量/成本上限。
- 输出 token 不可知怎么办？——预扣最大输出、事后校正；或接受少量超发。
- 429 是 RPM 还是 TPM 触发？怎么区分？——看响应头和错误体；无法区分时两者都要限制。

练习：给 Limiter 加 TPM 预扣，Agent 在每次 Complete 前后记录 usage；用假的慢 provider 观察排队与 429 次数变化。

### 13.3 全局 token 预算与降级

现状：`TokenBudget` 是**每个 bundle** 的预算；一个 PR 有多个 bundle 时，总花费仍可能超预期。

全局预算实现思路：

- 进程内 `atomic.Int64 remaining`，所有 bundle 共享；
- Agent 每轮按 usage 扣减；并发下可用「预留 → 结算」避免超支（先原子扣减估算值，结束后用实际值校正）；
- 预算低于阈值时按优先级降级：减少 tool 轮次 → 跳过低优先级 bundle → 返回部分结果并标记 `FailedBundles`；
- 指标：`gocr_budget_remaining`，日志记录降级原因。

面试追问：并发扣预算如何避免超支？降级应该由哪一层决定（pipeline 还是 agent）？预算耗尽时应该报错还是返回部分结果？

### 13.4 压测与 pprof

目标：证明「调度/队列/存储不是瓶颈」，并拿到可写进 README 的数字。

方法：

- 用 fake provider 构造固定延迟（例如 200ms），压测 50/100 个并发 job：观察排队时间、P95 端到端耗时、goroutine 数、内存；
- `go test -bench` 基准（parser、anchor、pipeline 纯逻辑），`benchstat` 对比优化前后；
- 通过 `net/http/pprof`（**只开在 debug 端口**）抓 CPU/内存/goroutine：
  - `go tool pprof http://127.0.0.1:6060/debug/pprof/profile?seconds=30`
  - `go tool pprof -http=:8081 <profile>` 看火焰图；
- 常见优化点：diff 流式解析（当前是全量读入）、`sync.Pool` 复用缓冲、减少 JSON 序列化、调整 MySQL 连接池、批量插入 findings；
- 优化数字要注明口径：真实 provider 还是 fake provider、并发数、数据集大小。

面试追问：P95 高怎么定位？内存上涨看什么？怎么证明优化有效（基准 + benchstat + 火焰图）？

### 13.5 评测接入 CI 门禁

问题：评测需要真实模型（成本 + 网络），模型升级后历史 baseline 会漂移，CI 里也不该放长期 key。

可行方案：

1. **录制回放**：按请求内容哈希把 provider 响应存成 fixture（类似 VCR/cassette）；CI 用录制数据跑，完全确定、零成本；定期用真实模型刷新录制。
2. **阈值门禁**：precision / anchor accuracy / F1 低于 baseline 阈值就 fail；把 `evals/baseline.json` 纳入版本管理。
3. **统计口径**：按 PR 配对 bootstrap 报置信区间，避免把噪声当成提升；本项目的 runner 目前只输出点估计。
4. **数据治理**：标注指南、开发集/报告集分离、干净的「无缺陷 PR」假阳性集（前面评测评审里提到过）。

面试追问：如何避免评测过拟合？模型升级怎么回归？离线录制和真实调用的一致性怎么保证？

### 13.6 公网 demo（可选）

- Docker 镜像 + 一台轻量云主机即可；Caddy/Nginx 做 TLS；
- `/metrics` 接 Prometheus/Grafana 截图放进 README；
- **不要**在公网暴露无鉴权的提交接口：至少加 token 鉴权 + 请求体上限 + 队列满 429；
- 诚实说明单机设计的扩展路径：无状态 worker + 外部队列 + 共享 MySQL/PostgreSQL。

### 13.7 建议练习顺序

1. 熔断器（状态机 + 单测 + 指标）
2. TPM 预扣与 usage 校正（接进 Agent）
3. 全局预算（原子预留 + 降级）
4. fake provider 压测 + pprof 优化，产出一组数字
5. 录制回放 + CI 评测门禁

## 推荐阅读顺序

第一遍（建立主线）：

`README` → `cli/review.go` → `pipeline.go` → `source/diff.go` → `anchor.go`

第二遍（补工程深度）：

`llm/openai.go` → `agent/loop.go` → `job/worker.go` → `store/mysql/mysql.go` → `api/server.go`

第三遍（评测与取舍）：

`evals/runner` → `docs/adr` → 自己加一个 case / 改一个策略并跑测试

## 面试 3 分钟讲法

- 背景：通用 AI 评审「说得笼统、指不准行」。
- 任务：让意见可锚定、可度量，并把它做成可部署的服务。
- 行动：diff 行号空间 → 锚定纠偏；确定性工程与 agent 分工；bundle 并发；MySQL SKIP LOCKED 队列 + lease；限流/缓存/预算；Prometheus 指标；评测集。
- 结果：anchor accuracy、precision/recall、token 成本、P95（用你自己的实测数字）。
- 诚实边界：单机设计；熔断/TPM/压测待补；评测门禁需要固定模型与离线录制。进阶设计见第 13 节。

# gocr

> 以「行级证据」为核心的 AI 代码评审服务：每条评审意见都能锚定到 diff 的精确行号，锚不上就显式降级，绝不瞎指。

- `gocr review`：本地评审 CLI，无数据库依赖
- `gocr serve`：异步评审服务，MySQL 任务队列 + worker 池 + 预算感知调度 + Prometheus 指标
- 参考 [alibaba/open-code-review](https://github.com/alibaba/open-code-review) 的「确定性工程 × Agent 混合」思路，代码自研

当前状态：**核心功能已实现**。本地 CLI 评审、异步服务（MySQL 队列 + worker + 崩溃恢复）、行号锚定、内容寻址缓存、Prometheus 指标与评测 runner 均可运行。尚未做：压测/pprof 调优、熔断器、线上 demo。

## 技术栈

| 类别 | 选型 |
|---|---|
| 语言 | Go 1.27 |
| CLI | spf13/cobra |
| HTTP | net/http（Go 1.22+ 路由），标准库 |
| 数据库 | MySQL 8.4（InnoDB），database/sql + go-sql-driver/mysql |
| 任务队列 | MySQL `SELECT ... FOR UPDATE SKIP LOCKED` + lease 超时恢复 |
| 文件匹配 | bmatcuk/doublestar（glob） |
| 并发 | errgroup（x/sync）、context 级联取消、x/time/rate 令牌桶 |
| LLM | OpenAI 兼容 `/chat/completions`（DeepSeek / Kimi / Ollama），net/http 直连 |
| 配置 | yaml.v3 + 环境变量（默认 < 文件 < env < flag） |
| 可观测 | log/slog JSON 日志、Prometheus client_golang |
| 测试 | 表驱动 testing、fuzz、httptest、-race、goleak、真实 MySQL 集成测试 |
| 工程 | Makefile、golangci-lint、GitHub Actions、Docker / Compose |
| 评测 | 自建 evals/cases + Go runner，与 baseline 对比 |

## 架构

```
gocr review (CLI, 无 DB)
  source(diff) -> selector -> rules -> bundle -> agent -> anchor -> report

gocr serve (HTTP + MySQL)
  POST /v1/reviews ──► jobs 表 (queued)
                          │
  Worker: Claim (SKIP LOCKED + lease) ──► bundle 并发评审 ──► findings 落库
                          │                       │
                  lease 过期 requeue       Limiter(令牌桶 + token 预算)
                          │
  GET /v1/reviews/{id} / GET /metrics / GET /healthz
```

设计原则：

1. 模型不做机械劳动：选文件、分组、配规则、算行号由代码完成。
2. 每个 bundle 独立上下文与预算，避免大 changeset 撑爆 context 和钱包。
3. 模型输出必须先过结构化校验，不合格降级而不是透传。
4. 所有行号语义收敛到 `source.FileDiff` 一个真相。

## 目录

```
cmd/gocr/              入口（仅做 exit code 转换）
internal/
  cli/                 review / serve 命令
  config/              yaml + env 配置
  source/              git diff 解析、行号空间（唯一接触 git 的包）
  selector/            文件筛选（allowlist / ignore / 大小；select 是关键字故命名 selector）
  rules/               规则分层注入
  bundle/              bundle 分组 + errgroup 并发
  llm/                 OpenAI 兼容 provider、重试、结构化输出校验
  agent/               tool-use 循环与工具（read_file / grep / hunk_context）
  anchor/              行号锚定与纠偏
  pipeline/            一次评审的编排
  report/              Finding 结构（唯一真相）
  store/               Store / Cache 接口；mysql/ 实现 + 迁移
  job/                 worker 池、租约恢复、限流调度
  api/                 HTTP 服务
  obs/                 slog + Prometheus 指标
evals/                 评测集与 runner
docs/adr/              架构决策记录
```

## 快速开始

需要 Go 1.27+、MySQL 8（推荐 Docker）。

```bash
# 1. 起 MySQL 并建库
docker compose up -d mysql

# 2. 起服务（首次启动自动建表；DSN 必须带 parseTime=true）
export GOCR_DSN='gocr:gocr@tcp(127.0.0.1:3306)/gocr?parseTime=true&charset=utf8mb4&loc=Local'
go run ./cmd/gocr serve

# 3. 探活与指标
curl http://localhost:8080/healthz
curl http://localhost:8080/metrics

# 4. 提交一个评审任务（异步），然后轮询结果
curl -X POST localhost:8080/v1/reviews \
  -H 'Content-Type: application/json' \
  -d '{"repo":"acme/api","diff":"diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1,2 @@\n package a\n+import \"os\"\n"}'
curl localhost:8080/v1/reviews/1
```

CLI 模式（不需要数据库）：

```bash
# 也可以直接安装
go install github.com/csx-3323730970/gocr/cmd/gocr@latest

export GOCR_API_KEY=sk-...

# 只列文件与分组，不调模型
go run ./cmd/gocr review --repo . --base HEAD~1 --head HEAD --dry-run

# 完整评审；blocker/major 会让退出码为 1，可直接进 CI
go run ./cmd/gocr review --repo . --base HEAD~1 --head HEAD

# 机器可读输出
go run ./cmd/gocr review --repo . --base HEAD~1 --head HEAD --json

# 追加临时规则文件
go run ./cmd/gocr review --repo . --rule ./extra-rules.md
```

评测（需要 API key；数字请按实测替换）：

```bash
export GOCR_API_KEY=sk-...
go run ./evals/runner                 # 跑全部 case 并与 baseline 对比
go run ./evals/runner -update         # 用当前结果覆盖 baseline.json
```

常用命令：

```bash
make build          # 二进制到 bin/gocr
make test           # 单元测试
make race           # -race 全量（Windows 需 gcc/CGO）
make integration    # 真实 MySQL 集成测试（需 DSN，必须使用可清空的测试库）
make evals          # 评测 runner
make ci             # vet + race + build
```

## 路线图

- [x] 项目骨架：类型 / 接口 / 命令 / HTTP / 迁移 / CI
- [x] diff 解析（-U0/-U3、rename、binary、CRLF）+ 表驱动与 fuzz 测试
- [x] LLM provider + 输出校验 + 修复重试 + 内容寻址缓存
- [x] agent tool-use 循环 + 工具沙箱（read_file / grep / hunk_context）
- [x] MySQL 任务状态机 + 幂等入队 + 查询 API
- [x] SKIP LOCKED 认领 + lease 恢复 + 令牌桶 + 背压 + 优雅停机
- [x] anchor 模块 + 评测 runner（precision/recall/F1/anchor 准确率）
- [ ] 熔断器、TPM 限流
- [ ] 压测 + pprof 优化数字
- [ ] 评测接入 CI 门禁（需要固定 API key/离线录制）
- [ ] 公网 demo 部署

进阶概念（熔断状态机、TPM 预扣与校正、全局预算、压测方法、评测门禁）的设计讨论见 [`docs/learning-path.md`](docs/learning-path.md) 第 13 节。

## 非目标

- 不做多 provider（OpenAI 兼容协议已覆盖 DeepSeek/Kimi/Ollama）
- 不做 MCP / IDE 插件 / Web 前端
- 不自动改代码（只建议，不落盘）
- 不自研 diff 算法与 AST 分析
- 不追求分布式部署（单机设计；无状态 worker + 外部队列是预留扩展点）

## 致谢

受 [alibaba/open-code-review](https://github.com/alibaba/open-code-review) 启发；本项目代码自研，取舍见 `docs/adr/`。

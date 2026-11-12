# ADR-0003：用 MySQL + SKIP LOCKED 做任务队列，而不是 Redis

## 背景

`gocr serve` 需要异步评审：HTTP 提交后立即返回 202，任务持久化，worker 认领执行，服务崩溃后任务不能丢。候选存储有 Redis（asynq）、内存队列、MySQL。

## 候选方案

1. Redis + asynq：成熟、吞吐高，但多一个部署组件，任务状态与业务数据分居两处。
2. 进程内 channel：简单，但进程退出即丢任务，无法重启恢复。
3. MySQL 8（InnoDB）：`SELECT ... FOR UPDATE SKIP LOCKED` 行级认领 + `lease_until` 租约 + 定时回收超时任务；任务状态、findings、LLM 缓存同库同事务。

## 决定

选 3。理由：本项目的吞吐瓶颈在 LLM（秒级），不在数据库（毫秒级），DB 队列足够；事务性认领 + 状态持久化集中在一处，语义简单（at-least-once + 内容寻址幂等缓存）；运维组件最少。单机设计，扩展路径为无状态 worker + 外部队列，不预先引入。

## 后果

- 正收益：崩溃恢复可验证；认领原子性由数据库保证；可直接在面试中讨论事务、索引、隔离级别与连接池。
- 成本：需要 lease 超时 janitor；连接池参数需匹配 worker 数；高吞吐场景不如 Redis。
- 约束：认领事务必须短，LLM 调用不能持事务；重复执行由幂等缓存兜底。

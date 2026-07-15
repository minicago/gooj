# Gooj 分布式评测部署指南

本文档说明如何以**分布式**方式部署 Gooj 的评测模块（不依赖 Web 模块），以及如何
使用新增的「重测 / 取消评测 / 取消成绩 / 批量操作 / 代码相似度检查」功能。

---

## 1. 架构概览

```
                ┌──────────────┐   HTTP /api/judge/*   ┌──────────────────┐
  用户提交 ─────▶│  Web 服务     │ ───────────────────▶ │  协调器 Coordinator │◀── 拉取任务
  (写 submissions)│ (UI + API)   │                      │  (队列分发/心跳/回收) │
                └──────────────┘                      └──────────────────┘
                       │ 共享 DB                              │  ▲
                       │                                      │  │ 上报结果
                  ┌────┴─────┐                          ┌──────┴──┴──────┐
                  │  数据库    │◀──── PopQueuedSubmission ──│  Worker 1..N   │
                  │ SQLite/MySQL│                          │ (无状态评测节点) │
                  └────────────┘                          └─────────────────┘
```

- **Web 服务**：负责用户界面、提交入口、管理 API。可选地在本地直接评测。
- **协调器 Coordinator**：持有任务队列（直接读写数据库），通过 HTTP 把任务分发给
  Worker，并回收结果写库。一个 Coordinator 进程即可，自身也可兼做评测节点。
- **Worker**：无状态评测节点，不断从 Coordinator 拉取任务、用 Docker 运行用户程序、
  上报结果。**不需要 Web 模块，也不需要直连数据库。**

> 核心思想：`PopQueuedSubmission` 在数据库层以原子方式认领任务
> （`SELECT ... FOR UPDATE` + `status=running`），因此无论多少个 Coordinator /
> Worker / Web 本地评测循环同时竞争，同一份提交只会被认领一次。

---

## 2. 配置

`config/config.yaml` 中新增了 `judge` 段：

```yaml
judge:
  mode: "local"              # local | coordinator | worker
  coordinator_addr: "http://localhost:9091"
  coordinator_port: 9091     # coordinator 监听端口
  worker_concurrency: 4      # 每个 worker 进程的最大并发评测数
  local_judge: true          # coordinator 是否同时在本机评测
```

- `local`（默认）：在进程内直接评测（Web 服务 `services.judge: true` 时使用）。
- `coordinator`：启动 HTTP 协调器，并通过 `local_judge` 决定是否同时本地评测。
- `worker`：启动评测 Worker，连接 `coordinator_addr`。

命令行也可覆盖：`gooj -method judge-worker -coordinator http://10.0.0.5:9091 -concurrency 8 -worker-id nodeA`

---

## 3. 部署步骤

### 3.1 前置条件（所有节点）

- Go 1.21+（或直接使用编译好的二进制）
- Docker，且已构建/拉取评测镜像 `gcc-with-time`（`docker build -t gcc-with-time docker/`）
- 数据库：
  - **单机/测试**：SQLite（`database.type: sqlite`），仅 Coordinator / Web 所在机器访问。
  - **多机分布式（推荐）**：MySQL（`database.type: mysql`），所有需要写库的节点
    （Web 与 Coordinator）连同一个 MySQL 实例。SQLite 不支持多机并发写入。

### 3.2 共享评测数据

Worker 与（本地评测的）Coordinator 在运行时会读取磁盘上的题目数据
`data/problem/<problemID>/config.json` 及 `*.in` / `*.ans`。因此**每台评测节点都必须能访问
到 `data/problem/` 目录**。推荐方式：

- 共享卷（NFS / 云盘 / Kubernetes `emptyDir`+init 同步 / Docker volume），或
- 部署时用 `rsync` 把 `data/problem/` 同步到各节点。

> Worker 不需要 `data` 库文件，也不连数据库；它只通过 HTTP 从 Coordinator 获取代码，
> 题目测试数据放在本地磁盘由 `runJudge` 读取。

### 3.3 启动顺序

1. **数据库**：确保 SQLite 文件就位或 MySQL 可连接。
2. **Coordinator**（评测调度节点）：
   ```bash
   ./gooj -method judge-coordinator -config config/config.yaml
   # coordinator 默认监听 :9091，同时本地评测（local_judge: true）
   ```
3. **Worker**（评测计算节点，可多台）：
   ```bash
   ./gooj -method judge-worker -coordinator http://<coordinator-ip>:9091 -concurrency 8
   ```
4. **Web 服务**（前端 + 提交入口，可单独部署，不需要靠近评测节点）：
   ```bash
   ./gooj -method run -config config/config.yaml
   ```
   - 若希望 Web 不参与评测，把 `services.judge` 设为 `false`，评测完全交给 Coordinator + Worker。
   - 若保留 `services.judge: true`，Web 也会作为额外的本地评测循环参与竞争，不影响正确性。

### 3.4 验证

- 查看协调器上的 Worker 列表：
  ```bash
  curl http://<coordinator-ip>:9091/api/judge/workers
  ```
- 健康检查：
  ```bash
  curl http://<coordinator-ip>:9091/api/judge/health
  ```
- 提交一个题目，观察 Worker 日志与 `data/message.txt`，结果最终写回数据库。

---

## 4. 可靠性设计（为什么不会“卡死”）

- **原子认领**：`PopQueuedSubmission` 用事务 + 行锁把 `queued` 改为 `running`，多节点不会重复评测。
- **心跳 + 回收（reaper）**：Coordinator 每 30s 检查一次分配表：
  - 若某 Worker 超过 60s 未发心跳，或某任务认领超过 5 分钟，则把该提交重置为 `queued`，
    由其它节点重新评测——**避免 Worker 崩溃导致提交永久卡在 running**。
- **重启自愈**：Coordinator 启动时把遗留的 `running`  submission 全部重置为 `queued`。
- **取消保护**：若提交在评测过程中被取消，最终结果会被丢弃（`UpdateSubmissionResult` 检测到
  `status=cancelled` 后直接返回），状态保持 `cancelled`。

---

## 5. 新增管理功能（需 EditPermission）

| 功能 | 接口 | 说明 |
|------|------|------|
| 重测 | `POST /api/submission/{id}/rejudge` | 把提交重置为 `queued` 重新评测（先清空旧 TestResults） |
| 取消评测 | `POST /api/submission/{id}/cancel_eval` | 把 `queued`/`running` 的提交置为 `cancelled`（运行中的结果会被丢弃） |
| 取消成绩 | `POST /api/submission/{id}/cancel_score` | 标记 `disqualified`，保留记录但成绩不计入排名 |
| 恢复成绩 | `POST /api/submission/{id}/restore_score` | 撤销取消成绩 |
| 批量操作 | `POST /api/submissions/batch` | Body `{"action":"rejudge|cancel_eval|cancel_score|restore_score","ids":[1,2,3]}` |
| 触发相似度检测 | `POST /api/similarity/check?problem=<id>&threshold=0.6` | 对题目全部提交做两两相似度比较并入库 |
| 查看相似度 | `GET /api/similarity?problem=<id>` | 列出该题目的相似度记录（按相似度降序） |

### 代码相似度算法

`similarity` 包实现轻量级查重：
1. 归一化：去除 `/* */`、`//` 注释、字符串字面量，转小写；
2. 分词：提取标识符/数字 token；
3. 取连续 `k=5` 个 token 组成的 k-gram，用 FNV-1a 哈希成集合；
4. 两两计算 **Jaccard 相似度** `|A∩B| / |A∪B|`；
5. 相似度 ≥ `threshold`（默认 0.6）的提交对写入 `similarity_records` 表。

> 该算法对“改排版/改注释/改字符串”不敏感，但会保留变量名差异（不重命名变量），
> 适合作为初步 plagiarism 筛查。对超大提交量（超过约 20 万对比较）会主动拒绝以避免
> O(n²) 开销爆炸。

---

## 6. 扩缩容

- **增加算力**：直接新增 Worker 进程/机器，Coordinator 通过心跳自动纳入调度。
- **减少算力**：关闭 Worker 即可；其未完成任务会在 Coordinator 回收超时后被重新分配。
- **高可用 Coordinator**：当前为单实例。生产可将数据库（MySQL）做主从，Coordinator
  可多实例部署（它们通过共享 DB 与原子认领互不冲突；同时把 `local_judge` 设为 `true`
  让每个 Coordinator 也参与评测）。Worker 端 `coordinator_addr` 指向负载均衡 / VIP。

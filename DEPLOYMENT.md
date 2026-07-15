# Gooj 部署指南

本指南说明如何把 Gooj（一个在线评测系统，Online Judge）以**单机 / 开发**模式部署运行。
分布式评测（多机 Coordinator + Worker）请参见 [DISTRIBUTED_DEPLOYMENT.md](./DISTRIBUTED_DEPLOYMENT.md)。

---

## 1. 架构概览（单机模式）

```
┌──────────────────────────────────────────────┐
│  gooj.out  (--method=run)                      │
│                                                │
│   ├─ Web 服务      :HTTP  (UI + 提交 + 管理 API) │
│   ├─ SQL 服务       :SQLite / MySQL 读写         │
│   ├─ 文件服务       :题目/提交文件读写            │
│   └─ 评测循环       :轮询 DB 队列，用 Docker 跑用户程序 │
│                                                │
│   └─ 命令控制台(cmd service) :9090 (可选)        │
└──────────────────────────────────────────────┘
         │
         ▼
   data/app.db  (SQLite) 或  MySQL 实例
```

默认 `judge.mode: local`：Web 进程内的评测循环直接认领并提交任务，**单进程即包含全部功能**。

---

## 2. 环境要求

| 依赖 | 说明 |
|------|------|
| **Go 1.21+** | 源码使用 `go 1.25.3`（`go.mod`），编译需要 Go 工具链；也可直接用他人编译好的二进制。 |
| **gcc + CGO** | SQLite 驱动 `mattn/go-sqlite3` 依赖 CGO。编译时必须 `CGO_ENABLED=1` 且本机有 `gcc`（或 `clang`）。 |
| **Docker**（仅评测需要） | 实际编译/运行用户提交的 C++ 代码在 Docker 沙箱中完成。若不装 Docker，**网页与提交入口正常，但提交会一直停留在 `queued`**。 |
| **数据库** | 默认 SQLite（零配置）；如需多机或更高并发，改用 MySQL（见第 7 节）。 |

---

## 3. 获取代码与构建

```bash
# 1. 拉取代码
git clone <repo-url> gooj
cd gooj

# 2. 编译（CGO 必须开启）
CGO_ENABLED=1 go build -o gooj.out .

# 3. 准备评测镜像（不评测可跳过）
docker build -t gcc-with-time docker/
```

> 评测镜像名固定为 `gcc-with-time`，由 `docker/Dockerfile` 构建（其 `time` 二进制被装入 `/usr/bin/time` 用于统计资源占用）。

---

## 4. 配置文件 `config/config.yaml`

```yaml
database:
  type: "sqlite"          # sqlite | mysql
  sqlite:
    path: "data/app.db"   # 相对路径，基于运行时的当前工作目录
  mysql:
    host: "localhost"
    port: 3306
    user: "root"
    password: ""          # ← 使用 MySQL 时填写
    dbname: "gooj"

server:
  port: 8081              # Web 服务监听端口

cmd:
  port: 9090              # 命令控制台端口（后台管理服务用）

services:
  sql: true               # 是否启用数据库服务
  judge: true             # 是否启用进程内评测循环
  file: true              # 是否启用文件服务

judge:
  mode: "local"           # local | coordinator | worker
  coordinator_addr: "http://localhost:9091"
  coordinator_port: 9091
  worker_concurrency: 4
  per_task_concurrency: 8  # 单任务内并发评测的测试点数（单任务多线程），默认 8，按主机核数调整
  local_judge: true
```

常用调整：
- **改端口**：改 `server.port`（例如已在本机 8012 部署时设为 `8012`）。
- **只跑 Web 不评测**：`services.judge: false`（评测交给独立 Coordinator/Worker）。
- **权重等运行期参数**：见 [DISTRIBUTED_DEPLOYMENT.md](./DISTRIBUTED_DEPLOYMENT.md) 中 rating 配置。

> 命令参数 `--config` 可指定配置文件路径（默认 `config/config.yaml`）。无 `--port` 参数，端口只能从配置文件读取。

---

## 5. 启动

### 5.1 前台运行（开发调试）

```bash
# 在项目根目录执行，data/ 相对路径才会正确解析
./gooj.out --method=run --config config/config.yaml
```

输出示例：
```
Configuration loaded from config/config.yaml
SQL service started
File service started
Judge service started
Background services started
listening on :8081
```

### 5.2 后台运行

**方式 A：使用内置 daemon 模式**

```bash
./gooj.out --method=run --background=true
```
注意：该模式通过 `go-daemon` 脱离终端，日志默认不落盘，适合生产常驻。

**方式 B：nohup（推荐用于开发，便于看日志）**

```bash
nohup ./gooj.out --method=run > server.log 2>&1 &
echo $!   # 记下 PID，便于停止
```

### 5.3 停止

```bash
kill <PID>                 # 正常退出
# 或按命令控制台：向 :9090 发送 shutdown
```

---

## 6. 首次登录与初始管理员

首次启动会自动初始化一个「超级组 `super`」和一个管理员账号 **`root`**：

- 账号：`root`
- 密码：**自动随机生成**，并写入 `data/rootpassword.txt`（权限 `0644`）
- `root` 属于 `super` 组，拥有题目、用户、组管理全部权限，且默认已审核通过

```bash
cat data/rootpassword.txt   # 查看初始密码
```

登录后请**立即修改密码**（用户页「修改密码」或后台「重置密码」），并可删除该明文文件：

```bash
rm data/rootpassword.txt
```

> 如果 `root` 已存在，重启不会覆盖其密码（仅确保其在 `super` 组且已审核）。

---

## 7. 改用 MySQL（可选）

1. 在 MySQL 中创建数据库：
   ```sql
   CREATE DATABASE gooj CHARACTER SET utf8mb4;
   ```
2. 编辑 `config/config.yaml`：
   ```yaml
   database:
     type: "mysql"
     mysql:
       host: "127.0.0.1"
       port: 3306
       user: "gooj"
       password: "<填写你的密码>"   # ← 敏感内容，请手动填写
       dbname: "gooj"
   ```
3. 重新启动服务，程序会自动 `AutoMigrate` 建表。

> 涉及数据库账号/密码等敏感信息时，请自行填写配置文件，**不要提交到版本库**（建议在 `.gitignore` 中忽略含密码的配置）。

---

## 8. 验证部署

```bash
# 主页应返回 200
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8081/

# 未登录访问受保护接口应跳转登录（302）
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8081/api/users

# 用 root 登录后访问受保护接口应返回 200
curl -s -c /tmp/cj.txt -X POST http://localhost:8081/login \
     -H "Content-Type: application/json" \
     -d '{"username":"root","password":"<上面查到的密码>"}'
curl -s -b /tmp/cj.txt -o /dev/null -w "%{http_code}\n" http://localhost:8081/api/users
```

---

## 9. 评测功能说明（Docker 沙箱）

提交代码后，进程内评测循环 `StartJudge()` 不断从数据库认领 `queued` 任务，在 Docker 容器中：

- 用 `g++ -O2 -std=c++17` 编译（编译内存上限 512MB，超时 10s）；
- 用 `/usr/bin/time -v` 运行，并施加 `--network none`、`--memory` 上限、`--cpus 1.0`、
  `--ulimit stack=262144` 等限制，**防止用户程序爆栈 / 耗尽资源 / 联网**；
- 超时则用 `docker rm -f` 强制回收容器，避免泄漏。

前提条件：**Docker 守护进程在运行，且镜像 `gcc-with-time` 已构建**（见第 3 节）。
否则提交会停留在 `queued`，日志中可能出现 `Failed to run docker`。

---

## 10. 常见问题

| 现象 | 原因 / 处理 |
|------|------------|
| `listen tcp 127.0.0.1:9090: bind: address already in use` | 命令控制台端口被占用，**不影响主服务**；可改 `cmd.port` 或释放占用进程。 |
| 提交一直 `queued` | Docker 未运行，或镜像 `gcc-with-time` 未构建（见第 9 节）。 |
| `failed to open SQLite database` | `data/` 目录不存在或路径错误；确保在项目根目录运行，或预先 `mkdir -p data`。 |
| 编译报 `cgo: C compiler "gcc" not found` | 未开启 CGO 或无 gcc；用 `CGO_ENABLED=1` 并安装 gcc 后重新编译。 |
| 端口被占用 | 修改 `server.port` 后重启。 |
| 想换数据库 | 见第 7 节（SQLite → MySQL）。 |

---

## 11. 分布式 / 高并发部署

当单机评测能力不足或需要多机时，把 `judge.mode` 设为 `coordinator` / `worker` 拆分评测，
详见 [DISTRIBUTED_DEPLOYMENT.md](./DISTRIBUTED_DEPLOYMENT.md)。

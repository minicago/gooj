#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
将 Gooj 的题目批量转换为 HydroOJ (Hydro) 支持的导入 zip 格式。

Gooj 题目存储在:
  - data/problem/<id>/config.json   评测配置 (memory_limit MB, time_limit ms, test_cases 分组)
  - data/problem/<id>/<n>.in, <n>.ans  测试数据
  - data/problem/<id>/statement.md     题面 (通常只有标题)
  - data/app.db 的 problems 表         标题 / 完整题面 / 时空间限制

Hydro 导入格式 (hydro-compress) 每个题目一个顶层目录 <id>/:
  <id>/problem.yaml         pid / title / tag / nSubmit / nAccept
  <id>/problem_zh.md        题面 (markdown)
  <id>/testdata/config.yaml type / time / memory / subtasks
  <id>/testdata/<n>.in
  <id>/testdata/<n>.out     (Gooj 的 .ans 重命名为 .out)
  <id>/solution/<name>.md   (可选, 本脚本不生成)

用法:
  python3 convert_to_hydro.py
  python3 convert_to_hydro.py --src data/problem --db data/app.db --out data/HydroExport.zip
  python3 convert_to_hydro.py --per-problem --out-dir data/HydroProblems
  python3 convert_to_hydro.py --owner 2 --pid-prefix C        # 非交互: 固定 owner + PID 前缀
  python3 convert_to_hydro.py --owner 2 --pid-map pid.json    # 非交互: PID 映射文件
  python3 convert_to_hydro.py 2 P1132 P1133                   # 位置参数: owner=2, 两题 PID 分别 P1132/P1133
  python3 convert_to_hydro.py 2 P1132                         # 只给首个 PID, 后续按数字递增: P1132, P1133, P1134 ...
  python3 convert_to_hydro.py 2                               # 位置参数仅 owner=2, PID 仍逐题交互询问
  python3 convert_to_hydro.py --mysql 2 P1132                  # 从 MySQL 读取 (连接参数读 config/config.yaml 的 mysql 块)
  python3 convert_to_hydro.py --mysql --mysql-host 127.0.0.1 --mysql-user gooj --mysql-password 'xxx' 2 P1132  # 命令行覆盖

说明:
  - owner (HydroOJ 上的用户ID) 与每题的 PID (字符串) 取决于 HydroOJ 实际情况。
    不传 --owner / --pid-prefix / --pid-map 时, 脚本会交互式询问:
    owner 只问一次; 每个题目的 PID 单独询问 (默认取 Gooj 数字ID)。
  - 非 TTY (管道/重定向) 且未提供 --owner/--pid-* 时, owner 默认 1, PID 默认 Gooj 数字ID。
"""

import os
import re
import sys
import json
import shutil
import sqlite3
import argparse
import tempfile
import zipfile


# --------------------------------------------------------------------------- #
# 极简 YAML 写出 (不依赖 PyYAML)
# --------------------------------------------------------------------------- #
def _y(v):
    """把标量写成 YAML 字面量 (字符串一律双引号转义, 最稳妥)。"""
    if isinstance(v, bool):
        return "true" if v else "false"
    if isinstance(v, (int, float)):
        return str(v)
    s = str(v).replace("\\", "\\\\").replace('"', '\\"')
    return '"' + s + '"'


def build_problem_yaml(pid, title, tags, n_submit, n_accept, owner):
    out = []
    out.append("pid: " + _y(str(pid)))
    out.append("owner: " + _y(str(owner)))
    out.append("title: " + _y(title if title else str(pid)))
    if tags:
        out.append("tag:")
        for t in tags:
            out.append("  - " + _y(t))
    else:
        out.append("tag: []")
    if n_submit is not None:
        out.append("nSubmit: " + str(n_submit))
    if n_accept is not None:
        out.append("nAccept: " + str(n_accept))
    return "\n".join(out) + "\n"


def build_testdata_config(memory_mb, time_ms, subtasks):
    """
    subtasks: [ {id, score, cases:[(in_file, out_file), ...]}, ... ]
    """
    out = []
    out.append("type: default")
    if time_ms:
        out.append("time: %dms" % time_ms)
    out.append("memory: %dMB" % memory_mb)
    out.append("subtasks:")
    for st in subtasks:
        out.append("  - score: " + str(st["score"]))
        out.append("    if: []")          # Gooj 无子任务依赖信息, 置空
        out.append("    id: " + str(st["id"]))
        out.append("    type: min")       # 子任务内全部通过才得分, 对应 Gooj 分组语义
        out.append("    cases:")
        for inp, outp in st["cases"]:
            out.append("      - input: " + _y(inp))
            out.append("        output: " + _y(outp))
    return "\n".join(out) + "\n"


# --------------------------------------------------------------------------- #
# 读取数据源
# --------------------------------------------------------------------------- #
def _query_problems(cur):
    """从 problems / submissions 表读取元信息, 返回 dict。数据库无关, 仅依赖列名一致。"""
    problems = {}
    cur.execute(
        "SELECT id, title, description, time_limit_ms, mem_limit_mb "
        "FROM problems"
    )
    for pid, title, desc, tl, ml in cur.fetchall():
        problems[pid] = {
            "title": title or "",
            "description": desc or "",
            "time_limit_ms": tl or 0,
            "mem_limit_mb": ml or 0,
            "nSubmit": None,
            "nAccept": None,
        }
    # 提交统计
    try:
        cur.execute(
            "SELECT problem_id, COUNT(*), "
            "SUM(CASE WHEN status IN ('accepted','ok') THEN 1 ELSE 0 END) "
            "FROM submissions GROUP BY problem_id"
        )
        for pid, total, acc in cur.fetchall():
            if pid in problems:
                problems[pid]["nSubmit"] = total or 0
                problems[pid]["nAccept"] = acc or 0
    except Exception:
        pass
    return problems


def load_db_sqlite(db_path):
    problems = {}
    if not db_path or not os.path.exists(db_path):
        return problems
    try:
        con = sqlite3.connect(db_path)
        cur = con.cursor()
        problems = _query_problems(cur)
        con.close()
    except sqlite3.Error as e:
        print("警告: 读取 SQLite 失败: %s" % e, file=sys.stderr)
    return problems


def load_db_mysql(cfg):
    """cfg: dict(host, port, user, password, db). 返回与 load_db_sqlite 相同结构。"""
    try:
        import pymysql
        connect = lambda: pymysql.connect(
            host=cfg["host"], port=int(cfg["port"]), user=cfg["user"],
            password=cfg["password"] or "", database=cfg["db"],
            charset="utf8mb4", connect_timeout=10,
        )
        driver = "pymysql"
    except ImportError:
        try:
            import mysql.connector
            connect = lambda: mysql.connector.connect(
                host=cfg["host"], port=int(cfg["port"]), user=cfg["user"],
                password=cfg["password"] or "", database=cfg["db"],
                charset="utf8mb4", connection_timeout=10,
            )
            driver = "mysql.connector"
        except ImportError:
            print("错误: 使用 MySQL 需要 pymysql 或 mysql-connector-python, "
                  "请先 `pip install pymysql`", file=sys.stderr)
            sys.exit(1)
    try:
        con = connect()
        cur = con.cursor()
        problems = _query_problems(cur)
        cur.close()
        con.close()
        print("已从 MySQL (%s:%s/%s) 读取 %d 道题的元信息 [%s]"
              % (cfg["host"], cfg["port"], cfg["db"], len(problems), driver))
        return problems
    except Exception as e:
        print("错误: 连接 MySQL 失败: %s" % e, file=sys.stderr)
        sys.exit(1)


def load_mysql_cfg_from_yaml(yaml_path):
    """尝试从 config/config.yaml 读取 database.mysql 块, 返回 dict 或 None。"""
    if not os.path.exists(yaml_path):
        return None
    try:
        import yaml  # 惰性导入, 避免 sqlite-only 用户缺少 PyYAML
        with open(yaml_path, "r", encoding="utf-8") as f:
            doc = yaml.safe_load(f) or {}
        db = doc.get("database", {}) or {}
        m = db.get("mysql", {}) or {}
        if not m:
            return None
        return {
            "host": str(m.get("host", "localhost")),
            "port": int(m.get("port", 3306)),
            "user": str(m.get("user", "root")),
            "password": str(m.get("password", "")),
            "db": str(m.get("dbname", "gooj")),
        }
    except Exception:
        return None


def load_db(db_path, mysql_cfg=None):
    """mysql_cfg 为 None 时走 SQLite, 否则走 MySQL。"""
    if mysql_cfg is not None:
        return load_db_mysql(mysql_cfg)
    return load_db_sqlite(db_path)


def natural_key(name):
    return [int(t) if t.isdigit() else t for t in
            "".join(("1" if c.isdigit() else "0") + c for c in name).split("0")
            if t]


def inc_pid(pid, delta):
    """对 PID 中最后一组连续数字做自增, 保留原数字宽度 (零填充)。

    例: inc_pid('P1132', 1) -> 'P1133'; inc_pid('T05', 2) -> 'T07';
        无数字时直接末尾拼接: inc_pid('X', 3) -> 'X3'。
    """
    m = re.search(r"\d+", pid)
    if not m:
        return pid + str(delta)
    num = int(m.group())
    width = len(m.group())
    repl = str(num + delta).zfill(width)
    return pid[:m.start()] + repl + pid[m.end():]


def load_config(src_dir):
    """读取 config.json, 失败返回 None。"""
    cfg_path = os.path.join(src_dir, "config.json")
    if os.path.exists(cfg_path):
        try:
            with open(cfg_path, "r", encoding="utf-8") as f:
                return json.load(f)
        except (ValueError, OSError) as e:
            print("警告: 解析 %s 失败: %s" % (cfg_path, e), file=sys.stderr)
    return None


# --------------------------------------------------------------------------- #
# 转换单题 -> 暂存目录
# --------------------------------------------------------------------------- #
def convert_problem(gooj_id, src_dir, db_info, stage_dir, hydro_pid, owner):
    pid = hydro_pid
    cfg = load_config(src_dir)

    # 时空间限制: 优先 DB, 回退 config.json
    mem_mb = 0
    time_ms = 0
    if db_info:
        mem_mb = db_info.get("mem_limit_mb") or 0
        time_ms = db_info.get("time_limit_ms") or 0
    if not mem_mb and cfg:
        mem_mb = cfg.get("memory_limit") or 0
    if not time_ms and cfg:
        time_ms = cfg.get("time_limit") or 0
    if not mem_mb:
        mem_mb = 256
    if not time_ms:
        time_ms = 1000

    # 题面/标题: 优先 DB
    title = (db_info or {}).get("title") or ""
    desc = (db_info or {}).get("description") or ""
    stmt_path = os.path.join(src_dir, "statement.md")
    if not desc and os.path.exists(stmt_path):
        with open(stmt_path, "r", encoding="utf-8") as f:
            desc = f.read()
    if not title:
        # 用 statement 第一行或目录名
        first = (desc.splitlines() or [str(pid)])[0].strip()
        title = first or str(pid)

    prob_dir = os.path.join(stage_dir, str(pid))
    td_dir = os.path.join(prob_dir, "testdata")
    os.makedirs(td_dir, exist_ok=True)

    # 子任务
    subtasks = []
    if cfg and cfg.get("test_cases"):
        for i, grp in enumerate(cfg["test_cases"], start=1):
            cases = []
            for n in grp.get("cases", []):
                inp = "%s.in" % n
                outp = "%s.out" % n
                s_in = os.path.join(src_dir, "%s.in" % n)
                s_out = os.path.join(src_dir, "%s.ans" % n)
                if os.path.exists(s_in):
                    shutil.copy(s_in, os.path.join(td_dir, inp))
                if os.path.exists(s_out):
                    shutil.copy(s_out, os.path.join(td_dir, outp))
                cases.append((inp, outp))
            if cases:
                subtasks.append({
                    "id": i,
                    "score": grp.get("score", 0),
                    "cases": cases,
                })
    else:
        # 回退: 自动发现 *.in, 全部归入一个子任务
        ins = sorted(
            (f for f in os.listdir(src_dir) if f.endswith(".in")),
            key=natural_key,
        )
        cases = []
        for f in ins:
            n = f[:-3]
            outp = n + ".out"
            shutil.copy(os.path.join(src_dir, f), os.path.join(td_dir, f))
            s_out = os.path.join(src_dir, n + ".ans")
            if os.path.exists(s_out):
                shutil.copy(s_out, os.path.join(td_dir, outp))
            cases.append((f, outp))
        if cases:
            subtasks.append({"id": 1, "score": 100, "cases": cases})

    if not subtasks:
        print("警告: 题目 %s 没有找到任何测试数据, 跳过" % pid, file=sys.stderr)
        return False

    # 写出文件
    tags = []  # Gooj 没有 tag 概念, 留空; 如需可在此扩展
    with open(os.path.join(prob_dir, "problem.yaml"), "w", encoding="utf-8") as f:
        f.write(build_problem_yaml(
            pid, title, tags,
            (db_info or {}).get("nSubmit"),
            (db_info or {}).get("nAccept"),
            owner,
        ))
    with open(os.path.join(prob_dir, "problem_zh.md"), "w", encoding="utf-8") as f:
        f.write(desc if desc else title)
    with open(os.path.join(td_dir, "config.yaml"), "w", encoding="utf-8") as f:
        f.write(build_testdata_config(mem_mb, time_ms, subtasks))
    return True


# --------------------------------------------------------------------------- #
# 交互式输入
# --------------------------------------------------------------------------- #
def prompt(msg, default=None):
    """交互式询问。非 TTY (管道/重定向) 时直接返回 default, 保证脚本可被非交互调用。"""
    if not sys.stdin.isatty():
        return default
    if default is not None:
        val = input("%s [%s]: " % (msg, default)).strip()
        return val or default
    return input("%s: " % msg).strip()


# --------------------------------------------------------------------------- #
# 打包
# --------------------------------------------------------------------------- #
def zip_tree(zf, tree_root):
    for root, _dirs, files in os.walk(tree_root):
        for fn in files:
            full = os.path.join(root, fn)
            arc = os.path.relpath(full, tree_root)
            zf.write(full, arc)


def main():
    ap = argparse.ArgumentParser(description="Gooj -> Hydro 题目导出")
    ap.add_argument("--src", default="data/problem",
                    help="Gooj 题目目录 (默认 data/problem)")
    ap.add_argument("--db", default="data/app.db",
                    help="Gooj SQLite 数据库 (默认 data/app.db)")
    ap.add_argument("--out", default="data/HydroExport.zip",
                    help="合并导出的 zip 路径 (默认 data/HydroExport.zip)")
    ap.add_argument("--per-problem", action="store_true",
                    help="每题单独一个 zip, 输出到 --out-dir")
    ap.add_argument("--out-dir", default="data/HydroProblems",
                    help="--per-problem 时的输出目录")
    ap.add_argument("--owner", default=None,
                    help="HydroOJ 上的 owner 用户ID (不填则交互询问)")
    ap.add_argument("--pid-prefix", default=None,
                    help="PID 前缀, 例如 'C' -> C1, C2 ... (不填且非交互时回退为 Gooj 数字ID)")
    ap.add_argument("--pid-map", default=None,
                    help="PID 映射 JSON 文件, 形如 {\"1\":\"C1008\",\"2\":\"C1009\"}")
    # ---- MySQL 数据源 (可选, 与 --db 二选一) ----
    ap.add_argument("--mysql", action="store_true",
                    help="从 MySQL 读取题目数据 (而非 SQLite)。连接参数默认读 config/config.yaml 的 mysql 块")
    ap.add_argument("--mysql-host", default=None, help="MySQL host (默认 config.yaml 或 localhost)")
    ap.add_argument("--mysql-port", default=None, help="MySQL port (默认 3306)")
    ap.add_argument("--mysql-user", default=None, help="MySQL user (默认 root)")
    ap.add_argument("--mysql-password", default=None, help="MySQL password")
    ap.add_argument("--mysql-db", default=None, help="MySQL database 名 (默认 gooj)")
    # 位置参数: [owner] [pid1] [pid2] ...
    #   仅传 owner 时所有题交互询问 PID; 传齐 PID 则非交互。
    ap.add_argument("pos", nargs="*",
                    help="可选位置参数: 第一个为 owner, 其余为各题 PID (按 Gooj ID 升序对应)")
    args = ap.parse_args()

    # ---- 解析位置参数: pos[0]=owner, pos[1..]=逐题 PID ----
    cli_owner = None
    cli_pids = []
    if args.pos:
        if args.pos[0].isdigit() or len(args.pos) >= 2 or not args.owner:
            # 第一个看起来像 owner (纯数字) 或用户没通过 --owner 指定 -> 当作 owner
            if args.owner is None:
                cli_owner = args.pos[0]
                cli_pids = args.pos[1:]
        else:
            cli_pids = args.pos
    if args.owner is not None:
        cli_owner = args.owner

    if not os.path.isdir(args.src):
        print("错误: 题目目录不存在: %s" % args.src, file=sys.stderr)
        sys.exit(1)

    # ---- 数据源: MySQL 或 SQLite ----
    mysql_cfg = None
    if args.mysql or args.mysql_host or args.mysql_port or args.mysql_user \
            or args.mysql_password is not None or args.mysql_db:
        # 优先命令行, 否则从 config/config.yaml 的 mysql 块取默认值
        ycfg = load_mysql_cfg_from_yaml("config/config.yaml") or {
            "host": "localhost", "port": 3306, "user": "root",
            "password": "", "db": "gooj",
        }
        mysql_cfg = {
            "host": args.mysql_host or ycfg["host"],
            "port": int(args.mysql_port or ycfg["port"]),
            "user": args.mysql_user or ycfg["user"],
            "password": args.mysql_password if args.mysql_password is not None
                        else ycfg["password"],
            "db": args.mysql_db or ycfg["db"],
        }
        db_info = load_db(None, mysql_cfg)
    else:
        db_info = load_db(args.db)
    print("已从数据库读取 %d 道题的元信息" % len(db_info))

    pids = sorted(
        (d for d in os.listdir(args.src)
         if d.isdigit() and os.path.isdir(os.path.join(args.src, d))),
        key=lambda x: int(x),
    )
    if not pids:
        print("错误: %s 下没有找到数字命名的题目目录" % args.src, file=sys.stderr)
        sys.exit(1)

    # ---- owner: 优先 位置参数/--owner, 否则交互询问, 再否则默认 1 ----
    if cli_owner is not None:
        owner = cli_owner
    else:
        owner = prompt("请输入 owner 用户ID (HydroOJ 上的用户ID)", default="1")
    if not owner:
        owner = "1"

    # ---- PID 映射: 优先 --pid-map 文件 ----
    pid_map = {}
    if args.pid_map:
        try:
            with open(args.pid_map, "r", encoding="utf-8") as f:
                pid_map = {str(k): str(v) for k, v in json.load(f).items()}
        except (ValueError, OSError) as e:
            print("警告: 读取 --pid-map 失败: %s" % e, file=sys.stderr)

    # 位置参数提供的逐题 PID (按 Gooj ID 升序对应)。
    # 若只给了部分/一个 PID, 则后续题目按最后一个 PID 中的数字递增。
    pos_pid_by_gooj = {}
    n_provided = min(len(cli_pids), len(pids))
    for i in range(len(pids)):
        gooj_id = int(pids[i])
        if i < n_provided:
            pos_pid_by_gooj[gooj_id] = cli_pids[i]
        elif cli_pids:
            # 从最后一个显式 PID 起自增
            pos_pid_by_gooj[gooj_id] = inc_pid(cli_pids[-1], i - n_provided + 1)

    def resolve_pid(gooj_id):
        if str(gooj_id) in pid_map:
            return pid_map[str(gooj_id)]
        if gooj_id in pos_pid_by_gooj:
            return pos_pid_by_gooj[gooj_id]
        if args.pid_prefix:
            return args.pid_prefix + str(gooj_id)
        # 交互: 每题询问; 非交互且无前缀/映射/位置参数时回退为数字ID
        info = db_info.get(gooj_id) or {}
        label = info.get("title") or str(gooj_id)
        return prompt("题目 [%s] (Gooj ID %s) 的 HydroOJ PID" % (label, gooj_id),
                      default=str(gooj_id))

    ok = 0
    if args.per_problem:
        os.makedirs(args.out_dir, exist_ok=True)
        for pid in pids:
            gooj_id = int(pid)
            hydro_pid = resolve_pid(gooj_id)
            with tempfile.TemporaryDirectory() as stage:
                if convert_problem(gooj_id, os.path.join(args.src, pid),
                                   db_info.get(gooj_id), stage,
                                   hydro_pid, owner):
                    zpath = os.path.join(args.out_dir, "%s.zip" % hydro_pid)
                    with zipfile.ZipFile(zpath, "w", zipfile.ZIP_DEFLATED) as zf:
                        zip_tree(zf, stage)
                    ok += 1
                    print("已导出: Gooj#%s -> PID %s (%s)" % (pid, hydro_pid, zpath))
    else:
        os.makedirs(os.path.dirname(os.path.abspath(args.out)), exist_ok=True)
        with tempfile.TemporaryDirectory() as stage:
            for pid in pids:
                gooj_id = int(pid)
                hydro_pid = resolve_pid(gooj_id)
                convert_problem(gooj_id, os.path.join(args.src, pid),
                                db_info.get(gooj_id), stage,
                                hydro_pid, owner)
                ok += 1
                print("已处理: Gooj#%s -> PID %s" % (pid, hydro_pid))
            with zipfile.ZipFile(args.out, "w", zipfile.ZIP_DEFLATED) as zf:
                zip_tree(zf, stage)
        print("已导出 %d 道题到: %s" % (ok, args.out))

    print("完成, 成功 %d / 共 %d 题" % (ok, len(pids)))


if __name__ == "__main__":
    main()

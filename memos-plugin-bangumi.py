#!/usr/bin/env python3
# Copyright (C) 2026 memos-plugin-bangumi contributors
# SPDX-License-Identifier: GPL-3.0-or-later
"""memos-plugin-bangumi - 将 Bangumi 用户「看过/玩过/读过/听过」且带短评的收藏导入 Memos。

纯文字 memo，正文含条目名、完成态文案、短评与 Bangumi 链接。
按 memo uid（bgm-{subject_id}）幂等，重复运行不产生重复 memo；
默认增量同步（状态文件记录最新 updated_at，提前停止处理更旧条目），--full 强制全量。

两种写入方式：
  1. API 模式（--api，memos 运行中，推荐）：memos >= 0.30 用 --password 登录换取短期 token，
     < 0.30 用 --token（Access Token）；请求体带 createTime 保留 Bangumi 时间
  2. 直写数据库（--db，需先停止 memos）：直接插入 memo 表，保留时间

仅用 Python 标准库（urllib / tomllib / sqlite3），无需安装任何依赖；
Python 的 ssl 走系统 OpenSSL 根库、DNS 走 libc getaddrinfo，Termux 等环境开箱即用。

用法示例：
  python3 memos-plugin-bangumi.py --bangumi-username sai --api http://localhost:5230 --password '***'
  python3 memos-plugin-bangumi.py --config config.toml --dry-run
"""

import argparse
import datetime
import http.client
import json
import os
import socket
import sqlite3
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

try:
    import tomllib
except ImportError:
    sys.stderr.write("需要 Python >= 3.11（内置 tomllib）\n")
    raise

def default_ua():
    return "memos-plugin-bangumi (https://github.com/inchei/memos-plugin-bangumi)"


DEFAULT_BANGUMI_BASE = "https://api.bgm.tv"
DEFAULT_CONFIG_PATH = "config.toml"
DEFAULT_DB = "memos.db"
DEFAULT_USER = "admin"
DEFAULT_VISIBILITY = "private"
DEFAULT_STATE = "state.json"
DEFAULT_TIMEOUT = 30
DEFAULT_LIMIT = 100
DEFAULT_LINK_BASE = "https://bgm.tv"

VIS_PRIVATE = "PRIVATE"
VIS_PROTECTED = "PROTECTED"
VIS_PUBLIC = "PUBLIC"


def die(msg):
    print(msg, file=sys.stderr)
    sys.exit(1)


def truncate(s, n):
    if len(s) <= n:
        return s
    return s[:n] + "…"


DEFAULTS = {
    "bangumi_username": "",
    "bangumi_base": DEFAULT_BANGUMI_BASE,
    "bangumi_ip": "",
    "user_agent": "",
    "link_base": DEFAULT_LINK_BASE,
    "api": "",
    "token": "",
    "password": "",
    "db": DEFAULT_DB,
    "user": DEFAULT_USER,
    "visibility": DEFAULT_VISIBILITY,
    "tag": "",
    "subject_types": [],
    "dry_run": False,
    "full": False,
    "verbose": False,
    "delete": False,
    "state": DEFAULT_STATE,
    "timeout": DEFAULT_TIMEOUT,
    "limit": DEFAULT_LIMIT,
}


def find_config_path(args):
    for i, a in enumerate(args):
        if a in ("-config", "--config") and i + 1 < len(args):
            return args[i + 1]
        if a.startswith("-config="):
            return a[len("-config="):]
    return DEFAULT_CONFIG_PATH


def load_config(path):
    cfg = dict(DEFAULTS)
    try:
        with open(path, "rb") as f:
            data = tomllib.load(f)
    except FileNotFoundError:
        return cfg
    except OSError as e:
        die("读取配置文件 {} 失败：{}".format(path, e))
    except tomllib.TOMLDecodeError as e:
        die("解析配置文件 {} 失败：{}".format(path, e))
    if not isinstance(data, dict):
        die("解析配置文件 {} 失败：内容不是 TOML 表".format(path))
    for key, value in data.items():
        if key in cfg:
            cfg[key] = value
    return cfg


def parse_subject_types(s):
    out = []
    for part in s.split(","):
        part = part.strip()
        if not part:
            continue
        try:
            out.append(int(part))
        except ValueError:
            raise argparse.ArgumentTypeError("无效的条目类型 {!r}".format(part))
    return out


def build_parser(cfg):
    p = argparse.ArgumentParser(
        prog="memos-plugin-bangumi",
        description="把 Bangumi 用户「看过/玩过/读过/听过」且带短评的收藏导入 Memos。",
        epilog="配置文件键名 = 选项名去掉 --（- 可写作 _），如 bangumi_username。",
    )
    p.add_argument("--config", default=DEFAULT_CONFIG_PATH, help="配置文件路径（默认 config.toml，不存在则跳过）")
    p.add_argument("--bangumi-username", dest="bangumi_username", default=cfg["bangumi_username"], help="Bangumi 用户名或 UID")
    p.add_argument("--bangumi-base", dest="bangumi_base", default=cfg["bangumi_base"], help="Bangumi API 地址")
    p.add_argument("--bangumi-ip", dest="bangumi_ip", default=cfg["bangumi_ip"], help="Bangumi 直连 IP（绕过 DNS，TLS 域名不变；DNS 异常环境使用）")
    p.add_argument("--link-base", dest="link_base", default=cfg["link_base"], help="memo 正文的条目链接域名（如 https://fxbgm.tv）")
    p.add_argument("--user-agent", dest="user_agent", default=cfg["user_agent"], help="请求 Bangumi 使用的 User-Agent")
    p.add_argument("--api", default=cfg["api"], help="Memos API 地址（设置则用 API 模式）")
    p.add_argument("--token", default=cfg["token"], help="直接使用的 Bearer token（memos < 0.30 的 Access Token）")
    p.add_argument("--password", default=cfg["password"], help="Memos 密码（memos >= 0.30 用于登录换取 token）")
    p.add_argument("--db", default=cfg["db"], help="Memos sqlite 数据库路径（直写模式）")
    p.add_argument("--user", default=cfg["user"], help="Memos 用户名（直写模式必填；API 模式用于登录/过滤）")
    p.add_argument("--visibility", default=cfg["visibility"], help="memo 可见性：private/protected/public")
    p.add_argument("--tag", default=cfg["tag"], help="附加标签（API 模式拼 #tag 到正文，直写模式写入 payload）")
    p.add_argument("--subject-types", dest="subject_types", type=parse_subject_types, default=cfg["subject_types"],
                   help="要导入的条目类型，逗号分隔：1书籍/2动画/3音乐/4游戏/6三次元（空=全部）")
    p.add_argument("--dry-run", dest="dry_run", action="store_true", default=cfg["dry_run"], help="只预览不写入")
    p.add_argument("--full", action="store_true", default=cfg["full"], help="忽略状态文件，全量扫描")
    p.add_argument("--verbose", action="store_true", default=cfg["verbose"], help="逐条输出创建的 memo")
    p.add_argument("--delete", action="store_true", default=cfg["delete"], help="卸载已导入的 Bangumi memos（删除 uid 以 bgm- 开头的 memo）")
    p.add_argument("--state", default=cfg["state"], help="增量状态文件路径")
    p.add_argument("--timeout", type=int, default=cfg["timeout"], help="HTTP 超时秒数")
    p.add_argument("--limit", type=int, default=cfg["limit"], help="Bangumi 分页大小（上限 100）")
    return p


def status_label(subject_type):
    return {1: "读过", 3: "听过", 4: "玩过"}.get(subject_type, "看过")


def subject_name(c):
    s = c.get("subject", {})
    name_cn = (s.get("name_cn") or "").strip()
    return name_cn if name_cn else (s.get("name") or "")


def build_content(c, link_base):
    s = c.get("subject", {})
    return "{0}《{1}》：{2}\n\n{3}/subject/{4}".format(
        status_label(s.get("type", 0)), subject_name(c), c.get("comment", ""),
        link_base.rstrip("/"), s.get("id"))


def parse_bangumi_time(s):
    s = s.strip()
    for layout in ("%Y-%m-%dT%H:%M:%S%z", None):
        try:
            t = datetime.datetime.strptime(s, layout) if layout else datetime.datetime.fromisoformat(s.replace("Z", "+00:00"))
            return int(t.timestamp())
        except ValueError:
            continue
    raise ValueError("无法解析时间 {!r}".format(s))


def visibility_value(s):
    return {"protected": VIS_PROTECTED, "public": VIS_PUBLIC}.get(s, VIS_PRIVATE)


def load_state(path):
    if not path:
        return {"last_updated_ts": 0}
    try:
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
        if not isinstance(data, dict) or "last_updated_ts" not in data:
            return {"last_updated_ts": 0}
        return {"last_updated_ts": int(data.get("last_updated_ts", 0))}
    except FileNotFoundError:
        return {"last_updated_ts": 0}
    except (OSError, ValueError) as e:
        die("解析状态文件 {} 失败：{}".format(path, e))


def save_state(path, state):
    if not path:
        return
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(state, f, indent=2)
        f.write("\n")
    os.replace(tmp, path)


def fmt_ts(ts):
    return time.strftime("%Y-%m-%d %H:%M:%S %z", time.localtime(ts))


def http_req(url, *, method="GET", data=None, headers=None, timeout=DEFAULT_TIMEOUT, opener=None):
    req = urllib.request.Request(url, data=data, headers=headers or {}, method=method)
    opener = opener or urllib.request.build_opener()
    try:
        with opener.open(req, timeout=timeout) as resp:
            return resp.status, resp.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()
    except urllib.error.URLError as e:
        raise RuntimeError("请求 {} 失败：{}".format(url, e.reason))


class _IPHTTPSConnection(http.client.HTTPSConnection):
    def __init__(self, *args, ip_override=None, **kwargs):
        self._ip_override = ip_override
        super().__init__(*args, **kwargs)

    def connect(self):
        tunnel_host = getattr(self, "_tunnel_host", None)
        sock = socket.create_connection((self._ip_override, self.port), self.timeout, self.source_address)
        self.sock = sock
        if tunnel_host:
            self._tunnel()
        server_hostname = tunnel_host if tunnel_host else self.host
        self.sock = self._context.wrap_socket(self.sock, server_hostname=server_hostname)


class _IPHTTPSHandler(urllib.request.HTTPSHandler):
    def __init__(self, ip_override, host, context=None, check_hostname=None):
        self._ip_override = ip_override
        self._target_host = host
        super().__init__(context=context, check_hostname=check_hostname)

    def https_open(self, req):
        if urllib.parse.urlsplit(req.full_url).hostname == self._target_host:
            return self.do_open(_IPHTTPSConnection, req,
                                ip_override=self._ip_override, context=self._context)
        return self.do_open(http.client.HTTPSConnection, req, context=self._context)


def build_opener(base, ip_override, timeout):
    if ip_override:
        host = urllib.parse.urlsplit(base).hostname
        if host:
            return urllib.request.build_opener(_IPHTTPSHandler(ip_override, host))
    return urllib.request.build_opener()


def fetch_collections(base, username, ua, ip_override, limit, subject_types, timeout):
    if limit < 1 or limit > 100:
        limit = DEFAULT_LIMIT
    opener = build_opener(base, ip_override, timeout)
    types = subject_types if subject_types else [0]
    all_items = []
    for st in types:
        offset = 0
        while True:
            u = "{0}/v0/users/{1}/collections?type=2&limit={2}&offset={3}".format(
                base.rstrip("/"), urllib.parse.quote(username, safe=""), limit, offset)
            if st != 0:
                u += "&subject_type=" + str(st)
            code, body = http_req(u, headers={"User-Agent": ua, "Accept": "application/json"}, timeout=timeout, opener=opener)
            if code != 200:
                raise RuntimeError("bangumi API 返回 HTTP {0}：{1}".format(code, truncate(body.decode("utf-8", "replace"), 200)))
            try:
                page = json.loads(body.decode("utf-8"))
            except ValueError as e:
                raise RuntimeError("解析 Bangumi 响应失败：{}".format(e))
            data = page.get("data") or []
            all_items.extend(data)
            if len(data) < limit:
                break
            offset += limit
    return all_items


class APIWriter:
    def __init__(self, base, token, user):
        self.base = base.rstrip("/")
        self.token = token
        self.user = user
        self.timeout = DEFAULT_TIMEOUT

    def _request(self, method, path, query=None, payload=None):
        url = self.base + path
        if query:
            url += "?" + urllib.parse.urlencode(query)
        data = None
        headers = {"Authorization": "Bearer " + self.token}
        if payload is not None:
            data = json.dumps(payload).encode("utf-8")
            headers["Content-Type"] = "application/json"
        return http_req(url, method=method, data=data, headers=headers, timeout=self.timeout)

    def set_timeout(self, timeout):
        self.timeout = timeout

    def close(self):
        pass

    def list_existing_uids(self):
        uids = set()
        page_token = ""
        while True:
            query = {"pageSize": "1000"}
            if self.user:
                query["filter"] = 'creator == "{0}"'.format(self.user)
            if page_token:
                query["pageToken"] = page_token
            code, body = self._request("GET", "/api/v1/memos", query)
            if code != 200:
                raise RuntimeError("memos 列表请求失败：HTTP {}".format(code))
            out = json.loads(body.decode("utf-8"))
            for m in out.get("memos") or []:
                name = m.get("name") or ""
                if name.startswith("memos/"):
                    uids.add(name[len("memos/"):])
            page_token = out.get("nextPageToken") or ""
            if not page_token:
                break
        return uids

    def list_bangumi_owned(self):
        return sorted(u for u in self.list_existing_uids() if u.startswith("bgm-"))

    def create(self, uid, content, visibility, create_time, ts, tag):
        if tag:
            content += "\n#" + tag
        payload = {"content": content, "visibility": visibility}
        if create_time:
            payload["createTime"] = create_time
        code, body = self._request("POST", "/api/v1/memos", {"memoId": uid}, payload)
        if code == 200:
            return True
        api_code = None
        try:
            api_code = json.loads(body.decode("utf-8")).get("code")
        except ValueError:
            pass
        if api_code == 6:
            return False
        raise RuntimeError("创建 memo 失败：HTTP {0}：{1}".format(code, truncate(body.decode("utf-8", "replace"), 200)))

    def delete(self, uid):
        code, body = self._request("DELETE", "/api/v1/memos/" + urllib.parse.quote(uid, safe=""))
        if code in (200, 404):
            return
        raise RuntimeError("删除 memo 失败：HTTP {0}：{1}".format(code, truncate(body.decode("utf-8", "replace"), 200)))


def sign_in(base, username, password, timeout):
    url = base.rstrip("/") + "/api/v1/auth/signin"
    payload = json.dumps({"password_credentials": {"username": username, "password": password}}).encode("utf-8")
    code, body = http_req(url, method="POST", data=payload,
                          headers={"Content-Type": "application/json"}, timeout=timeout)
    if code != 200:
        raise RuntimeError("memos 登录失败（HTTP {0}）：{1}".format(code, truncate(body.decode("utf-8", "replace"), 200)))
    try:
        out = json.loads(body.decode("utf-8"))
    except ValueError as e:
        raise RuntimeError("解析 memos 登录响应失败：{}".format(e))
    token = out.get("accessToken") or out.get("token") or ""
    user = (out.get("user") or {}).get("name", "")
    if not token:
        raise RuntimeError("memos 登录未返回 access token")
    writer = APIWriter(base, token, user)
    writer.set_timeout(timeout)
    return writer


class DBWriter:
    def __init__(self, path, username):
        self.conn = sqlite3.connect(path)
        self.conn.execute("PRAGMA busy_timeout = 3000")
        try:
            row = self.conn.execute("SELECT id FROM user WHERE username = ?", (username,)).fetchone()
        except sqlite3.Error as e:
            self.conn.close()
            raise RuntimeError("查询 memos 用户失败：{}".format(e))
        if row is None:
            self.conn.close()
            raise RuntimeError("memos 数据库中没有用户 {!r}，请检查 user 配置".format(username))
        self.user_id = row[0]

    def list_existing_uids(self):
        rows = self.conn.execute("SELECT uid FROM memo").fetchall()
        return set(r[0] for r in rows if r[0])

    def list_bangumi_owned(self):
        rows = self.conn.execute(
            "SELECT uid FROM memo WHERE creator_id = ? AND uid LIKE 'bgm-%' ORDER BY uid",
            (self.user_id,)).fetchall()
        return [r[0] for r in rows]

    def create(self, uid, content, visibility, create_time, ts, tag):
        exists = self.conn.execute("SELECT COUNT(1) FROM memo WHERE uid = ?", (uid,)).fetchone()[0]
        if exists > 0:
            return False
        payload = {}
        if tag:
            payload["tags"] = [tag]
        self.conn.execute(
            "INSERT INTO memo (uid, creator_id, created_ts, updated_ts, row_status, content, visibility, pinned, payload)"
            " VALUES (?, ?, ?, ?, 'NORMAL', ?, ?, 0, ?)",
            (uid, self.user_id, ts, ts, content, visibility, json.dumps(payload)))
        self.conn.commit()
        return True

    def delete(self, uid):
        self.conn.execute("DELETE FROM memo WHERE uid = ? AND creator_id = ?", (uid, self.user_id))
        self.conn.commit()

    def close(self):
        self.conn.close()


def open_writer(cfg, timeout):
    if cfg["api"]:
        if cfg["token"]:
            writer = APIWriter(cfg["api"].rstrip("/"), cfg["token"], cfg["user"])
        elif cfg["password"] and cfg["user"]:
            writer = sign_in(cfg["api"], cfg["user"], cfg["password"], timeout)
        else:
            raise RuntimeError("API 模式需要 --token，或 --user 与 --password（memos >= 0.30）")
        writer.set_timeout(timeout)
        existing = writer.list_existing_uids()
        return writer, existing
    writer = DBWriter(cfg["db"], cfg["user"])
    try:
        existing = writer.list_existing_uids()
    except Exception:
        writer.close()
        raise
    return writer, existing


def sync(cfg):
    if not (cfg.get("bangumi_username") or "").strip():
        die("缺少 bangumi-username，请通过命令行参数或配置文件指定")
    if not cfg.get("bangumi_base"):
        cfg["bangumi_base"] = DEFAULT_BANGUMI_BASE
    if not cfg.get("user_agent"):
        cfg["user_agent"] = default_ua()
    if cfg.get("limit", 0) < 1 or cfg.get("limit", 0) > 100:
        cfg["limit"] = DEFAULT_LIMIT
    timeout = cfg.get("timeout") or DEFAULT_TIMEOUT
    api_mode = bool(cfg.get("api"))
    if not api_mode and not cfg.get("db"):
        cfg["db"] = DEFAULT_DB
    link_base = cfg.get("link_base") or DEFAULT_LINK_BASE

    print("正在从 Bangumi 拉取用户 {!r} 的收藏…".format(cfg["bangumi_username"]))
    try:
        collections = fetch_collections(
            cfg["bangumi_base"], cfg["bangumi_username"], cfg["user_agent"],
            cfg.get("bangumi_ip", ""), cfg["limit"], cfg.get("subject_types") or [],
            timeout)
    except Exception as e:
        die(str(e))

    state = load_state(cfg.get("state") or "")
    incremental = (not cfg.get("full")) and state["last_updated_ts"] > 0
    if incremental:
        print("增量模式：跳过 updated_at <= {} 的旧条目（--full 强制全量）".format(fmt_ts(state["last_updated_ts"])))
    else:
        print("全量模式：首次运行或无有效状态，扫描全部收藏")

    writer = None
    existing = set()
    if not cfg.get("dry_run"):
        writer, existing = open_writer(cfg, timeout)
        close_after = True
    else:
        close_after = False

    created = skipped = total = 0
    max_ts = 0
    try:
        for c in collections:
            total += 1
            try:
                ts = parse_bangumi_time(c.get("updated_at") or "")
            except ValueError as e:
                print("  跳过 {}：时间解析失败（{}）".format(subject_name(c), e))
                continue
            if ts > max_ts:
                max_ts = ts
            if incremental and ts <= state["last_updated_ts"]:
                break
            if not (c.get("comment") or "").strip():
                continue
            sid = (c.get("subject") or {}).get("id", 0)
            if not sid or sid <= 0:
                print("  跳过条目 id 无效的收藏（name={}）".format(subject_name(c)))
                continue
            uid = "bgm-{}".format(sid)
            content = build_content(c, link_base)
            if cfg.get("dry_run"):
                print("  [dry-run] {}\n{}\n".format(uid, content))
                created += 1
                continue
            if uid in existing:
                skipped += 1
                continue
            try:
                ok = writer.create(uid, content, visibility_value(cfg.get("visibility")),
                                   c.get("updated_at") or "", ts, cfg.get("tag") or "")
            except Exception as e:
                print("  创建 {} 失败：{}".format(uid, e))
                continue
            if ok:
                if cfg.get("verbose"):
                    print("  已创建 {}：{}《{}》".format(uid, status_label((c.get("subject") or {}).get("type", 0)), subject_name(c)))
                created += 1
            else:
                skipped += 1
    finally:
        if close_after:
            writer.close()

    if not cfg.get("dry_run") and max_ts > 0:
        state["last_updated_ts"] = max_ts
        try:
            save_state(cfg.get("state") or "", state)
        except OSError as e:
            die("保存状态文件失败：{}".format(e))

    action = "dry-run 待创建" if cfg.get("dry_run") else "创建"
    print("\n完成：扫描 {} 条收藏，{} {} 条，跳过 {} 条".format(total, action, created, skipped))
    return 0


def uninstall(cfg):
    if not cfg.get("api") and not cfg.get("db"):
        cfg["db"] = DEFAULT_DB
    timeout = cfg.get("timeout") or DEFAULT_TIMEOUT
    writer, _ = open_writer(cfg, timeout)
    try:
        uids = writer.list_bangumi_owned()
        if not uids:
            print("未找到已导入的 Bangumi memo（uid 以 bgm- 开头）")
        elif cfg.get("dry_run"):
            for uid in uids:
                print("  [dry-run] 将删除 {}".format(uid))
            print("\n完成：dry-run 待删除 {} 条".format(len(uids)))
        else:
            deleted = 0
            for uid in uids:
                try:
                    writer.delete(uid)
                except Exception as e:
                    print("  删除 {} 失败：{}".format(uid, e))
                    continue
                if cfg.get("verbose"):
                    print("  已删除 {}".format(uid))
                deleted += 1
            print("\n完成：删除 {} 条 Bangumi memo，失败 {} 条".format(deleted, len(uids) - deleted))
    finally:
        writer.close()

    if not cfg.get("dry_run"):
        try:
            os.remove(cfg.get("state") or "")
            print("已重置增量状态文件 {}（下次同步为全量）".format(cfg.get("state")))
        except OSError:
            pass
    return 0


def main(argv):
    if hasattr(sys.stdout, "reconfigure"):
        sys.stdout.reconfigure(line_buffering=True)
        sys.stderr.reconfigure(line_buffering=True)
    config_path = find_config_path(argv)
    cfg = load_config(config_path)
    args = build_parser(cfg).parse_args(argv)
    cfg.update({k: v for k, v in vars(args).items() if k in cfg})
    if cfg.get("delete"):
        return uninstall(cfg)
    return sync(cfg)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
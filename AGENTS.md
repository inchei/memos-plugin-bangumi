# agents.md 开发指引

本文件供 AI 助手 / 开发者在 memos-plugin-bangumi 仓库工作时参考。

## 项目概述

把 Bangumi 用户「看过/玩过/读过/听过」且带短评（`comment`）的收藏导入 Memos，纯文字 memo。
支持两种写入模式：

- **API 模式**（`--api`，memos 运行中，推荐）：memos ≥ 0.30 用 `--user`+`--password`
  调用 `/api/v1/auth/signin` 换取短期 token；memos < 0.30 用 `--token`（Access Token）。
  `POST /api/v1/memos?memoId={uid}` 设置 uid 且幂等；请求体 `createTime` 保留 Bangumi 时间。
- **直写数据库**（`--db`）：直接插入 memos sqlite `memo` 表（需先停止 memos）。

单文件、纯 Python 标准库实现（`urllib` / `tomllib` / `sqlite3`），无第三方依赖，
Python ≥ 3.11（`tomllib` 3.11 才进入标准库）。不需要任何工具链或构建步骤。

## 目录结构

- `memos-plugin-bangumi.py`  全部代码（含文件头 GPL 版权声明）
- `config.example.toml`      配置模板
- `.github/workflows/sync.yml` 每 6 小时在 GitHub runner 上跑一次 API 模式同步
  （需 memos 公网可达；凭据走 Secrets；`state.json` 走 Actions cache 不进仓库，支持
  workflow_dispatch 传 `watermark` 播种初始水印 / `full` 强制全量；未配置 secrets 则安全跳过）
- `logo.png`   README 顶部展示的仓库 logo
- `README.md` / `AGENTS.md` / `CONTRIBUTING.md` / `LICENSE` / `.gitignore`

## 技术约束（务必遵守）

- 只用 Python 标准库，禁止新增依赖（pip 包）——这是本项目相对早期 Go 版、以及能零配置跑在
  Termux 等环境的全部意义
- 配置键名 = 命令行参数去 `--`（`-` 可写作 `_`）；命令行参数优先于配置文件（argparse 默认值来自配置）
- 面向用户的输出与参数说明用中文；代码不加注释，只保留 docstring / 函数签名注释
- 修改源码时勿删文件头的版权/SPDX 行
- 保持单文件：不要拆成多文件；无 `--version`（已移除，版本无发布语义）
- 无 git hooks / CI 门禁：改完靠下面的「验证」手工自检

## 关键逻辑（修改时保持）

- `fetch_collections`：`GET /v0/users/{username}/collections?type=2&limit&offset`，`limit` 上限 100；
  `subject_types` 为空时一次拉全（不传 `subject_type`），否则逐个类型拉取；需带规范 User-Agent。
  `ip_override`（`--bangumi-ip`/`bangumi_ip`）非空时用自定义 `_IPHTTPSConnection`/`_IPHTTPSHandler`
  直连该 IP：仅当目标 host 等于 bangumi_base 的 hostname 才替换地址，域名/SNI/TLS 校验保持不变。
  这是兜底选项——Python 走 libc `getaddrinfo` 与系统 OpenSSL，Termux 等环境通常无需它
- `status_label`：按 `subject.type` 给完成态文案：1=读过、2/6=看过、3=听过、4=玩过
- `build_content`：`看过《name_cn》：短评\n\n{link_base}/subject/{id}`（name_cn 为空用 name；
  link_base 默认 `https://bgm.tv`，可用配置 `link_base` / `--link-base` 换成 fxbgm.tv 等镜像）
- `parse_bangumi_time`：ISO8601（+08:00，含 `fromisoformat` 容错）转 epoch 秒
- 过滤：`comment` 空白则跳过；`uid = bgm-{subject_id}` 幂等（uid 已存在则跳过）
- 输出：默认只打印进度与汇总，`--verbose` 才逐条输出创建的 memo；`--dry-run` 始终打印预览内容
- 卸载：`--delete` 删除 uid 以 `bgm-` 开头的 memo 并重置状态文件（不访问 Bangumi，只需 memos 连接参数；
  API 模式 `DELETE /api/v1/memos/{uid}`，404 视为成功；直写库按 `uid + creator_id` 删除）
- 增量：`state.json` 记 `last_updated_ts`（epoch 秒）；列表按 updated_at 降序，
  遇到 `ts <= 水印` 即 `break`（后续更旧）；`--full` 或首次运行（水印为 0）走全量；
  `--dry-run` 不读/写 memos，也不保存状态；GitHub Actions 模式的 `state.json` 走 cache 不进仓库，
  首次可用 sync.yml 的 `watermark` 输入播种，此后每次跑完自动存回
- API 模式幂等：`memoId` 重复时 memos 返回 `code=6`（ALREADY_EXISTS）视为跳过
- API 模式 `tag` 以 `#tag` 拼入正文（memos 标签从正文 hashtag 提取）；直写库写 `payload.tags`
- 直写库要求库已由 memos 初始化（有 `user` 表且存在用户），导入前 memos 必须停止

## 验证

```sh
# 语法检查
python3 -m py_compile memos-plugin-bangumi.py

# 帮助
python3 memos-plugin-bangumi.py --help

# dry-run 预览（不写入 memos，不保存状态）
python3 memos-plugin-bangumi.py --bangumi-username sai --dry-run

# API 模式端到端：本地起一个测试 memos（--data 临时目录、--port 5230）并建用户，
# 真实导入一次，再次运行确认幂等跳过，并抽查 memo 的 uid / createTime / content。
# 直写库模式：memos 停止后用 --db 指向测试库导入，重启后用 API 抽查。
```

改动后必须跑通一次 dry-run 和一次真实导入（含重复运行幂等检查）。
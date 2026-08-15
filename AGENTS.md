# agents.md 开发指引

本文件供 AI 助手 / 开发者在 memos-plugin-bangumi 仓库工作时参考。

## 项目概述

把 Bangumi 用户「看过/玩过/读过/听过」且带短评（`comment`）的收藏导入 Memos，纯文字 memo。
支持两种写入模式：

- **API 模式**（`--api`，memos 运行中，推荐）：memos ≥ 0.30 用 `--user`+`--password`
  调用 `/api/v1/auth/signin` 换取短期 token；memos < 0.30 用 `--token`（Access Token）。
  `POST /api/v1/memos?memoId={uid}` 设置 uid 且幂等；请求体 `createTime` 保留 Bangumi 时间。
- **直写数据库**（`--db`）：直接插入 memos sqlite `memo` 表（需先停止 memos）。

## 目录结构

- `main.go`       入口、参数/配置合并、同步主流程（文件头含 GPL 版权声明）
- `config.go`     TOML 配置结构与加载（`config.toml` 默认读当前目录，不存在则跳过）
- `bangumi.go`    Bangumi 收藏拉取、分页、updated_at 解析、文案映射、正文生成
- `state.go`      增量状态文件（水印 last_updated_ts）
- `memos_api.go`  API 模式实现（signin / 列 uid / 创建 memo）
- `memos_db.go`   直写 sqlite 实现（modernc.org/sqlite，纯 Go 无 cgo）
- `README.md` / `AGENTS.md` / `LICENSE` / `config.example.toml` / `.gitignore`

## 技术约束（务必遵守）

- Go 标准库 + 两个依赖：`github.com/pelletier/go-toml/v2`（配置）、`modernc.org/sqlite`（直写库）
- 配置键名 = 命令行参数去 `--`（`-` 可写作 `_`）；命令行参数优先于配置文件（flag 默认值即来自配置文件）
- 面向用户的输出与参数说明用中文；代码不加注释，只保留 docstring/函数签名注释
- 修改源码时勿删文件头的版权/SPDX 行
- 构建产物应为单个静态二进制：`go build -o memos-plugin-bangumi .`

## 关键逻辑（修改时保持）

- `fetchCollections`：`GET /v0/users/{username}/collections?type=2&limit&offset`，`limit` 上限 100；
  `subject_types` 为空时一次拉全（不传 `subject_type`），否则逐个类型拉取；需带规范 User-Agent
- `statusLabel`：按 `subject.type` 给完成态文案：1=读过、2/6=看过、3=听过、4=玩过
- `buildContent`：`看过《name_cn》：短评\n\n{link_base}/subject/{id}`（name_cn 为空用 name；
  link_base 默认 `https://bgm.tv`，可用配置 `link_base` / `--link-base` 换成 fxbgm.tv 等镜像）
- `parseBangumiTime`：布局 `2006-01-02T15:04:05-07:00`（+08:00），转 epoch 秒
- 过滤：`comment` 空白则跳过；`uid = bgm-{subject_id}` 幂等（uid 已存在则跳过）
- 输出：默认只打印进度与汇总，`--verbose` 才逐条输出创建的 memo；`--dry-run` 始终打印预览内容
- 增量：`state.json` 记 `last_updated_ts`（epoch 秒）；列表按 updated_at 降序，
  遇到 `ts <= 水印` 即 `break`（后续更旧）；`--full` 或首次运行（水印为 0）走全量；
  `--dry-run` 不读/写 memos，也不保存状态
- API 模式幂等：`memoId` 重复时 memos 返回 `code=6`（ALREADY_EXISTS）视为跳过
- API 模式 `tag` 以 `#tag` 拼入正文（memos 标签从正文 hashtag 提取）；直写库写 `payload.tags`
- 直写库要求库已由 memos 初始化（有 `user` 表且存在用户），导入前 memos 必须停止

## 验证

```sh
# 语法/构建（依赖安装：go mod tidy 会自动解析 go.mod 中的 import）
go mod tidy && go build -o memos-plugin-bangumi .

# 帮助
./memos-plugin-bangumi --help

# dry-run 预览（不写入 memos，不保存状态）
./memos-plugin-bangumi --bangumi-username sai --dry-run

# API 模式端到端：先在本地起一个测试 memos（--data 临时目录）并建用户，
# 真实导入一次，再次运行确认幂等跳过，并抽查 memo 的 uid / createTime / content。
# 直写库模式：memos 停止后用 --db 指向测试库导入，重启后用 API 抽查。
```

改动后必须跑通一次 dry-run 和一次真实导入（含重复运行幂等检查）。

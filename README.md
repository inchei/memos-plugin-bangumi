# memos-plugin-bangumi

![Go](https://img.shields.io/badge/Go-1.26-blue)
[![Memos](https://img.shields.io/badge/Memos-%E2%89%A50.26.0-1E6D51)](https://github.com/usememos/memos)

把 Bangumi 用户「看过 / 玩过 / 读过 / 听过」且**带短评**的收藏导入 [Memos](https://github.com/usememos/memos)，
memo 为纯文字，正文含条目名、完成态文案、短评和 Bangumi 链接。

## 版本要求

| 依赖 | 版本 |
| --- | --- |
| Go | ≥ 1.26 |
| Bangumi | 无需 |
| Memos | API 模式 ≥ 0.26.0；直写库 ≥ 0.22 |

## 原理

- 调 `GET /v0/users/{username}/collections?type=2` 拉取完成态收藏，`type=2` 对应不同条目类型的文案：
  书籍=读过、动画/三次元=看过、音乐=听过、游戏=玩过
- 仅导入短评（`comment`）非空的条目，无文字则跳过
- memo 正文：`看过《阿基拉》：东京，燃烧；……` + 条目链接（默认 `https://bgm.tv/subject/5118`；
  链接域名可用 `--link-base` 或配置 `link_base` 换成其它镜像，如 `https://fxbgm.tv`）
- 每条 memo 以 `uid = bgm-{subject_id}` 幂等，重复运行不产生重复 memo
- 时间用收藏的 `updated_at`（+08:00）写入 `createTime`/`created_ts`，保留原始时间
- **增量同步**：列表按 `updated_at` 降序返回，状态文件记录最新 `updated_at`，下次运行提前停止翻页；
  `--full` 强制全量

## 使用

### 方式一：API 模式（推荐，memos 运行中）

```sh
# memos >= 0.30（登录换取短期 token）
memos-plugin-bangumi --bangumi-username sai \
    --api http://localhost:5230 --user admin --password '你的密码'

# memos < 0.30（使用账号里的 Access Token）
memos-plugin-bangumi --bangumi-username sai \
    --api http://localhost:5230 --token 'AccessToken'
```

### 方式二：直写数据库（需先停止 memos）

```sh
memos-plugin-bangumi --bangumi-username sai --db ~/.memos/memos.db --user admin
```

### 预览（不写入）

```sh
memos-plugin-bangumi --bangumi-username sai --dry-run
```

默认只输出进度与汇总；`--verbose` 可逐条显示创建的 memo（`--dry-run` 始终打印预览内容）。

### cron 定时增量同步

```sh
# 每 30 分钟同步一次
*/30 * * * * cd /path/to/memos-plugin-bangumi && ./memos-plugin-bangumi --config config.toml >> sync.log 2>&1
```

## 配置文件

默认读取当前目录 `config.toml`（不存在则跳过），也可用 `--config` 指定其它路径；
命令行参数会覆盖配置文件中的同名项。复制 `config.example.toml` 为 `config.toml` 即可。
全部参数见 `memos-plugin-bangumi --help`。

```toml
bangumi_username = "sai"
api = "http://localhost:5230"
password = "你的密码"
visibility = "private"
tag = "bangumi"
subject_types = []   # 空=全部；如 [2, 4] 只导入动画与游戏
```

## 说明与限制

- 短评需为**公开收藏**（API 无鉴权时读不到私有收藏）
- Bangumi 存在 bug：修改评分/短评可能不更新 `updated_at`，此类「旧条目补短评」增量会漏，
  可定期用 `--full` 补扫
- 直写数据库前请停止 memos，否则可能 `database is locked`
- 需设置规范的 User-Agent（默认值见 `config.example.toml`，可覆盖）

## 许可证

[GNU General Public License v3.0 or later](LICENSE)（GPL-3.0-or-later）

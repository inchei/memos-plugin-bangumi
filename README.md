<p align="center">
  <img src="logo.png" width="120" alt="memos-plugin-bangumi">
</p>

# memos-plugin-bangumi

![Python](https://img.shields.io/badge/Python-%E2%89%A53.11-3776AB)
[![Memos](https://img.shields.io/badge/Memos-%E2%89%A50.26.0-1E6D51)](https://github.com/usememos/memos)

把 Bangumi 用户「看过 / 玩过 / 读过 / 听过」且**带短评**的收藏导入 [Memos](https://github.com/usememos/memos)，
memo 为纯文字，正文含条目名、完成态文案、短评和 Bangumi 链接。

## 版本要求

| 依赖 | 版本 |
| --- | --- |
| Python | ≥ 3.11 |
| Memos | API 模式 ≥ 0.26.0；直写库 ≥ 0.22 |

## 原理

- 调 `GET /v0/users/{username}/collections?type={type}` 拉取完成态收藏
- 仅导入短评（`comment`）非空的条目，无文字则跳过
- memo 正文：`看过《阿基拉》：东京，燃烧；……` + 条目链接（默认 `https://bgm.tv/subject/5118`；
  链接域名可用 `--link-base` 换成其它镜像）
- 每条 memo 以 `uid = bgm-{subject_id}` 幂等，重复运行不产生重复 memo
- 时间用收藏的 `updated_at`（+08:00）写入 `createTime`/`created_ts`，保留原始时间
- 列表按 `updated_at` 降序返回，状态文件记录最新 `updated_at`，下次运行提前停止处理更旧条目；
  `--full` 强制全量

## 使用方式

### 方式一：API 模式

memos >= 0.30（登录换取短期 token）：

```sh
python3 memos-plugin-bangumi.py --bangumi-username sai \
    --api http://localhost:5230 --user admin --password '你的密码'
```

 0.26.0 ≤ memos < 0.30（使用账号里的 Access Token）：

```sh
python3 memos-plugin-bangumi.py --bangumi-username sai \
    --api http://localhost:5230 --token 'AccessToken'
```

### 方式二：直写数据库

需停止当前 memos，写完后重启。

```sh
python3 memos-plugin-bangumi.py --bangumi-username sai --db ~/.memos/memos.db --user admin
```

## 同步

### cron

```sh
# 每 30 分钟同步一次
*/30 * * * * cd /path/to/memos-plugin-bangumi && python3 memos-plugin-bangumi.py --config config.toml >> sync.log 2>&1
```

### GitHub Actions

前提：memos 实例可从公网访问。

fork 本仓库，参考 [sync.yml](.github/workflows/sync.yml) 每 6 小时在 GitHub runner 上自动跑一次 API 模式同步。

配置仓库 Secrets（Settings → Secrets and variables → Actions）：

| Secret | 说明 |
| --- | --- |
| `BANGUMI_USERNAME` | Bangumi 用户名（必填） |
| `MEMOS_API` | memos 地址，如 `https://memos.example.com`（必填） |
| `MEMOS_PASSWORD` | memos 密码（memos ≥ 0.30，推荐） |
| `MEMOS_USER` | memos 登录用户名（配合密码） |
| `MEMOS_TOKEN` | 或 memos < 0.30 的 Access Token（替代密码） |

增量状态（`state.json`）通过 Actions cache 在两次运行间保留；未配置 secrets 时该任务会安全跳过。

## 卸载

删除 uid 以 `bgm-` 开头（即本工具导入）的 memo，并重置增量状态文件，增加 `--delete` 参数即可，例：

```sh
python3 memos-plugin-bangumi.py --delete --api http://localhost:5230 --user admin --password '你的密码'
```

## 配置文件

默认读取当前目录 `config.toml`，也可用 `--config` 指定其它路径；

命令行参数会覆盖配置文件。参考 `config.example.toml`。

## 说明与限制

- 短评需为**公开收藏**（API 无鉴权时读不到私有收藏）
- Bangumi 存在 bug：修改评分/短评可能不更新 `updated_at`，此类「旧条目补短评」增量会漏，
  可定期用 `--full` 补扫
- 直写数据库前请停止 memos，否则可能 `database is locked`
- 需设置规范的 User-Agent（默认值见 `config.example.toml`，可覆盖）

## 许可证

[GNU General Public License v3.0 or later](LICENSE)（GPL-3.0-or-later）

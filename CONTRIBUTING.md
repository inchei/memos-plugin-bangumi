# 贡献指南

欢迎为 memos-plugin-bangumi 贡献代码、文档与 issue。项目以 [GPL-3.0-or-later](LICENSE) 许可发布。

## 开发环境

| 工具 | 版本 |
| --- | --- |
| Go | ≥ 1.26 |
| golangci-lint | 2.x（`lint` / 钩子使用） |

## 常用命令

```sh
go mod tidy                      # 解析依赖（勿手动 go get，见 AGENTS.md）
go build -o memos-plugin-bangumi .  # 构建单二进制
gofmt -w $(find . -name '*.go' -not -path './vendor/*')   # 格式化源码
gofmt -l $(find . -name '*.go' -not -path './vendor/*')   # 检查是否已格式化
go vet ./...                     # go vet
golangci-lint run                # golangci-lint（需 2.x）
go test ./...                    # 运行测试
git config core.hooksPath .githooks   # 安装 git pre-commit 钩子（一次性）
```

## 代码约定

- 遵循 [AGENTS.md](AGENTS.md)：目录结构、`memoWriter` 两种写入模式、配置键名规则、
  增量水印逻辑、GPL 版权头等
- 面向用户的输出与参数说明用中文
- 代码不加注释，只保留 docstring / 函数签名注释
- 提交前必须通过：`gofmt`、`go vet`、`go build`、`golangci-lint`，并跑通一次 dry-run 与一次真实导入

## 提交流程

1. 从 `main` 新建分支：`git checkout -b fix/short-description`
2. 修改并自检（`gofmt`、`go vet ./...`、`golangci-lint run`、`go build ./...`）
3. 提交（pre-commit 钩子会自动重跑上述检查；可用 `SKIP=lint git commit` 跳过某步）
4. 推送并创建 Pull Request，描述改动动机与验证结果

## Issue 报告

请包含：运行环境（Go / memos 版本）、复现步骤、`config.toml`（隐去密码与 token）、
以及实际输出与预期差异。

## 许可证

新增文件请保留文件头的 `Copyright (C) 2026 memos-plugin-bangumi contributors` 与
`SPDX-License-Identifier: GPL-3.0-or-later` 两行。整个项目以 GPL-3.0-or-later 发布。

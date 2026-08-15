// Copyright (C) 2026 memos-plugin-bangumi contributors
// SPDX-License-Identifier: GPL-3.0-or-later
/* memos-plugin-bangumi - 将 Bangumi 用户「看过/玩过/读过/听过」且带短评的收藏导入 Memos。

纯文本 memo，正文含条目名、完成态文案、短评与 Bangumi 链接。
按 memo uid（bgm-{subject_id}）幂等，重复运行不产生重复 memo；
默认增量同步（状态文件记录最新 updated_at，提前停止翻页），--full 强制全量。

两种写入方式：
  1. API 模式（--api，memos 运行中，推荐）：memos >= 0.30 用 --password 登录，
     < 0.30 用 --token（Access Token）；请求体带 createTime 保留 Bangumi 时间
  2. 直写数据库（--db，需先停止 memos）：直接插入 memo 表，保留时间

用法示例：
  memos-plugin-bangumi --bangumi-username sai --api http://localhost:5230 --password '***'
  memos-plugin-bangumi --config config.toml --dry-run
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

var version = "0.1.0"

// intListFlag 支持逗号分隔的整数列表（如 --subject-types 2,4）。
type intListFlag []int

func (l *intListFlag) String() string {
	parts := make([]string, 0, len(*l))
	for _, v := range *l {
		parts = append(parts, strconv.Itoa(v))
	}
	return strings.Join(parts, ",")
}

func (l *intListFlag) Set(s string) error {
	if s == "" {
		*l = nil
		return nil
	}
	var out []int
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.Atoi(p)
		if err != nil {
			return fmt.Errorf("无效的条目类型 %q", p)
		}
		out = append(out, v)
	}
	*l = out
	return nil
}

// memoWriter 抽象 API 与直写库两种写入方式。
type memoWriter interface {
	listExistingUIDs(context.Context) (map[string]bool, error)
	listBangumiOwned(context.Context) ([]string, error)
	create(ctx context.Context, uid, content, visibility, createTime string, ts int64, tag string) (bool, error)
	delete(ctx context.Context, uid string) error
	close()
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	configPath := findConfigPath(args)

	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fs := flag.NewFlagSet("memos-plugin-bangumi", flag.ContinueOnError)
	var showVersion bool
	fs.StringVar(&configPath, "config", configPath, "配置文件路径（默认 config.toml，不存在则跳过）")
	fs.StringVar(&cfg.BangumiUsername, "bangumi-username", cfg.BangumiUsername, "Bangumi 用户名或 UID")
	fs.StringVar(&cfg.BangumiBase, "bangumi-base", cfg.BangumiBase, "Bangumi API 地址")
	fs.StringVar(&cfg.LinkBase, "link-base", cfg.LinkBase, "memo 正文的条目链接域名（如 https://fxbgm.tv）")
	fs.StringVar(&cfg.UserAgent, "user-agent", cfg.UserAgent, "请求 Bangumi 使用的 User-Agent")
	fs.StringVar(&cfg.API, "api", cfg.API, "Memos API 地址（设置则用 API 模式）")
	fs.StringVar(&cfg.Token, "token", cfg.Token, "直接使用的 Bearer token（memos < 0.30 的 Access Token）")
	fs.StringVar(&cfg.Password, "password", cfg.Password, "Memos 密码（memos >= 0.30 用于登录换取 token）")
	fs.StringVar(&cfg.DB, "db", cfg.DB, "Memos sqlite 数据库路径（直写模式）")
	fs.StringVar(&cfg.User, "user", cfg.User, "Memos 用户名（直写模式必填；API 模式用于登录/过滤）")
	fs.StringVar(&cfg.Visibility, "visibility", cfg.Visibility, "memo 可见性：private/protected/public")
	fs.StringVar(&cfg.Tag, "tag", cfg.Tag, "附加标签（API 模式拼 #tag 到正文，直写模式写入 payload）")
	subjectTypes := intListFlag(cfg.SubjectTypes)
	fs.Var(&subjectTypes, "subject-types", "要导入的条目类型，逗号分隔：1书籍/2动画/3音乐/4游戏/6三次元（空=全部）")
	fs.BoolVar(&cfg.DryRun, "dry-run", cfg.DryRun, "只预览不写入")
	fs.BoolVar(&cfg.Full, "full", cfg.Full, "忽略状态文件，全量扫描")
	fs.BoolVar(&cfg.Verbose, "verbose", cfg.Verbose, "逐条输出创建的 memo")
	fs.BoolVar(&cfg.Delete, "delete", cfg.Delete, "卸载已导入的 Bangumi memos（删除 uid 以 bgm- 开头的 memo）")
	fs.StringVar(&cfg.State, "state", cfg.State, "增量状态文件路径")
	fs.IntVar(&cfg.Timeout, "timeout", cfg.Timeout, "HTTP 超时秒数")
	fs.IntVar(&cfg.Limit, "limit", cfg.Limit, "Bangumi 分页大小（上限 100）")
	fs.BoolVar(&showVersion, "version", false, "显示版本")
	fs.Usage = func() { printUsage(fs) }
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if showVersion {
		fmt.Printf("memos-plugin-bangumi %s\n", version)
		return 0
	}
	cfg.SubjectTypes = []int(subjectTypes)

	if cfg.Delete {
		return uninstall(cfg)
	}
	return sync(cfg)
}

func findConfigPath(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-config" || a == "--config" {
			if i+1 < len(args) {
				return args[i+1]
			}
		} else if strings.HasPrefix(a, "-config=") {
			return strings.TrimPrefix(a, "-config=")
		}
	}
	return defaultConfigPath
}

func sync(cfg *Config) int {
	ctx := context.Background()

	if strings.TrimSpace(cfg.BangumiUsername) == "" {
		fmt.Fprintln(os.Stderr, "缺少 bangumi-username，请通过命令行参数或配置文件指定")
		return 1
	}
	if cfg.BangumiBase == "" {
		cfg.BangumiBase = defaultBangumiBase
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = defaultUserAgent
	}
	if cfg.Limit < 1 || cfg.Limit > 100 {
		cfg.Limit = defaultLimit
	}
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = time.Duration(defaultTimeout) * time.Second
	}
	apiMode := cfg.API != ""
	if !apiMode && cfg.DB == "" {
		cfg.DB = defaultDB
	}

	fmt.Printf("正在从 Bangumi 拉取用户 %q 的收藏…\n", cfg.BangumiUsername)
	collections, err := fetchCollections(ctx, cfg.BangumiBase, cfg.BangumiUsername, cfg.UserAgent, cfg.Limit, cfg.SubjectTypes, timeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	state, err := loadState(cfg.State)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	incremental := !cfg.Full && state.LastUpdatedTS > 0
	if incremental {
		fmt.Printf("增量模式：跳过 updated_at <= %s 的旧条目（--full 强制全量）\n",
			time.Unix(state.LastUpdatedTS, 0).Format("2006-01-02 15:04:05 -0700"))
	} else {
		fmt.Println("全量模式：首次运行或无有效状态，扫描全部收藏")
	}

	var writer memoWriter
	var existing map[string]bool
	if !cfg.DryRun {
		writer, existing, err = openWriter(cfg, timeout)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer writer.close()
	}

	var created, skipped, total int
	var maxTS int64
	for _, c := range collections {
		total++
		ts, err := parseBangumiTime(c.UpdatedAt)
		if err != nil {
			fmt.Printf("  跳过 %s：时间解析失败（%v）\n", subjectName(c), err)
			continue
		}
		if ts > maxTS {
			maxTS = ts
		}
		if incremental && ts <= state.LastUpdatedTS {
			break // 列表按 updated_at 降序，后续均更旧
		}
		if strings.TrimSpace(c.Comment) == "" {
			continue // 仅有文字：无短评的收藏跳过
		}
		if c.SubjectID <= 0 {
			fmt.Printf("  跳过条目 id 无效的收藏（name=%s）\n", subjectName(c))
			continue
		}
		uid := fmt.Sprintf("bgm-%d", c.SubjectID)
		content := buildContent(c, cfg.LinkBase)
		if cfg.DryRun {
			fmt.Printf("  [dry-run] %s\n%s\n\n", uid, content)
			created++
			continue
		}
		if existing[uid] {
			skipped++
			continue
		}
		ok, err := writer.create(ctx, uid, content, visibilityValue(cfg.Visibility), c.UpdatedAt, ts, cfg.Tag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  创建 %s 失败：%v\n", uid, err)
			continue
		}
		if ok {
			if cfg.Verbose {
				fmt.Printf("  已创建 %s：%s《%s》\n", uid, statusLabel(c.Subject.Type), subjectName(c))
			}
			created++
		} else {
			skipped++
		}
	}

	if !cfg.DryRun && maxTS > 0 {
		state.LastUpdatedTS = maxTS
		if err := state.save(cfg.State); err != nil {
			fmt.Fprintf(os.Stderr, "保存状态文件失败：%v\n", err)
			return 1
		}
	}

	fmt.Printf("\n完成：扫描 %d 条收藏，%s %d 条，跳过 %d 条\n", total, map[bool]string{true: "dry-run 待创建", false: "创建"}[cfg.DryRun], created, skipped)
	return 0
}

// uninstall 卸载已导入的 Bangumi memos：删除 uid 以 bgm- 开头的 memo，并重置增量状态。
// 不访问 Bangumi，仅需 memos 连接参数（--api / --db）。
func uninstall(cfg *Config) int {
	ctx := context.Background()
	if cfg.API == "" && cfg.DB == "" {
		cfg.DB = defaultDB
	}
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = time.Duration(defaultTimeout) * time.Second
	}
	writer, _, err := openWriter(cfg, timeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer writer.close()

	uids, err := writer.listBangumiOwned(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(uids) == 0 {
		fmt.Println("未找到已导入的 Bangumi memo（uid 以 bgm- 开头）")
	} else if cfg.DryRun {
		for _, uid := range uids {
			fmt.Printf("  [dry-run] 将删除 %s\n", uid)
		}
		fmt.Printf("\n完成：dry-run 待删除 %d 条\n", len(uids))
		return 0
	} else {
		deleted := 0
		for _, uid := range uids {
			if err := writer.delete(ctx, uid); err != nil {
				fmt.Fprintf(os.Stderr, "  删除 %s 失败：%v\n", uid, err)
				continue
			}
			if cfg.Verbose {
				fmt.Printf("  已删除 %s\n", uid)
			}
			deleted++
		}
		fmt.Printf("\n完成：删除 %d 条 Bangumi memo，失败 %d 条\n", deleted, len(uids)-deleted)
	}

	if !cfg.DryRun {
		if err := os.Remove(cfg.State); err == nil {
			fmt.Printf("已重置增量状态文件 %s（下次同步为全量）\n", cfg.State)
		}
	}
	return 0
}

func openWriter(cfg *Config, timeout time.Duration) (memoWriter, map[string]bool, error) {
	ctx := context.Background()
	if cfg.API != "" {
		var client *apiClient
		var err error
		switch {
		case cfg.Token != "":
			client = &apiClient{base: strings.TrimRight(cfg.API, "/"), client: &http.Client{Timeout: timeout}, token: cfg.Token}
		case cfg.Password != "" && cfg.User != "":
			client, err = signIn(cfg.API, cfg.User, cfg.Password, timeout)
			if err != nil {
				return nil, nil, err
			}
		default:
			return nil, nil, fmt.Errorf("API 模式需要 --token，或 --user 与 --password（memos >= 0.30）")
		}
		existing, err := client.listExistingUIDs(ctx)
		if err != nil {
			return nil, nil, err
		}
		return client, existing, nil
	}
	writer, err := openDB(cfg.DB, cfg.User)
	if err != nil {
		return nil, nil, err
	}
	existing, err := writer.listExistingUIDs(ctx)
	if err != nil {
		writer.close()
		return nil, nil, err
	}
	return writer, existing, nil
}

func printUsage(fs *flag.FlagSet) {
	fmt.Fprintf(fs.Output(), "用法：memos-plugin-bangumi [选项]\n\n把 Bangumi 用户「看过/玩过/读过/听过」且带短评的收藏导入 Memos。\n\n选项：\n")
	fs.PrintDefaults()
	fmt.Fprintf(fs.Output(), "\n配置文件键名 = 选项名去掉 --（- 可写作 _），如 bangumi_username。\n")
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

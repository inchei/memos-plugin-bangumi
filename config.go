// Copyright (C) 2026 memos-plugin-bangumi contributors
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
)

const (
	defaultBangumiBase = "https://api.bgm.tv"
	defaultConfigPath  = "config.toml"
	defaultDB          = "memos.db"
	defaultUser        = "admin"
	defaultVisibility  = "private"
	defaultState       = "state.json"
	defaultTimeout     = 30
	defaultLimit       = 100
	defaultLinkBase    = "https://bgm.tv"
)

var defaultUserAgent = "memos-plugin-bangumi/" + version + " (https://github.com/yourname/memos-plugin-bangumi)"

// Config 对应 TOML 配置文件的顶层结构，键名与命令行参数（去掉 --、- 换 _）一致。
type Config struct {
	BangumiUsername string `toml:"bangumi_username"`
	BangumiBase     string `toml:"bangumi_base"`
	UserAgent       string `toml:"user_agent"`
	LinkBase        string `toml:"link_base"`

	API      string `toml:"api"`
	Token    string `toml:"token"`
	Password string `toml:"password"`
	DB       string `toml:"db"`
	User     string `toml:"user"`
	// visibility: private / protected / public
	Visibility string `toml:"visibility"`
	Tag        string `toml:"tag"`

	SubjectTypes []int  `toml:"subject_types"`
	DryRun       bool   `toml:"dry_run"`
	Full         bool   `toml:"full"`
	Verbose      bool   `toml:"verbose"`
	State        string `toml:"state"`
	Timeout      int    `toml:"timeout"`
	Limit        int    `toml:"limit"`
}

func loadConfig(path string) (*Config, error) {
	cfg := &Config{
		BangumiBase: defaultBangumiBase,
		UserAgent:   defaultUserAgent,
		LinkBase:    defaultLinkBase,
		DB:          defaultDB,
		User:        defaultUser,
		Visibility:  defaultVisibility,
		State:       defaultState,
		Timeout:     defaultTimeout,
		Limit:       defaultLimit,
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("读取配置文件 %s 失败：%v", path, err)
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件 %s 失败：%v", path, err)
	}
	return cfg, nil
}

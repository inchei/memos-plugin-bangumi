// Copyright (C) 2026 memos-plugin-bangumi contributors
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Subject 条目简略信息。
type Subject struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	NameCN string `json:"name_cn"`
	Type   int    `json:"type"`
}

// Collection 用户收藏条目，来自 /v0/users/{username}/collections。
type Collection struct {
	SubjectID int     `json:"subject_id"`
	Type      int     `json:"type"`
	Rate      int     `json:"rate"`
	Comment   string  `json:"comment"`
	UpdatedAt string  `json:"updated_at"`
	Subject   Subject `json:"subject"`
}

type collectionPage struct {
	Data  []Collection `json:"data"`
	Total int          `json:"total"`
}

const updatedAtLayout = "2006-01-02T15:04:05-07:00"

// statusLabel 按条目类型给出「完成态」文案。
// 1 书籍/读过、2 动画/看过、3 音乐/听过、4 游戏/玩过、6 三次元/看过。
func statusLabel(subjectType int) string {
	switch subjectType {
	case 1:
		return "读过"
	case 3:
		return "听过"
	case 4:
		return "玩过"
	}
	return "看过"
}

func subjectName(c Collection) string {
	if strings.TrimSpace(c.Subject.NameCN) != "" {
		return c.Subject.NameCN
	}
	return c.Subject.Name
}

// buildContent 生成 memo 正文：条目名 + 完成态文案 + 短评 + 条目链接。
// linkBase 为链接域名，如 https://bgm.tv 或 https://fxbgm.tv。
func buildContent(c Collection, linkBase string) string {
	return fmt.Sprintf("%s《%s》：%s\n\n%s/subject/%d",
		statusLabel(c.Subject.Type), subjectName(c), c.Comment,
		strings.TrimRight(linkBase, "/"), c.SubjectID)
}

// parseBangumiTime 解析 "+08:00" 形式的 ISO8601 时间为 epoch 秒。
func parseBangumiTime(s string) (int64, error) {
	t, err := time.Parse(updatedAtLayout, strings.TrimSpace(s))
	if err != nil {
		return 0, err
	}
	return t.Unix(), nil
}

// fetchCollections 分页拉取指定用户的「完成态」（type=2）收藏。
// subjectTypes 为空表示不过滤条目类型，否则逐个类型拉取（subject_type 过滤在服务端进行）。
func fetchCollections(ctx context.Context, base, username, ua string, limit int, subjectTypes []int, timeout time.Duration) ([]Collection, error) {
	if limit < 1 || limit > 100 {
		limit = defaultLimit
	}
	client := &http.Client{Timeout: timeout}
	types := subjectTypes
	if len(types) == 0 {
		types = []int{0}
	}
	var all []Collection
	for _, st := range types {
		for offset := 0; ; {
			u := fmt.Sprintf("%s/v0/users/%s/collections?type=2&limit=%d&offset=%d",
				strings.TrimRight(base, "/"), url.PathEscape(username), limit, offset)
			if st != 0 {
				u += "&subject_type=" + strconv.Itoa(st)
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("User-Agent", ua)
			req.Header.Set("Accept", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				return nil, fmt.Errorf("请求 Bangumi 失败：%v", err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return nil, err
			}
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("bangumi API 返回 %s：%s", resp.Status, truncate(string(body), 200))
			}
			var page collectionPage
			if err := json.Unmarshal(body, &page); err != nil {
				return nil, fmt.Errorf("解析 Bangumi 响应失败：%v", err)
			}
			all = append(all, page.Data...)
			if len(page.Data) < limit {
				break
			}
			offset += limit
		}
	}
	return all, nil
}

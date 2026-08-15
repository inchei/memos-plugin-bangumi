// Copyright (C) 2026 memos-plugin-bangumi contributors
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	visPrivate   = "PRIVATE"
	visProtected = "PROTECTED"
	visPublic    = "PUBLIC"
)

// visibilityValue 将配置里的 private/protected/public 映射为 memos 的枚举值。
func visibilityValue(s string) string {
	switch s {
	case "protected":
		return visProtected
	case "public":
		return visPublic
	}
	return visPrivate
}

// apiClient 通过 memos REST API 读写 memo（memos 运行中）。
type apiClient struct {
	base   string
	client *http.Client
	token  string
	user   string
}

type signInResponse struct {
	AccessToken string `json:"accessToken"`
	User        struct {
		Name string `json:"name"`
	} `json:"user"`
}

// signIn 用用户名密码登录（memos >= 0.30 不再提供静态 Access Token，
// 改用 POST /api/v1/auth/signin 换取短期 token）。
func signIn(base, username, password string, timeout time.Duration) (*apiClient, error) {
	payload := fmt.Sprintf(`{"password_credentials":{"username":%q,"password":%q}}`, username, password)
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(base, "/")+"/api/v1/auth/signin", strings.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 memos 登录接口失败：%v", err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("memos 登录失败（%s）：%s", resp.Status, truncate(string(data), 200))
	}
	var out signInResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("解析 memos 登录响应失败：%v", err)
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("memos 登录未返回 access token")
	}
	return &apiClient{base: strings.TrimRight(base, "/"), client: client, token: out.AccessToken, user: out.User.Name}, nil
}

func (c *apiClient) doJSON(ctx context.Context, method, path string, query url.Values, payload, out interface{}) (int, []byte, error) {
	var body io.Reader
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(buf)
	}
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return 0, nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("请求 memos API 失败：%v", err)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return 0, nil, err
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, data, fmt.Errorf("解析 memos 响应失败：%v", err)
		}
	}
	return resp.StatusCode, data, nil
}

// listExistingUIDs 列出当前用户已有 memo 的 uid 集合。
func (c *apiClient) listExistingUIDs(ctx context.Context) (map[string]bool, error) {
	uids := make(map[string]bool)
	pageToken := ""
	for {
		query := url.Values{}
		query.Set("pageSize", "1000")
		if c.user != "" {
			query.Set("filter", fmt.Sprintf("creator == %q", c.user))
		}
		if pageToken != "" {
			query.Set("pageToken", pageToken)
		}
		var out struct {
			Memos []struct {
				Name string `json:"name"`
			} `json:"memos"`
			NextPageToken string `json:"nextPageToken"`
		}
		code, _, err := c.doJSON(ctx, http.MethodGet, "/api/v1/memos", query, nil, &out)
		if err != nil {
			return nil, err
		}
		if code != http.StatusOK {
			return nil, fmt.Errorf("memos 列表请求失败：HTTP %d", code)
		}
		for _, m := range out.Memos {
			if n := strings.TrimPrefix(m.Name, "memos/"); n != m.Name {
				uids[n] = true
			}
		}
		pageToken = out.NextPageToken
		if pageToken == "" {
			break
		}
	}
	return uids, nil
}

// create 创建 memo；uid 已存在则跳过（幂等）。createTime 为非空时写入原始时间。
// tag 非空时以 #tag 拼入正文（memos 的标签从正文 hashtag 提取）。
func (c *apiClient) create(ctx context.Context, uid, content, visibility, createTime string, _ int64, tag string) (bool, error) {
	if tag != "" {
		content += "\n#" + tag
	}
	payload := map[string]string{
		"content":    content,
		"visibility": visibility,
	}
	if createTime != "" {
		payload["createTime"] = createTime
	}
	query := url.Values{}
	query.Set("memoId", uid)
	code, data, err := c.doJSON(ctx, http.MethodPost, "/api/v1/memos", query, payload, nil)
	if err != nil {
		return false, err
	}
	if code == http.StatusOK {
		return true, nil
	}
	var apiErr struct {
		Code int `json:"code"`
	}
	if len(data) > 0 {
		_ = json.Unmarshal(data, &apiErr)
	}
	if apiErr.Code == 6 { // ALREADY_EXISTS：已存在，幂等跳过
		return false, nil
	}
	return false, fmt.Errorf("创建 memo 失败：HTTP %d：%s", code, truncate(string(data), 200))
}

func (c *apiClient) close() {}

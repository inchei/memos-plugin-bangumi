// Copyright (C) 2026 memos-plugin-bangumi contributors
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// SyncState 增量同步状态：记录上次见到的最新的 updated_at（epoch 秒）。
// Bangumi 收藏列表按 updated_at 降序返回，据此可提前停止翻页。
type SyncState struct {
	LastUpdatedTS int64 `json:"last_updated_ts"`
}

func loadState(path string) (*SyncState, error) {
	if path == "" {
		return &SyncState{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SyncState{}, nil
		}
		return nil, fmt.Errorf("读取状态文件 %s 失败：%v", path, err)
	}
	var st SyncState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("解析状态文件 %s 失败：%v", path, err)
	}
	return &st, nil
}

func (s *SyncState) save(path string) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

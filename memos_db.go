// Copyright (C) 2026 memos-plugin-bangumi contributors
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"
)

// dbWriter 直写 memos sqlite 数据库（需先停止 memos）。
type dbWriter struct {
	db     *sql.DB
	userID int64
}

func openDB(path, username string) (*dbWriter, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开数据库 %s 失败：%v", path, err)
	}
	var id int64
	if err := db.QueryRow(`SELECT id FROM user WHERE username = ?`, username).Scan(&id); err != nil {
		db.Close()
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("memos 数据库中没有用户 %q，请检查 user 配置", username)
		}
		return nil, fmt.Errorf("查询 memos 用户失败：%v", err)
	}
	return &dbWriter{db: db, userID: id}, nil
}

func (w *dbWriter) listExistingUIDs(_ context.Context) (map[string]bool, error) {
	rows, err := w.db.Query(`SELECT uid FROM memo`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	uids := make(map[string]bool)
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		uids[uid] = true
	}
	return uids, rows.Err()
}

// create 插入 memo，uid 已存在则跳过（幂等）；ts 为 Bangumi updated_at 的 epoch 秒。
func (w *dbWriter) create(_ context.Context, uid, content, visibility, _ string, ts int64, tag string) (bool, error) {
	var exists int
	if err := w.db.QueryRow(`SELECT COUNT(1) FROM memo WHERE uid = ?`, uid).Scan(&exists); err != nil {
		return false, err
	}
	if exists > 0 {
		return false, nil
	}
	payload := map[string]interface{}{}
	if tag != "" {
		payload["tags"] = []string{tag}
	}
	p, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}
	_, err = w.db.Exec(
		`INSERT INTO memo (uid, creator_id, created_ts, updated_ts, row_status, content, visibility, pinned, payload)
		 VALUES (?, ?, ?, ?, 'NORMAL', ?, ?, 0, ?)`,
		uid, w.userID, ts, ts, content, visibility, string(p))
	if err != nil {
		return false, err
	}
	return true, nil
}

func (w *dbWriter) close() { _ = w.db.Close() }

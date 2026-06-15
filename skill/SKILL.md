---
name: sessions
description: |
  用 sessions CLI 讀取過去的 Claude Code session，取代直接讀 JSONL。
  CLI 在 context 外完成過濾，原始 300K 壓到 30-50K，只保留對話和 tool call 一行摘要。
  使用者想回顧、引用、分析過去的對話時使用。
allowed-tools:
  - Bash
  - Read
---

# Session Reader

## 選擇子命令

| 意圖 | 命令 |
|------|------|
| 找目標 session | `sessions list` — 列出最近 session，`-p` 過濾專案 |
| 讀對話內容 | `sessions read <id>` — 對話全文 + tool call 一行摘要 |
| 注入為 context | `sessions context <id>` — 同 read 但更緊湊，帶 metadata header |
| 分析 token 消耗 | `sessions stats <id>` — 字元分佈、壓縮比、per-tool 明細 |
| 檢查過濾遺漏 | `sessions audit <id>` — 從被過濾內容取樣檢視 |
| 展開特定 tool call | `sessions expand <id> <tool-id>` — read 輸出的 #xxxx 即 tool-id |
| 查看 CLI 使用紀錄 | `sessions usage` — 列出哪些 session 曾呼叫此 CLI |

Session ID 支援 prefix match，前 8 碼通常就夠。

## 分頁讀取

大 session 的 read 輸出可達數千行。一次全倒會超出 Bash stdout buffer，
被 harness 寫入 persisted-output 檔案，之後只能用 Read 分段載入——等同讀原始 JSONL。

`read` 和 `context` 預設輸出 200 行後截斷，截斷時印出總行數和建議的下一段 offset。

讀取流程：
1. `sessions read <id>` → 前 200 行 + 總行數提示
2. `sessions read <id> -offset 180 -max-lines 200` → 第 180-380 行（overlap 前頁尾部 20 行銜接上下文）
3. 重複直到讀完或已取得所需資訊

全文輸出用 `-max-lines 0`。只在確認輸出行數可控時使用。

## Flags

### 分頁控制（read/context）

| Flag | 預設 | 說明 |
|------|------|------|
| `-max-lines N` | 200 | 輸出行數上限，0 = 無限制 |
| `-offset N` | 0 | 從第 N 行開始輸出 |

### 詳細模式（read/context）

選擇性展開被壓縮的內容。只在使用者明確需要該層資訊時開啟。

| Flag | 展開內容 |
|------|----------|
| `-verbose-agents` | Agent subagent 回傳的完整分析結果 |
| `-verbose-bash` | Bash stdout/stderr 完整輸出 |
| `-verbose-thinking` | Assistant thinking 區塊 |
| `-verbose-commands` | Slash/bash 指令的完整終端輸出 |

### Stats 選項

| Flag | 說明 |
|------|------|
| `-no-tokens` | 跳過 token 計算，只看字元分佈 |

## 過濾邏輯

保留對話文字和 tool call 一行摘要。過濾 tool result 原始輸出、檔案內容、tool input JSON、system/noise messages。
壓縮比視 session 組成而定：tool I/O 為主 80-88%，純對話為主 40-65%。

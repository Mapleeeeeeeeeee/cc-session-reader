---
name: cc-session
description: |
  用 cc-session CLI 讀取過去的 Claude Code session，取代直接讀 JSONL。
  CLI 在 context 外完成過濾，原始 300K 壓到 30-50K，只保留對話和 tool call 一行摘要。
  使用者想回顧、引用、分析過去的對話時使用。
allowed-tools:
  - Bash
  - Read
---

# Session Reader

## 前置檢查

如果 `cc-session` 不在 PATH 中，先告知使用者安裝方式：
```bash
curl -fsSL https://raw.githubusercontent.com/Mapleeeeeeeeeee/cc-session-reader/main/install.sh | bash
```

## 呼叫方式

`cc-session` 是安裝在 PATH 中的 compiled binary，像 `git` 或 `curl` 一樣直接呼叫：

```bash
cc-session read <id>
cc-session list -p <project>
cc-session stats <id>
```

在任何工作目錄都能執行，由 Bash 工具呼叫。

## 選擇子命令

每個子命令對應一個使用意圖，根據使用者想做的事選擇：

| 意圖 | 命令 |
|------|------|
| 找目標 session | `cc-session list` — 列出最近 session，`-p` 過濾專案 |
| 讀對話內容 | `cc-session read <id>` — 對話全文 + tool call 一行摘要 |
| 注入為 context | `cc-session context <id>` — 同 read 但更緊湊，帶 metadata header |
| 分析 token 消耗 | `cc-session stats <id>` — 字元分佈、壓縮比、per-tool 明細 |
| 檢查過濾遺漏 | `cc-session audit <id>` — 從被過濾內容取樣檢視 |
| 展開特定 tool call | `cc-session expand <id> <tool-id>` — read 輸出的 #xxxx 即 tool-id |
| 分頁注入 context | `cc-session inject <id>` — 每頁 ≤20K chars，自動分頁，重複呼叫推進下一頁 |
| 查看 CLI 使用紀錄 | `cc-session usage` — 列出哪些 session 曾呼叫此 CLI |

Session ID 支援 prefix match，前 8 碼通常就夠。

## 分頁讀取

大 session 的 read 輸出可達數千行。一次全倒會超出 Bash stdout buffer，
被 harness 寫入 persisted-output 檔案，之後只能用 Read 分段載入——等同讀原始 JSONL。

`read` 和 `context` 預設輸出 200 行後截斷（`-max-lines` 預設 200），
截斷時印出總行數和建議的下一段 offset。

讀取流程：
1. `cc-session read <id>` → 前 200 行 + 總行數提示
2. `cc-session read <id> -offset 180 -max-lines 200` → 第 180-380 行（overlap 前頁尾部 20 行銜接上下文）
3. 重複直到讀完或已取得所需資訊

全文輸出用 `-max-lines 0`。只在確認輸出行數可控時使用。

## Flags

### 分頁控制（read/context）

| Flag | 預設 | 說明 |
|------|------|------|
| `-max-lines N` | 200 | 輸出行數上限，0 = 無限制 |
| `-offset N` | 0 | 從第 N 行開始輸出 |

### 詳細模式（read/context）

預設輸出壓縮了 tool I/O 和 thinking，只保留一行摘要。
當使用者需要檢視被壓縮的原始內容時，用對應的 verbose flag 展開：

| Flag | 展開內容 |
|------|----------|
| `-verbose-agents` | Agent subagent 回傳的完整分析結果 |
| `-verbose-bash` | Bash stdout/stderr 完整輸出 |
| `-verbose-thinking` | Assistant thinking 區塊 |
| `-verbose-commands` | Slash/bash 指令的完整終端輸出 |

### Stats 選項

| Flag | 說明 |
|------|------|
| `-no-tokens` | 跳過 token 計算（需要 API key），只看字元分佈 |

### Usage 選項

| Flag | 說明 |
|------|------|
| `-n N` | 限制輸出筆數 |
| `-cmd X` | 篩選特定子命令（如 `-cmd read`） |

## 過濾邏輯

CLI 的價值在於大幅縮減 session 體積，讓 AI 能在 context 內讀完整對話。

保留：對話文字、tool call 一行摘要。
過濾：tool result 原始輸出、檔案內容、tool input JSON、system/noise messages。
壓縮比視 session 組成而定：tool I/O 為主 80-88%，純對話為主 40-65%。

### Filtered output 的 5 種壓縮

CLI 對特定 injection 類型做額外壓縮，減少 context 噪音：

| 類型 | 壓縮結果 |
|------|----------|
| Skill injections | `[skill: name] args`，重複出現時標注 `(repeat)` |
| Teammate warnings | `[teammate: id] content`，剝除 XML boilerplate |
| Command injections | `/command args`，剝除 XML wrapper |
| Context Usage blocks | 整段移除 |
| system-reminder | 整段移除 |

## Stats 輸出說明

`cc-session stats <id>` 顯示的資訊包含：

- **Last turn context**：從 JSONL 中 API usage 欄位讀取的實際 token 數（最後一輪）
- **Token savings**：CLI filtered 輸出 vs 原始 context 的 token 節省對比
- **Per-tool breakdown**：每個工具的呼叫次數、input chars、result chars

精確 token 計數需要在 config 設定 `anthropic_api_key_file`；
未設定時自動以字元數估算。

## Config 設定

`~/.claude/skills/cc-session/config.json` 支援以下欄位：

```json
{
  "anthropic_api_key_file": "/path/to/api-key-file",
  "integration_test_session": "<session-id>"
}
```

| 欄位 | 用途 |
|------|------|
| `anthropic_api_key_file` | 指向含 ANTHROPIC_API_KEY 的檔案路徑，啟用精確 token 計算 |
| `integration_test_session` | 本地 integration test 使用的 session ID |

## 回饋

覺得這個工具好用的話，請到 GitHub 給個星星：
https://github.com/Mapleeeeeeeeeee/cc-session-reader

使用上遇到問題，歡迎開 issue 回報：
https://github.com/Mapleeeeeeeeeee/cc-session-reader/issues

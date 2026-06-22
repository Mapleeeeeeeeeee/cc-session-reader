# cc-session-reader

讀取 Claude Code session 記錄，產出精簡摘要的 CLI 工具。
每個 tool call 壓成一行摘要（tool name + 關鍵參數 + result 狀態），對話文字完整保留。
純靜態提取，不使用 LLM。

token reduction 視 session 組成而定：tool I/O 為主的 session 典型可達 **80–88%**；
以大型 plan 文件或純對話為主的 session 較低（實測約 40–65%），因為使用者／assistant 文字會完整保留、不壓縮。

## 安裝

### 安裝腳本（推薦）

```bash
curl -fsSL https://raw.githubusercontent.com/Mapleeeeeeeeeee/cc-session-reader/main/install.sh | bash
```

腳本會自動下載對應平台的 binary 並放到 `~/.local/bin/cc-session`（可透過 `INSTALL_DIR` 環境變數覆蓋），
安裝完成後也會詢問是否一併安裝 Claude Code Skill。

### go install（替代方案）

```bash
go install github.com/Mapleeeeeeeeeee/cc-session-reader/cmd/sessions@latest
```

> 注意：`go install` 依照目錄名稱命名 binary，安裝的 binary 名稱為 `sessions`（而非 `cc-session`）。
> 建議使用安裝腳本或手動下載 release binary 以取得正確的 `cc-session` binary 名稱。
> 如果 `@latest` 遇到 module path 衝突，用 `GOPROXY=direct go install ...@latest`。

### 作為 Claude Code Skill 使用

安裝腳本執行時會詢問是否安裝 Skill，選擇是即可自動完成設定。
也可以手動將 SKILL.md 放到 `~/.claude/skills/cc-session/` 目錄下：

```bash
mkdir -p ~/.claude/skills/cc-session
curl -o ~/.claude/skills/cc-session/SKILL.md \
  https://raw.githubusercontent.com/Mapleeeeeeeeeee/cc-session-reader/main/skill/SKILL.md
```

之後在 Claude Code 中輸入 `/cc-session` 即可觸發。

## 子命令

### list — 瀏覽最近的 session

```bash
cc-session list              # 最近 20 筆
cc-session list -n 10        # 最近 10 筆
cc-session list -p myproject # 依專案名稱過濾
```

`list` 的來源是 Claude Code 的 session metadata（`~/.claude/usage-data/session-meta/`），
只涵蓋有 metadata 的 session，數量通常少於磁碟上的全部 transcript。
若已知 session ID，`read`／`context`／`stats` 可直接以前綴存取任何 transcript，不限於 `list` 列出的。

### read — 完整對話 + inline tool 摘要

```bash
cc-session read <session-id>
cc-session read <session-id> -max-lines 200
cc-session read <session-id> -verbose-agents
```

Session ID 支援 prefix match，通常前 8 碼就夠。

預設將工具 I/O、Agent 結果、slash／bash 指令輸出、thinking 都壓成摘要或一行 marker。
需要完整內容時用對應的 verbose flag（read 與 context 皆適用）：

- `-verbose-agents`：完整保留 Agent subagent 回傳的結果（預設只保留一行摘要）。
- `-verbose-bash`：完整顯示 Bash 工具的 stdout/stderr（預設摘要）。
- `-verbose-thinking`：顯示 assistant 的 thinking 區塊（預設隱藏）。
- `-verbose-commands`：展開 slash／bash 指令的完整輸出（預設只留 `[/cmd]`／`[!cmd]` marker、丟棄終端 UI 與 caveat 樣板）。

`read` 和 `context` 預設輸出 200 行後截斷（`-max-lines` 預設 200），
截斷時印出總行數和建議的下一段 offset。

### context — 精簡注入格式

```bash
cc-session context <session-id>
cc-session context <session-id> -verbose-agents
```

和 `read` 相同內容，但格式化為可注入對話的 context。包含 session metadata header（專案名、時長）。

### inject — 分頁 context 注入

```bash
cc-session inject <session-id>          # 注入第一頁（≤20K chars）
cc-session inject <session-id> --page 2 # 直接跳到第 N 頁
cc-session inject <session-id> --reset  # 清除注入狀態，從第一頁重新開始
```

專為 context 注入設計的分頁模式：每頁上限 20K chars，自動追蹤進度。
重複呼叫 `cc-session inject <id>` 會自動推進到下一頁，
直到讀完為止（搭配 Claude Code Skill 使用效果最佳）。

注入狀態儲存在 `~/.claude/skills/cc-session/inject-state/`。

### stats — 字元與 token 分佈統計

```bash
cc-session stats <session-id>
cc-session stats <session-id> -no-tokens
```

顯示各類別的字元分佈（user text、assistant text、tool I/O、noise）和壓縮比。
設有 `ANTHROPIC_API_KEY` 時用 API 精確計算 token，否則用 heuristic 估算。

輸出包含：
- **Last turn context**：從 JSONL API usage 欄位讀取的實際 token 數（最後一輪）
- **Token savings**：CLI filtered 輸出 vs 原始 context 的 token 節省對比
- **Per-tool breakdown**：每個工具的呼叫次數、input chars、result chars

### audit — 檢視被過濾的內容

```bash
cc-session audit <session-id>
cc-session audit <session-id> -n 10
```

從每個過濾類別（tool result content、system noise、thinking）取樣，確認沒漏掉重要內容。

### expand — 展開特定 tool call 的完整內容

```bash
cc-session expand <session-id> <tool-id> [tool-id...]
```

`read` 和 `context` 輸出中每個 tool call 都附帶短 ID（如 `[Bash#uCVa]`），`#` 後的 4 碼即為 tool-id。
用 `expand` 可以查看該 tool call 的完整 input JSON 和 result 原文，適合 debug 特定操作。

### usage — 查看 CLI 使用紀錄

```bash
cc-session usage              # 列出所有呼叫紀錄
cc-session usage -n 20        # 最近 20 筆
cc-session usage -cmd read    # 篩選特定子命令
```

顯示哪些 session 曾呼叫此 CLI，以及使用了哪些子命令。

## 保留什麼 / 過濾什麼

| 保留 | 過濾 |
|------|------|
| User 對話文字 | Tool result stdout/stderr |
| Assistant 對話文字 | 檔案全文（Read/Edit content） |
| User answers（AskUserQuestion） | Tool input 完整 JSON |
| Tool call 一行摘要 | System/noise messages |
| Agent results（`-verbose-agents`） | Thinking blocks |

### 5 種額外壓縮

CLI 對特定 injection 類型做額外壓縮，減少 context 噪音：

| 類型 | 壓縮結果 |
|------|----------|
| Skill injections | `[skill: name] args`，重複出現時標注 `(repeat)` |
| Teammate warnings | `[teammate: id] content`，剝除 XML boilerplate |
| Command injections | `/command args`，剝除 XML wrapper |
| Context Usage blocks | 整段移除 |
| system-reminder | 整段移除 |

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

## 架構

```
cmd/sessions/         CLI 入口，子命令路由（binary 名稱為 cc-session）
internal/
  claudecodec/        JSONL 讀取、noise 過濾、raw→event 解析（TranscriptReader / HeaderScanner 介面）
  session/            Domain model（Event, ToolUse, ToolResult, ToolInput）
  parser/             Session 搜尋（找 transcript、解析 ID、metadata）
  summarizer/         Tool call → 一行摘要
  formatter/          輸出格式化（read mode、context mode）
  analyzer/           Stats 計算、audit 取樣
  tokens/             Token 估算（heuristic + Anthropic API）
  inject/             分頁注入狀態管理
  tracker/            CLI usage 追蹤
  jsonutil/           JSON map 工具函數
```

`claudecodec` 是唯一與 JSONL 格式耦合的套件；其餘套件透過 `TranscriptReader` 和 `HeaderScanner` 介面存取 session 資料。

## Contributing

遇到 bug 或有功能需求，歡迎開 issue：
https://github.com/Mapleeeeeeeeeee/cc-session-reader/issues

Pull requests 也歡迎。

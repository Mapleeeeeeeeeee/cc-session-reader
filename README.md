# cc-session-reader

讀取 Claude Code session 記錄，產出精簡摘要的 CLI 工具。
每個 tool call 壓成一行摘要（tool name + 關鍵參數 + result 狀態），對話文字完整保留。
純靜態提取，不使用 LLM。

token reduction 視 session 組成而定：tool I/O 為主的 session 典型可達 **80–88%**；
以大型 plan 文件或純對話為主的 session 較低（實測約 40–65%），因為使用者／assistant 文字會完整保留、不壓縮。

## 安裝

```bash
go install github.com/Mapleeeeeeeeeee/cc-session-reader/cmd/sessions@v0.1.0
```

安裝後 `sessions` binary 會放在 `$GOPATH/bin`（確保該路徑在 PATH 中）。

> 如果 `@latest` 遇到 module path 衝突，用明確版號 `@v0.1.0` 或 `GOPROXY=direct go install ...@latest`。

### 作為 Claude Code Skill 使用

將 SKILL.md 放到 `~/.claude/skills/sessions/` 目錄下：

```bash
mkdir -p ~/.claude/skills/sessions
curl -o ~/.claude/skills/sessions/SKILL.md \
  https://raw.githubusercontent.com/Mapleeeeeeeeeee/cc-session-reader/main/skill/SKILL.md
```

之後在 Claude Code 中輸入 `/sessions` 即可觸發。

## 子命令

### list — 瀏覽最近的 session

```bash
sessions list              # 最近 20 筆
sessions list -n 10        # 最近 10 筆
sessions list -p myproject # 依專案名稱過濾
```

`list` 的來源是 Claude Code 的 session metadata（`~/.claude/usage-data/session-meta/`），
只涵蓋有 metadata 的 session，數量通常少於磁碟上的全部 transcript。
若已知 session ID，`read`／`context`／`stats` 可直接以前綴存取任何 transcript，不限於 `list` 列出的。

### read — 完整對話 + inline tool 摘要

```bash
sessions read <session-id>
sessions read <session-id> -max-lines 200
sessions read <session-id> -verbose-agents
```

Session ID 支援 prefix match，通常前 8 碼就夠。

預設將工具 I/O、Agent 結果、slash／bash 指令輸出、thinking 都壓成摘要或一行 marker。
需要完整內容時用對應的 verbose flag（read 與 context 皆適用）：

- `-verbose-agents`：完整保留 Agent subagent 回傳的結果（預設只保留一行摘要）。
- `-verbose-bash`：完整顯示 Bash 工具的 stdout/stderr（預設摘要）。
- `-verbose-thinking`：顯示 assistant 的 thinking 區塊（預設隱藏）。
- `-verbose-commands`：展開 slash／bash 指令的完整輸出（預設只留 `[/cmd]`／`[!cmd]` marker、丟棄終端 UI 與 caveat 樣板）。

### context — 精簡注入格式

```bash
sessions context <session-id>
sessions context <session-id> -verbose-agents
```

和 `read` 相同內容，但格式化為可注入對話的 context。包含 session metadata header（專案名、時長）。

### stats — 字元與 token 分佈統計

```bash
sessions stats <session-id>
sessions stats <session-id> -no-tokens
```

顯示各類別的字元分佈（user text、assistant text、tool I/O、noise）和壓縮比。
設有 `ANTHROPIC_API_KEY` 時用 API 精確計算 token，否則用 heuristic 估算。

### audit — 檢視被過濾的內容

```bash
sessions audit <session-id>
sessions audit <session-id> -n 10
```

從每個過濾類別（tool result content、system noise、thinking）取樣，確認沒漏掉重要內容。

### expand — 展開特定 tool call 的完整內容

```bash
sessions expand <session-id> <tool-id> [tool-id...]
```

`read` 和 `context` 輸出中每個 tool call 都附帶短 ID（如 `[Bash#uCVa]`），`#` 後的 4 碼即為 tool-id。
用 `expand` 可以查看該 tool call 的完整 input JSON 和 result 原文，適合 debug 特定操作。

## 保留什麼 / 過濾什麼

| 保留 | 過濾 |
|------|------|
| User 對話文字 | Tool result stdout/stderr |
| Assistant 對話文字 | 檔案全文（Read/Edit content） |
| User answers（AskUserQuestion） | Tool input 完整 JSON |
| Tool call 一行摘要 | System/noise messages |
| Agent results（`-verbose-agents`） | Thinking blocks |

## 架構

```
cmd/sessions/         CLI 入口，子命令路由
internal/
  claudecodec/        JSONL 讀取、noise 過濾、raw→event 解析
  session/            Domain model（Event, ToolUse, ToolResult, ToolInput）
  parser/             Session 搜尋（找 transcript、解析 ID、metadata）
  summarizer/         Tool call → 一行摘要
  formatter/          輸出格式化（read mode、context mode）
  analyzer/           Stats 計算、audit 取樣
  tokens/             Token 估算（heuristic + Anthropic API）
  jsonutil/           JSON map 工具函數
```

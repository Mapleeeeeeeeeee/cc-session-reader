# cc-session-reader

讀取 Claude Code session 記錄，產出精簡摘要的 CLI 工具。
每個 tool call 壓成一行摘要（tool name + 關鍵參數 + result 狀態），對話文字完整保留。
純靜態提取，不使用 LLM。

典型 token reduction：**80–88%**。

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

### read — 完整對話 + inline tool 摘要

```bash
sessions read <session-id>
sessions read <session-id> -max-lines 200
sessions read <session-id> -verbose-agents
```

Session ID 支援 prefix match，通常前 8 碼就夠。

`-verbose-agents`：完整保留 Agent subagent 回傳的結果（預設只保留一行摘要）。

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

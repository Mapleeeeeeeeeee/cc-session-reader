# cc-session-reader

讀取 Claude Code session 記錄，產出精簡摘要的 CLI 工具。
每個 tool call 壓成一行摘要（tool name + 關鍵參數 + result 狀態），對話文字完整保留。
純靜態提取，不使用 LLM。

token reduction 視 session 組成而定：tool I/O 為主的 session 典型可達 **80–88%**；
以大型 plan 文件或純對話為主的 session 較低（實測約 40–65%），因為使用者／assistant 文字會完整保留、不壓縮。

## 安裝

### macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/Mapleeeeeeeeeee/cc-session-reader/main/install.sh | bash
```

腳本會自動下載對應平台的 binary 並放到 `~/.local/bin/cc-session`（可透過 `INSTALL_DIR` 環境變數覆蓋），
並預設安裝 Claude Code Skill。不需要 Skill 時加 `--no-skill`。

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/Mapleeeeeeeeeee/cc-session-reader/main/install.ps1 | iex
```

安裝到 `$env:LOCALAPPDATA\cc-session\`，互動模式下詢問是否加入 PATH 和安裝 Skill。

### 其他安裝方式

- **下載 Binary**：從 [GitHub Releases](https://github.com/Mapleeeeeeeeeee/cc-session-reader/releases) 下載對應平台壓縮檔，解壓後放到 PATH。
- **go install**：`go install github.com/Mapleeeeeeeeeee/cc-session-reader/cmd/cc-session@latest`（binary 放在 `$GOPATH/bin`）。
- **僅安裝 Skill**：`mkdir -p ~/.claude/skills/cc-session && curl -o ~/.claude/skills/cc-session/SKILL.md https://raw.githubusercontent.com/Mapleeeeeeeeeee/cc-session-reader/main/SKILL.md`

## 子命令

| 命令 | 說明 | 範例 |
|------|------|------|
| `list` | 瀏覽最近的 session（用過 cc-session 的標 `[refs]`） | `cc-session list -n 10 -p myproject` |
| `read` | 完整對話 + inline tool 摘要 | `cc-session read <id> -max-lines 200` |
| `context` | 精簡注入格式，含 session metadata header | `cc-session context <id>` |
| `inherit` | 分頁 context 繼承（每頁 ≤20K chars，自動追蹤進度，`-reset` 重來） | `cc-session inherit <id>` |
| `stats` | 字元與 token 分佈統計及壓縮比 | `cc-session stats <id> -no-tokens` |
| `audit` | 取樣被過濾的內容，確認沒漏掉重要資訊 | `cc-session audit <id> -n 10` |
| `expand` | 展開特定 tool call 的完整 input/result | `cc-session expand <id> uCVa` |
| `usage` | 查看此 CLI 的使用紀錄 | `cc-session usage -cmd read` |

Session ID 支援 prefix match，通常前 8 碼就夠。`read` 和 `context` 預設截斷 200 行（`-max-lines` 可調），截斷時印出總行數和建議的下一段 offset。

`list` 來源是 session metadata（`~/.claude/usage-data/session-meta/`），數量少於磁碟全部 transcript；`read`／`context`／`stats` 可直接存取任何 transcript，不限於 `list` 列出的。

### Verbose flags（適用 read / context）

| Flag | 效果 |
|------|------|
| `-verbose-agents` | 完整保留 Agent subagent 回傳結果（預設只留一行摘要） |
| `-verbose-bash` | 完整顯示 Bash 工具的 stdout/stderr（預設摘要） |
| `-verbose-thinking` | 顯示 assistant 的 thinking 區塊（預設隱藏） |
| `-verbose-commands` | 展開 slash／bash 指令完整輸出（預設只留 marker） |

## 壓縮邏輯

工具呼叫、Bash 輸出、Agent 結果、thinking 都預設壓成摘要或一行 marker；User／Assistant 對話文字完整保留。當 session 內有 cc-session inherit/read/context 呼叫時，連續呼叫壓成一行（如 `(cc-session#id: inherited session X here, N lines omitted)`），`-verbose-bash` 跳過此壓縮。舊 session 裡的 `cc-session inject` 呼叫（改名前的舊命令名）也會比照壓成一行，維持 `injected session X here` 的措辭。Skill injection、teammate warning、command injection、Context Usage 區塊、system-reminder 等 injection 類型會額外壓縮或整段移除，減少 context 噪音。詳細過濾規則見 [SKILL.md](SKILL.md)。

## Config 設定

> 💡 **提示**：此設定檔為**選擇性（Optional）**。若未配置，僅會影響 `stats` 與 `benchmark` 的 Token 計算；其他讀取、過濾與注入等核心功能均不受影響。

若需要 Token 統計或自訂行為，可在 `~/.claude/skills/cc-session/config.json` 進行配置。您可以使用專案根目錄的 `config.json.template` 作為範本建立設定：

```bash
mkdir -p ~/.claude/skills/cc-session
curl -o ~/.claude/skills/cc-session/config.json \
  https://raw.githubusercontent.com/Mapleeeeeeeeeee/cc-session-reader/main/config.json.template
```

該設定檔支援以下欄位：

```json
{
  "anthropic_api_key_file": "~/.config/anthropic/.env",
  "integration_test_session": "<session-id>",
  "no_usage": false
}
```

| 欄位 | 用途 |
|------|------|
| `anthropic_api_key_file` | 指向含 `ANTHROPIC_API_KEY` 的檔案路徑，啟用精確 token 計算 |
| `integration_test_session` | 本地 integration test 使用的 session ID |
| `no_usage` | 設為 `true` 停用 CLI usage 追蹤（不寫入 `usage.jsonl`） |

也可透過環境變數 `CC_SESSION_NO_USAGE=1` 達到相同效果（覆蓋 config.json 設定）。

## 架構

```
cmd/cc-session/       CLI 入口，子命令路由
internal/
  claudecodec/        JSONL 讀取、noise 過濾、raw→event 解析（TranscriptReader / HeaderScanner 介面）
  session/            Domain model（Event, ToolUse, ToolResult, ToolInput）
  parser/             Session 搜尋（找 transcript、解析 ID、metadata）
  summarizer/         Tool call → 一行摘要
  formatter/          輸出格式化（read mode、context mode）
  analyzer/           Stats 計算、audit 取樣
  tokens/             Anthropic token counting API
  inject/             分頁注入狀態管理
  tracker/            CLI usage 追蹤
  jsonutil/           JSON map 工具函數
```

`claudecodec` 是唯一與 JSONL 格式耦合的套件；其餘套件透過 `TranscriptReader` 和 `HeaderScanner` 介面存取 session 資料。

## 移除

```bash
rm ~/.local/bin/cc-session
rm -rf ~/.claude/skills/cc-session
```

## Contributing

遇到 bug 或有功能需求，歡迎開 issue：
https://github.com/Mapleeeeeeeeeee/cc-session-reader/issues

Pull requests 也歡迎。

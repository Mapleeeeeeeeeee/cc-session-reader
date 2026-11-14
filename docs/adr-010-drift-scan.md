# ADR-010：`drift` 指令，Claude Code 更新後一次列出 reader 還沒處理的形狀

**狀態**：已實作。

## 為什麼要有這個指令

ADR-008 反覆出現同一種事：Claude Code 更新後在 transcript 裡寫出新的形狀，reader 不認得，
要等到某個輸出壞了才有人發現（對話被印兩次、turn 計數偏高、統計少算）。
`audit` 的「Dropped record types」區段只看單一 session、只看頂層 `type`；
其他三處同樣靜默丟棄，完全看不到：

- `type: "attachment"` 的 `attachment.type`（整類當 noise）。
- `type: "system"` 的 `subtype`（整類當 noise，只有 `compact_boundary` 有專門處理）。
- `message.content` 陣列裡的 block `type`，以及 `tool_result` 內層 content 的 block `type`
  （只讀 `text`、`thinking`、`tool_use`、`tool_result`，其餘靜默略過）。

`cc-session drift [-days N]`（預設 30）掃描 `~/.claude/projects` 下近 N 天內修改過的 `.jsonl`，
對上面四類（內層 block 與外層分成兩段，所以輸出五段）列出「reader 沒有明確處理」的值。
每個值顯示出現筆數、出現在幾個 session、最早出現的 Claude Code `version` 與日期、一個範例位置（檔案路徑加行號）。
未知的頂層 `type` 沿用 `audit` 的 ATTENTION 規則（帶 `message.role`）：判斷直接呼叫
`analyzer.DiscardedTypeStats.NeedsAttention`，不另寫一份。

指令只報告。`read`、`stats`、`audit`、`benchmark`、`inject` 的輸出不變，被略過的 block type 也沒有順手去處理；
要不要處理，是看過報告之後另一個決定。

## 決定：「已處理」每一類只有一個定義來源，而且是 reader 自己用的那份

另外維護一份給 `drift` 看的清單，遲早跟 reader 的實際行為脫節，到時候報告說「已處理」，實際上 reader 早就不認得。
所以每一類只認下面這一個來源，都在 `internal/claudecodec`，`drift` 透過 `IsXxx` 函式查詢，自己不存任何清單：

| 類別 | 定義來源 | 是 reader 分派用的那份嗎 |
|---|---|---|
| 頂層 `type` | `reader.go` 的 `conversationTypes`、`noiseTypes`、`knownNonConversationTypes`（`IsHandledRecordType`） | 是，`parseLineWithToolCalls` 直接查這三張表 |
| `attachment.type` | `known_shapes.go` 的 `knownAttachmentTypes` | 否，新增的清單，見下 |
| `system` 的 `subtype` | `known_shapes.go` 的 `knownSystemSubtypes` | 只有 `compact_boundary` 是；其餘是新增的清單，見下 |
| `message.content` 的 block `type` | `known_shapes.go` 的 `handledContentBlockTypes` | 見下 |
| `tool_result` 內層 block `type` | `known_shapes.go` 的 `handledToolResultBlockTypes` | 見下 |

**頂層 `type`**：直接重用既有三張表，沒有新增任何東西。

**`attachment.type`、`system` 的 `subtype`**：reader 整類丟掉，從來不看子類型，所以「已處理的集合」本來就不存在。
這兩份清單因此是新增的，而且它們就是唯一來源；它們的意義是「已經有人看過、確認不需要處理的值」，不是「reader 會分派的值」。
`compact_boundary` 是 reader 實際分派的子類型，清單裡用 reader 分派時同一個常數（`systemSubtypeCompaction`）。

初始內容是 2026-11-14 掃描本機全部 6,803 個 transcript（不限天數，不含 workflow 的 `journal.jsonl`）
列舉出來的：63 種 `attachment.type`、12 種 `system` subtype（含 `compact_boundary`）。
這個基準線的意思是「到這天為止本機看過的都已知」，所以 Claude Code 之後新增的值才會出現在報告裡。
其中 `critical_system_reminder` 是掃描當天（CLI 2.1.325）才出現的，2 筆，也列進基準線。
使用者之後看到新值、確認無害，在對應的 map 補一行；補完它就不再出現。

**block `type`**：reader 是用 `switch` 與 `if` 分派的，不是查表，所以集合本身是新增的。
為了讓它跟分派動作綁在一起，做了兩件事：分派處的字面字串全部換成同一組常數
（`blockTypeText`、`blockTypeThinking`、`blockTypeToolUse`、`blockTypeToolResult`），集合也用這組常數建；
再加 `known_shapes_test.go`，每個集合成員各用一筆 transcript 證明 reader 確實讀它。
這擋得住「reader 停止處理某個型別、集合沒跟著改」，擋不住「reader 新增處理一個型別、忘了加進集合」
（後者的症狀是 `drift` 多報一個其實已經處理的值，不會漏報）。

## 其他決定

- **掃描範圍用檔案修改時間，不用 session 起始時間。** 約 7,000 個 `.jsonl`，逐檔讀 header 太慢；`stat` 就夠。
  代價是 resume 過的舊 session 會因為最近寫入而被掃到，報告裡可能含天數視窗之外的紀錄，
  「最早出現」是依紀錄自己的 `timestamp`，不受視窗影響。
- **掃主 session 與 `subagents/` 底下所有層。** subagent transcript 算進它所屬的 session（`subagents` 上一層的目錄名），
  不另外算一個 session。
- **跳過 `journal.jsonl`。** workflow 在 agent transcript 旁邊寫的 journal 不是 transcript，
  紀錄是 `started`、`result`、`launched`、`failed`，不跳過的話它們會永遠出現在「未處理 type」裡。
- **沒有時間戳或 `version` 的紀錄（如 `api-request-blob`）沿用同一檔案前面最近一筆的值。**
  同一檔案前面都沒有時，顯示 `unknown version` 或 `unknown date`。
- **串流逐行讀，只留每個值的計數。** 記憶體不隨檔案大小增長。
  本機 3.4 GB 的近 30 天 transcript 約 22 秒，最大常駐記憶體約 33 MB。
- **不是合法 JSON 的行略過並計數**（例如正在寫入的最後一行），輸出開頭會說跳過幾行。
- **指令不放進 SKILL.md 的 `argument-hint`，也不列在 README 的子命令表。** 它跟 `benchmark` 一樣是維護者用的分析指令，
  不是 skill 的快速啟動動作；`argHint` 留空，SKILL.md 就不用跟著改。

## 第一次實際掃描（2026-11-14，本機，預設近 30 天，1,755 個檔案、484 個 session）

兩類有東西：

- 未處理頂層 `type`：`history-suppression`（34 筆，2.1.311）、`dev-mods`（18 筆，2.1.303）、
  `continued-in`（2 筆，2.1.296）。三個都不帶 `message.role`，沒有 ATTENTION。
- 未處理 content block：`message.content` 裡有 `image`（204 筆）、`server_tool_use`（182）、`advisor_tool_result`（170）；
  `tool_result` 內層有 `image`（2,682）、`tool_reference`（249）、`document`（1）。

這些這次都只列出，沒有處理。筆數是含有該值的紀錄數，一筆紀錄裡有幾個同型 block 只算一筆。
「最早出現」是掃描視窗內最早的一筆，不是全部歷史；要看更早，加大 `-days`。

## 延後

「沒帶 `promptSource` 卻以 `user:` 印出的訊息樣式」偵測（文字內容的漂移，不是結構的漂移）不在這個指令範圍，之後另外決定。

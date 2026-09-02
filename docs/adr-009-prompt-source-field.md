# ADR-009：訊息來源先看 `promptSource` 欄位，字串比對降為備援

**狀態**：已實作（2026-09-02），決定 4 於同日修正排除 `sdk`（見該節）。
字串分類認不出形狀的 `sdk` 來源標 `user (sdk):`；context 格式的短前綴是 `S:`（`U:`/`H:` 之外新增一個）。

判斷 promptSource 是否與字串分類衝突（決定 4）、以及決定 3 的「system 但形狀認不出來」，
兩者的解析都收在 parser 層（`claudecodec.parseLineWithToolCalls`）：字串分類先跑一次，
結果不論如何都先掛上 `PromptSource`；只有 `typed`、`queued`、`suggestion_accepted` 這三個人的來源
對上 harness 形狀時才整則重置成純文字，`sdk` 不在此列。
render 層因此不需要重新判斷字串分類，只在「字串分類完全沒認出形狀」的兜底分支上，
依 `PromptSource` 決定 `harness:`／`user (sdk):`／`user:`（`session.UserMessage.IsClassifiedAsHarness`
與 `formatter.plainTextRole`）。實測 `b11858cf` 的 8 則 `system` 訊息全是 `<task-notification>`
這種已認得的形狀，`read` 輸出的 md5 改動前後一致。

ADR-008 把 harness 注入的 user 訊息在 parser 層分類，但判斷全靠比對文字。
每次 harness 改措辭或加新的包裝，reader 就漏一批（ADR-008 之後兩天又補了八種）。
CLI 從 2.1.165 起在 user entry 上寫 `promptSource` 欄位，這份 ADR 記錄它能替代多少字串比對。

量測基礎：`~/.claude/projects` 下最近 60 天的主 session 與 `subagents/` 兩層 transcript，
版本 2.1.165 以上的 user 文字 entry 共 11,064 則（2026-09-02）。

## 欄位長什麼樣

| 值 | 則數 | 意思 | 啟動一輪的比例 |
|---|---:|---|---:|
| `typed` | 3,547 | 人在終端打的 | 89.6% |
| `sdk` | 1,213 | SDK 或 `claude -p` 程式化送入 | 98.3% |
| `system` | 1,206 | harness 注入 | 93.9% |
| `queued` | 158 | 人排隊送出的 | 93.0% |
| `suggestion_accepted` | 50 | 人接受了建議 | 96.0% |
| 沒帶 | 4,890 | 見下節 | |

「啟動一輪」的判準同 ADR-008 第 5 項：往後掃到下一則 user 文字為止，中間有帶 usage 的 assistant 訊息。
五種值全都在九成上下，也就是**帶 `promptSource` 的訊息就是啟動 turn 的 prompt**。

## 沒帶的那 44% 不是缺資料，是另一類訊息

沒帶 `promptSource` 的 4,890 則裡，前 22 種形狀全是 harness 注入或附屬訊息：

| 形狀 | 則數 |
|---|---:|
| teammate message（開場白起頭 1,080 + 標籤直接起頭 769） | 1,849 |
| skill 注入 `Base directory for this skill:` | 464 |
| `<system-reminder>` | 439 |
| `[Request interrupted by user]` | 244 |
| `[Image: …]` 佔位符 | 237 |
| `<local-command-caveat>` / `<local-command-stdout>` / `<command-*>` | 394 |
| 壓縮續接、`[SYSTEM NOTIFICATION]`、coordinator、fork-boilerplate、nudge、mid-turn 使用者訊息 | 271 |

所以這個欄位的語意是：**有帶 = 這則是啟動 turn 的 prompt；沒帶 = 這則是注入或附屬**。
「沒帶」本身就是訊號，不是缺值。

覆蓋率依版本在 25% 到 89% 之間浮動，但那反映的是各版本注入訊息的比例不同，不是欄位時有時無。

## 與字串比對的衝突

| 情況 | 則數 |
|---|---:|
| 人的來源（`typed`/`queued`/`suggestion_accepted`）但字串比對判成 harness | **0** |
| `system` 但字串比對判不出來 | 15 |

那 15 則是排程或迴圈觸發的 prompt（`Check if background task …`、`Background agent "…"`、
`檢查 /qa skill fork…`），內容是自然語言，沒有任何可比對的形狀。
只有 `promptSource` 抓得到它們。

## 決定

1. **有 `promptSource` 時以它為準。** `system` 歸 harness 角色；`typed`、`queued`、`suggestion_accepted` 歸 user；
   `sdk` 歸 user（它是呼叫方下的指令，在那份 transcript 裡就是使用者的位置），但在 header 標 `user (sdk):`，
   讓讀者知道不是人手打的。**`sdk` 標的是這份 session 由誰驅動，不是這一則訊息由誰寫的**：
   harness 注入（`<task-notification>`、stop hook 等）在 SDK 或 `claude -p` 驅動的 session 裡
   一樣會帶 `sdk`，跟人手打的訊息沒有欄位上的差異，所以決定 4 的字串分類覆蓋規則排除 `sdk`。
2. **`CountsAsTurn()` 對帶 `promptSource` 的訊息一律回 true**，不分值。五種值實測都啟動一輪。
   ADR-008 第 5 項那張逐種類的表只剩「沒帶」的訊息需要。
3. **字串比對留著，只處理沒帶的訊息。** 沒帶的訊息才需要知道是哪一種注入（決定 compact 形式）。
   帶 `promptSource=system` 但字串比對判不出形狀的（上面那 15 則），渲染全文、標 `harness:`。
4. **`promptSource` 與字串比對結論相反時以 `promptSource` 為準，但 `sdk` 除外。** `typed`、`queued`、
   `suggestion_accepted` 這三個值只在人打字或按下建議時才會出現，跟字串分類衝突就代表分類錯了；
   `sdk` 不是，它在整份 session 裡固定不變，harness 注入繼承的是驅動方式而不是訊息作者。
   字串分類認出 harness 形狀時，`sdk` 保留那個分類，渲染成 compact 形式、標 `harness:`；
   分類認不出的 `sdk` 訊息才標 `user (sdk):`。`CountsAsTurn()` 比照同一條界線：
   `sdk` 上帶 ADR-008 判定不算一輪的形狀（`interrupted`、`agents-stopped`、stop hook、skill 注入等）時，
   跟著形狀的判定走，不再套用「帶 `promptSource` 就一定算一輪」的通則。

   這條規則原本寫的是 `sdk` 也適用覆蓋（實測 0 衝突），v0.1.76（PR #13）依此實作後，
   sdk 來源中有 harness 形狀的訊息以 v0.1.76 實測會被展開成全文，標成 `user (sdk):`。
5. **2.1.165 以前的 transcript 走現行路徑**，不變。

## 不做的

**不把「沒帶 `promptSource`」直接當成 harness。** `[Image: …]` 佔位符沒帶但屬於使用者訊息，
`The user sent a new message while you were working:` 沒帶但 body 是人打的。
沒帶只代表「不是啟動 turn 的 prompt」，是不是人講的還要看內容。

**不用 `isMeta` 當第二來源。** 它在 skill 注入、圖片佔位符、stop hook feedback 上都出現，語意是「附屬」而不是「harness」。

## 預期效果

- `harness:` 標籤多抓到 15 則現在判不出來的排程 prompt
- 之後 harness 改措辭，影響的只剩 compact 形式（會退化成全文渲染），不再影響角色標籤與 K
- K 不變：帶 `promptSource` 的訊息在現行政策下本來就幾乎都算 turn（`system` 那組 91% 是 task-notification，ADR-008 已算）

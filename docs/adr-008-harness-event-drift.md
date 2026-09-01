# ADR-008：追上 harness 事件格式的漂移，並把 turn 計數從字元統計裡拆出來

**狀態**：已實作。

Reader 靠比對字面字串認出 Claude Code 寫進 transcript 的事件。那些字串會變，而且已經變過了。
盤點最近 60 天的 120 個 transcript（35,940 筆 entry）發現三類落差，這份 ADR 記錄五項決定。

| # | 決定 | 影響 |
|---|------|------|
| 1 | `noiseTypes` 補上 8 個 entry type | 597 KB 從「沒被解析」變成 `system_noise` |
| 2 | 6 種 harness 注入的 user 訊息改在 parser 層分類 | 不再冒充使用者訊息 |
| 3 | 新增 `harness:` 角色標籤 | 讀的人分得出哪幾行是人打的 |
| 4 | teammate 偵測改成只認 XML 標籤，兩種變體都收 | 主判斷已死，改認標籤後散文重寫不再影響偵測 |
| 5 | 拆出 `UserMessage.CountsAsTurn()`，內容依實測校準 | 八個 session 的 turn 計數誤差 70 → 29 |
| 6 | skill 注入改走 `sourceToolUseID` 結構連結認定 | 沒有 base-directory 行的 bundled skill 不再全文渲染 |

量測基礎：`~/.claude/projects` 下依修改時間取最新 120 個 `.jsonl`（2026-08-30 往前 60 天）。

## 1. `noiseTypes` 漏了 8 個 CLI 後來才加的 entry type

`noiseTypes` 是手寫的白名單，收 13 個型別。實測出現、不在名單裡的有 8 個：

| type | 筆數 | 位元組 |
|---|---:|---:|
| `atis-latch` | 1,600 | 191,680 |
| `frame-link` | 664 | 96,167 |
| `worktree-state` | 285 | 115,708 |
| `file-history-delta` | 168 | 80,618 |
| `artifact-autoreact-ledger` | 159 | 80,936 |
| `artifact-comment-monitor` | 49 | 17,522 |
| `agent-setting` | 37 | 3,947 |
| `cost-state` | 14 | 10,608 |
| 合計 | 2,976 | 597,186 |

這些型別都沒有 `message` 欄位，所以 `parseLineWithToolNames` 走到
`raw.Message == nil` 那條路徑，型別不在白名單就 `return session.Event{}, false, nil`，
整行不產生 Event。

**輸出沒有錯**：不管認不認得，它們都不會出現在 filtered text 裡。
錯的是統計：`EventNoise` 會把字元累進 `analyzer` 的 `system_noise`，
fall-through 不會，這 597 KB 在分類統計裡是隱形的。

決定：補進白名單。這是白名單設計的固有成本：每次 CLI 加型別就要補一次。
沒有改成「反向白名單」（只列出要解析的型別，其餘一律 noise），
因為那會讓一個真的帶對話內容的新型別被靜默吃掉，那個失敗方向比統計少算嚴重得多。

`cost-state` 帶 `totalCostUSD` 和 `modelUsage`，正好是 benchmark 現在靠 API usage
逐筆重算的東西。這次只把它歸為 noise，沒有拿來用；要用是另一件事。

## 2. 6 種 harness 注入的 user 訊息完全沒分類

`classifyHarnessUserMessage` 認得 system-reminder、skill 注入、teammate、
context usage、command 注入。實測還有 6 種沒認得：

| 訊息開頭 | 則數 | 字元 | 平均 |
|---|---:|---:|---:|
| `<task-notification>` | 64 | 147,340 | 2,302 |
| `This session is being continued…` | 13 | 190,493 | 14,653 |
| `[Request interrupted by user` | 13 | 403 | 31 |
| `N background agents were stopped…` | 4 | 1,605 | 401 |
| `A session-scoped Stop hook is now active` | 2 | 1,034 | 517 |
| `Skill /X was loaded earlier…` | 2 | 357 | 178 |

`<task-notification>` 是特例：formatter 認得它、會壓成 `[summary]`，但 parser 不認得。
`classify.go` 開頭寫著「detection lives here in the parser layer so the formatter and
stats consumers branch on domain fields, never re-match tags」，
而 `render.go` 對原文再比對一次 `strings.Contains(text, "<task-notification>")`。
這次把它移到 parser，formatter 改判 flag。

各自的 compact 形式，沿用現有的 `[kind: id]` 慣例：

| 訊息 | 呈現 | 字元 |
|---|---|---:|
| Stop hook | `[goal] <條件>` | 517 → 50 |
| agents stopped | `[agents stopped: 7]` | 401 → 19 |
| Skill 重複載入 | `[skill: X] (repeat)`（走既有的 skill 路徑） | 178 → 33 |
| Request interrupted | `[interrupted]` | 31 → 13 |
| 壓縮續接 | `[compaction summary]` + 全文 | 14,653 → 14,320 |
| task-notification | 維持既有壓縮，只改角色標籤 | 不變 |

**壓縮續接訊息的 body 全留**。那是前一段對話的摘要，正是繼承 context 的人要的東西；
只剝掉開頭三句 harness 框架和結尾的「read the full transcript at」指示。

**這不是為了省 token**。上表六種加起來 60 天只省約 7,300 字元，
攤到 120 個 session 是每個約 60 字元。價值在第 3 項和第 5 項。

## 3. 新增 `harness:` 角色標籤

改之前所有 user-role entry 都印成 `user:`，不管是人打的還是 harness 塞的。
繼承這段 context 的 Claude 沒有辦法分辨。實測這個 session（`b11858cf`）：
46 則標成 `user:`，其中 10 則不是人打的。

改之後 36 則 `user:` + 10 則 `harness:`，總數不變。

`context` 格式的前綴同步從 `U:` 分出 `H:`。

字要叫什麼考慮過 `system:` 和 `hook:`。選 `harness:` 是因為
`system` 在 transcript 裡已經是一個 entry type（`system/compact_boundary` 等），
再借來當角色標籤會有兩個意思；`hook` 只涵蓋六種裡的一種。

## 4. teammate 的主判斷已經是死碼

```go
teammateWarning = "IMPORTANT: This is NOT from your user"
```

60 天內 133 則 teammate message，**這個字串命中 0 則**。
harness 改寫成「This came from another Claude session — not typed by your user…」了。

現在還認得出來，純粹靠第二個 fallback 分支比對開場白
`Another Claude session sent a message:`，那條 133/133 全中。
也就是整個 teammate 偵測吊在單一條件上。開場白哪天再改一次，
teammate message 就會整個變成一般 user 訊息：全文渲染、樣板不砍。
turn 數會剛好還是對的（一般訊息本來就算一個 turn），所以這個故障不會反映在 K 上。

第一版決定（改成一組標記，開場白或任一版免責聲明命中即可）已經不夠：
60 天內 1,112 則 teammate message 有 80 則用 `<agent-message>` 標籤而非
`<teammate-message>`，開場白和免責聲明都沒有附上，兩個標記都落空。
80 則裡有 3 則（35 個樣本中）連屬性都沒有。

改成只認 XML 標籤本身，不再看散文。開場白、免責聲明都是 harness 生成給人看的
說明文字，harness 會重寫；標籤是 harness 自己要解析的結構，重寫成本高得多。
兩種標籤都收：`<teammate-message>`（發送端 id 放在 `teammate_id`）與
`<agent-message>`（放在 `from`，可能整個不帶屬性）。標籤清單收在
`session.TeammateTagVariants`，`classify.go` 和 `CompactTeammateMessage`
共用同一份，避免兩處各自列一次而漂移。下次改字只有改到標籤名稱才會讓偵測失效。

`CompactTeammateMessage` 裡剝除警告的那段同理也是死碼，但無害：
抽取迴圈只取標籤之間的內容，尾巴的警告本來就進不來。這次沒動它。

## 5. 一個 flag 同時回答了兩個不相干的問題

`ComputeStats` 的 user 分支同時做兩件事：分類字元（哪些被砍、哪些被留），
以及數 turn。分類用 `continue` 提早跳出，而 `userTurnCount++` 寫在迴圈末尾，
於是**每一個為了字元統計寫的 `continue`，都順手把 turn 也跳掉了**。

對 system-reminder、command 輸出是對的。對 teammate message 是錯的。

K = API call 數 ÷ turn 數，`cost.go` 用 `extraCallsPerTurn(K) = K − 1` 算
「一輪之內除了首呼叫還要再打幾次」。所以 turn 的定義是
**一個由外部 prompt 啟動、跑到 agent 停下為止的工作單位**。
teammate message 啟動的正是這種單位。

決定性的理由不是「teammate message 像不像使用者」，是分子分母必須數同一群：
`apiCallCount` 對每一則 usage 不同的 assistant 訊息都加一，不管那一輪是誰觸發的，
teammate message 引發的 API call 全在分子裡。
改之前的 K 是「全部的 API call ÷ 只有人類發起的 turn」。

`fc65aeeb` 是 165 次 call ÷ 4 個 turn = 41.2，模型於是認為每輪要重讀 prefix 40 次；
它實際有 11 則 teammate message。

決定：把政策收進 `UserMessage.CountsAsTurn()`，在字元統計的分支之前就決定完，
不再靠 `continue` 的副作用。

### 哪幾種算，用量的不用推的

第一版政策是推理出來的：teammate 算，其餘 harness 注入一律不算。
那一版比不改還糟（見下面的校準表）。

改成量。對每一則 harness 訊息，往後掃到下一則真正的 user 訊息為止，
看中間有沒有出現帶 usage 的 assistant 訊息，也就是這則訊息有沒有把 agent 叫起來做事：

| 種類 | 啟動一輪 | 沒有 | 比例 |
|---|---:|---:|---:|
| teammate | 128 | 8 | 94% |
| compaction 續接 | 12 | 0 | 100% |
| task-notification | 63 | 8 | 89% |
| stop hook | 2 | 0 | 100% |
| skill 重複載入 | 3 | 0 | 100% |
| interrupted | 0 | 14 | 0% |
| agents stopped | 0 | 4 | 0% |

stop hook 和 skill 重複載入雖然 100%，但這個測試對它們無效：
它們是某個已經由別的 entry 啟動的 invocation 的尾巴（`/goal` 的 marker 在前兩筆），
測試分不出「這則啟動了工作」和「這則只是工作開始前的最後一筆」。
teammate、compaction、task-notification 沒有這個問題，它們前面沒有同一輪的其他 user entry。

| 訊息種類 | 渲染 | 算 turn |
|---|---|---|
| system-reminder / context-usage | 丟棄 | 否 |
| command 輸出 / caveat | 丟棄 | 否 |
| skill / command 注入 | 壓縮 | 否（伴隨觸發它的使用者訊息出現，算了會重複） |
| stop hook / skill 重複載入 | 壓縮 | 否（同上） |
| interrupted / agents stopped | 壓縮 | 否（實測不啟動任何一輪） |
| teammate message | 壓縮 | **是** |
| task-notification | 壓縮 | **是** |
| 壓縮續接 | 保留 | **是** |
| 一般訊息 | 保留 | 是 |

### 校準

真值取「agent 被叫起來做事的次數」：掃過 transcript，
每當一則帶 usage 的 assistant 訊息前面不是另一則 assistant，就是一輪的開始。
八個 session 的累計絕對誤差：

| 政策 | 誤差 |
|---|---:|
| 改動前 | 70 |
| 第一版（只加 teammate，其餘 harness 全不算） | 81 |
| 採用的版本 | **29** |

真值本身也是啟發式的（`921fb399` 三種政策都 +12，`3017876d` 都 +9，
表示還有別的來源在多算），所以絕對數字不可當精確值，這裡看的是相對高低。

### 方向

取壓縮率相近（25–26%）的六個 session 看，cold cache 的 10-turn 節省隨 K 上升而下降
（K 2.8 → 68%，K 8.6 → 59%），所以 K 高估會**低估**效益。
同一批的 warm cache（54–57%）沒有趨勢。這是固定壓縮率區間的觀察，不是受控實驗。

## 6. skill 注入只認文字前綴，bundled skill 沒有那行

`classifyHarnessUserMessage` 認 skill 注入的條件是文字以 `Base directory for this skill:` 開頭。
bundled skill（如 `artifact-design`）的注入沒有這一行，內文直接開始，
於是整份 skill 以 `user:` 全文渲染。60 天內 50 則，約佔 skill 注入的 12%，
單一則就是數 KB；讀 `b11858cf` 時它佔了輸出的一成。

原始 JSONL 裡這類 entry 帶 `isMeta: true` 和 `sourceToolUseID`，後者指回前一則 assistant 的
`Skill` tool_use，而那個 tool_use 的 `input.skill` 就是 skill 名字。
`isMeta` 單獨不能當標記：圖片佔位符、stop hook feedback 也帶它。

決定：reader 既有的 tool_use id 對照表順便記下 Skill 的 `skill` 與 `args`，
user 訊息先查這條連結，命中就是 skill 注入；文字前綴降為備援，
讓無狀態的 `ParseLine`（沒有對照表）維持原行為。
對過真實 transcript，`input.skill` 與文字路徑從路徑抽出來的名字一致，
同一個 skill 走兩條路都能在 `seenSkills` 去重。

這是第 2 項同一種病的另一個實例：harness 早就給了結構欄位，reader 還在比對字串。
teammate message 沒有這種欄位（頂層欄位與一般 user 訊息完全相同），所以第 4 項只能留在字串比對。

## 沒有解決的

**slash / bang command 不算 turn**，維持改動前的行為。
`/goal` 這類 invocation 會觸發一整輪工作，照第 5 項的定義應該算；
但它在 transcript 裡是三筆 entry（`<command-name>` marker、`<command-message>` 注入、
可能還有 skill body），要決定哪一筆代表那個 turn 才不會重複計算。
`!` 開頭的 bang command 又不觸發 API 回合，不能跟 slash 一起處理。
這是另一個決定，不混進這次。

**`cost-state` 沒有拿來用**。它帶著 `totalCostUSD` 和 `modelUsage`，
benchmark 現在是靠 API usage 逐筆重算的。要不要改用它是另一件事。

# ADR-007：五項輸出格式改動，量過才決定

**狀態**：已實作。量測程式在 `experiment/format-probes` 分支（不合併）。

實作後對同五個 session 重新量整份 read 輸出：656,370 → 658,278 token（**+0.3%**）。
比決策時預估的 −0.4% 高，差額來自第 1 項：跳過雜訊行之後取到的下一行通常更長也更有內容
（`=== engine exports ===` 22 字元變成 `32:export function createBaseGameSnapshot(): GameSnapshotV4 {` 57 字元）。
那是這項改動要的效果，不是回歸。

取代 ADR-003 的三項決定：成功結果的摘錄取法、Bash 摘要的內容、成功狀態標記。
其餘 ADR-003 決定不變。

## 五項決定

| # | 改動 | token 影響 |
|---|------|-----------:|
| 1 | 成功結果的摘錄套用與失敗路徑同一套雜訊過濾，並剝掉 ANSI | 內容變動，實測整體 +0.7% |
| 2 | Bash 摘要加上指令動詞（跳過鋪陳後的「程式 + 第一個參數」，上限 30 字元） | +1.9% |
| 3 | 成功不再標 `-> ok`，只標 `-> FAILED` | −2.0% |
| 4 | 訊息時間戳改成 `[HH:MM:SS]`，換日時插一行 `--- YYYY-MM-DD ---` | −0.4% |
| 5 | 分頁上限從 20,000 提到 28,000 bytes | 頁數 −27% |

第 2 項和第 3 項綁在一起做，合計 **−0.0%**。

量測方法：`cc-session formatbench`，對 5 個 33K 到 389K token 的 session 渲染每個變體，
各送一次 Anthropic token counting API。表中是五個 session 的平均。

## 1. 成功結果的摘錄要過濾雜訊，但不改成取最後一行

`ToolResult.Summary()` 目前兩條路徑不對稱：失敗走 `firstMeaningfulErrorLine`，
會跳過 cat 行號、裸 `Exit code N`、hook 樣板；成功只走 `FirstLine`，取第一個非空行，零過濾。

後果是讀者拿到橫幅當結論：

```
[Bash#Gj9T] 查 PR #407 狀態與 CI -> ok: Work seamlessly with GitHub from the command line.
[Bash#XhoN] 檢查 agent 中斷後的工作區狀態 -> ok: --- HEAD ---
[Bash#tQvn] 跑新增的 archetype 測試 -> ok: \x1b[1m\x1b[46m RUN \x1b[49m\x1b[22m \x1b[36mv4.0.18
```

第一行是 `gh` 的說明橫幅，讀的人會以為那就是 PR 狀態。第二行是腳本自己 echo 的區段標題。

掃 1,391 個成功且有輸出的 Bash 結果，**第一行有 31.9% 命中雜訊樣式**：

| 樣式 | 佔比 |
|------|-----:|
| 腳本 echo 的區段標題（`=== x ===`、`--- x ---`） | 13.7% |
| 程式碼片段（兩格以上縮排開頭） | 8.4% |
| ANSI 色碼開頭 | 6.1% |
| 進度或狀態前綴（`[STARTED]`、`Checking `、`> `） | 3.3% |
| 版本橫幅 | 0.4% |
| 至少命中一種 | **31.9%** |

改法是把成功路徑接上 `isNoiseExcerptLine`，補進上表的橫幅與進度樣式，
並在取摘錄前套 `session.StripANSI`。跳過雜訊行後取下一行，不是整段丟掉。

實作後在那個 1,258 個 Bash 呼叫的 session 上：292 行（23%）拿到更好的摘錄，
沒有任何一行失去摘錄，帶 ANSI 的行從 95 降到 0。
23% 低於上表的 31.9%，因為實作的規則比量測時的樣式嚴格：
區段標題要頭尾同一種符號才算（`--- HEAD ---` 算，diff 的 `--- a/file.go` 不算），
縮排的程式碼片段不跳過（跳過只會換到另一段程式碼，還可能丟掉答案）。

一個抓不到的情況：純散文的說明橫幅沒有可辨識的形狀，
`gh` 的 `Work seamlessly with GitHub from the command line.` 過濾不掉。
那個案例靠第 2 項緩解：同一行有 `| gh pr`，讀者看得出橫幅不是答案。

### 不改成取最後一行

抽樣 1,060 個成功的多行 Bash 結果，逐筆比對第一行與最後一行，
最後一行沒有系統性比較好：大量是 `  })`、diff 尾巴、程式碼片段。

```
DESC  讀 VM 要改的區段   (44 lines)
  1st '  const snapshot = useMemo<GameSnapshotV4>('
  last'    transitionLockRef.current = true'
DESC  看剩餘兩處改動   (58 lines)
  1st 'diff --git a/src/features/nccuLifeSimulator/components/LifeSimulatorCo'
  last'   })'
```

位置換不出正確性，過濾才行。

## 2. Bash 摘要要帶指令動詞，而抽取方式決定它便不便宜

`summarizer.go` 目前有 description 就只印 description，command 整個丟掉。
description 是當時那個 Claude 寫給人看的轉述，讀的人無法確認實際跑了什麼。
Bash 佔範例 session 1,604 個 tool 呼叫裡的 1,258 個（78%），
等於這個 session 大部分的動作都不可驗證。

直接接上指令前綴很貴，因為預算會花在鋪陳上。實測 20 字元經常整段被吃掉：

```
[Bash#KPso] 更新 #423 內文 | cd /Users/maple/Desk
[Bash#6yGZ] 驗證兩處測試斷言 | echo "=== game-engin
```

改成掃過 `cd`、環境變數指派、`export`、`echo` 這些片段，
取第一個真正的程式名加它的第一個參數：

```
[Bash#TV5d] 用型別檢查找出所有斷掉的引用 | pnpm tsc: src/...(19,3): error TS24
[Bash#DK5W] Force-push rebased bento branch | git push: To https://github.com/...
[Bash#MS6r] 看索引那一行 | grep -n: 71:- [教學中 Joyride overlay 攔截底部按鈕...]
```

動詞涵蓋率 1,261 行中 1,258 行（99.8%），成本減半：

| 抽取方式與長度 | token |
|----------------|------:|
| 原始前綴 20 字元 | +3.7% |
| 原始前綴 60 字元 | +9.8% |
| 動詞抽取 20 字元 | +1.9% |
| 動詞抽取 30 字元 | +2.0% |

30 字元和 20 字元幾乎同價，因為抽出來的動詞很少超過 20 字元。取 30，截斷少一點。

實作後 1,258 個 Bash 呼叫全部抽到動詞。除了 `cd`、環境變數、`echo`，
還要跳過 shell 控制關鍵字（`until`、`while`、`for`），
否則 `until gh pr checks` 會把 `until` 當成程式名（實測有 30 行）。

已知瑕疵，實作時一併處理：`/opt/homebrew/bin/gh`、`$HOME/.nvm/versions/` 這類
絕對路徑程式名會吃掉預算，取 basename。

## 3. 成功不標記，只標失敗

範例 session 的 1,604 個 tool 摘要裡 1,550 個 ok、41 個 FAILED、13 個沒有結果。
成功是預設狀況，每行講一遍等於對每一行課稅。其中 96 行是連摘錄都沒有的裸 `-> ok`。

保留 `-> FAILED`、成功不標，語意完全相同，省 2.0%。

`formatRetryCollapse` 靠 summary 字串裡的 `FAILED` 字樣插入 `×N`，要一起改。

成功行去掉標記後變成 `描述: 摘錄`。實作時保留 `->` 作為分隔
（`描述 -> 摘錄`），成本差極小，但讀者一眼分得出哪裡是呼叫、哪裡是結果。

## 4. 時間戳改成時鐘含秒加換日標記

現行 `01-02 15:04` 每則訊息重複日期，而且沒有年份。
`context` 還有 session header 可以推年份，`read` 沒有 header，完全沒有線索。

浪費的是每行重複的日期，不是時間本身。把日期抽成換日標記後，
省下來的空間足以換到秒精度，總計還是省的：

| 方案 | 樣子 | token |
|------|------|------:|
| 現行 | `[08-06 03:06]` | — |
| 加年份 | `[2026-08-06 03:06]` | +0.6% |
| 加秒 | `[08-06 03:06:15]` | +0.4% |
| 時鐘 + 換日標記 | `[03:06]` 加 `--- 2026-08-06 ---` | −0.8% |
| **時鐘含秒 + 換日標記** | `[03:06:15]` 加 `--- 2026-08-06 ---` | **−0.4%** |
| 相對分鐘 | `[+83]` | −1.0% |

選時鐘含秒：比現行多了秒和年份，還便宜 0.4%。

相對分鐘最便宜但不選：跟外部 log（CI、伺服器 log）對時間時要先做一次換算。

換日標記是必要的。實測 session 真的跨天：兩個樣本一個跨 3 天、一個跨 2 天。
但很稀疏，1,808 個事件的 session 只觸發兩次。

```
--- 2026-08-06 ---

[03:06:15] user:
...
--- 2026-08-07 ---

[02:08:31] user:
```

## 5. 分頁上限提到 28,000 bytes

`maxPageBytes = 20_000` 的理由寫在 `internal/inject/inject.go`：
「怕超過就被 Claude Code 的 Bash tool 寫成檔案而不是回傳 stdout」。

二分實測那個門檻：

| 輸出 bytes | 結果 |
|-----------:|------|
| 30,000 | 完整 inline 回傳 |
| 31,000 | 寫成檔案，只回 2KB 預覽 |
| 32,768 | 寫成檔案 |
| 40,000 | 寫成檔案 |
| 50,000 | 寫成檔案 |

門檻是 30,000 字元。扣掉 page marker 與 footer 的空間，取 28,000。
那個 871KB 的 session 從 44 頁降到 32 頁，往返少 27%。實測最大的一頁 28,073 bytes。

**這個常數綁在 harness 的行為上，不是 cc-session 自己說了算。**
只驗證了這一版 Claude Code，而該上限使用者可以調整。
實作時把 30,000 這個實測值寫進常數旁的註解，日後 harness 改了才知道要重測。

## 尚未決定

跨 session 搜尋（`cc-session search`）確認要做，但介面與輸出格式還沒定，
另立 ADR。已確定的一點：比對要在渲染後的過濾文字上做，
不能 shell out 給 `grep`：那份文字是 renderer 在記憶體裡產生的，磁碟上不存在。
`grep` 只搜得到原始 JSONL，那正是這個功能要取代的做法。

## 這些數字的界線

百分比來自 5 個 session 的平均，最小 33K token、最大 389K token。
第 2、3 項的比例跟 Bash 呼叫佔比高度相關，Bash 少的 session 影響會小
（樣本裡 630f4c64 的每一項都明顯低於其他四個）。
第 1 項的 31.9% 來自兩個 session 的 1,391 筆結果。
第 5 項的門檻只驗證了一版 harness。

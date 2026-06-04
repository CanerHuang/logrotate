# logrotate

以 [phuslu/log](https://github.com/phuslu/log) 為基礎的輕量 logging 套件,提供:

- **檔案輪替**：單檔超過上限自動 rotate,並保留指定數量的舊檔。
- **動態 UDP fan-out**:在程式執行中隨時新增/移除 UDP 輸出目標,把 log 同時轉送到遠端收集器(log server、SIEM…)。
- **非同步轉發**:UDP 送出走背景 goroutine,網路 I/O 不會拖慢應用程式的 logging 路徑。
- **無鎖讀路徑**:fan-out 用 `atomic.Pointer` 快照 + copy-on-write,高頻寫 log 不碰 mutex。

`import` 後即可直接使用套件層級的 `logrotate.Info()` 等函式,無需自行建立 logger。

## 安裝

```bash
go get github.com/CanerHuang/logrotate
```

需求:Go 1.26+。

## 快速開始

不做任何設定就能用,預設會寫到當前目錄的 `app.log`:

```go
package main

import "github.com/CanerHuang/logrotate"

func main() {
	defer logrotate.Close() // 程式結束前 flush 緩衝、關閉檔案與連線

	logrotate.Info().Str("module", "main").Msg("service started")
	logrotate.Warn().Int("retry", 3).Msg("downstream slow")
	logrotate.Error().Err(err).Msg("request failed")
}
```

寫 log 的鏈式 API 直接沿用 phuslu/log 的 `*log.Entry`,常用方法:`Str`、`Int`、`Bool`、`Float64`、`Err`、`Msg`、`Msgf`…

## 自訂設定

從 `DefaultConfig()` 拿一份預設值,改你在意的欄位後傳給 `Init()`。**請在開始寫 log 之前呼叫**(例如 `main` 的開頭):

```go
cfg := logrotate.DefaultConfig()
cfg.Filename = "logs/myapp.log"
cfg.MaxSize = 50 << 20 // 50 MiB 輪替一次
cfg.MaxBackups = 10    // 保留 10 個舊檔
cfg.Level = log.InfoLevel

logrotate.Init(cfg)
defer logrotate.Close()
```

### Config 欄位

| 欄位 | 型別 | 預設 | 說明 |
| --- | --- | --- | --- |
| `Filename` | `string` | `app.log` | 輸出檔名 |
| `MaxSize` | `int64` | `1 << 20` (1 MiB) | 單檔最大位元組,超過即輪替 |
| `MaxBackups` | `int` | `7` | 保留的舊檔數量 |
| `LocalTime` | `bool` | `false` | 輪替檔名是否用本地時間(否則 UTC) |
| `Level` | `log.Level` | `InfoLevel` | 最低輸出等級 |
| `EnsureFolder` | `bool` | `true` | 寫檔前自動建出上層目錄(0755) |
| `ChannelSize` | `uint` | `4096` | UDP 非同步緩衝長度 |
| `DiscardOnFull` | `bool` | `true` | 緩衝滿時丟棄新 entry(`false` 則背壓等待) |

> `Init()` 中字串/數值欄位若為零值,會自動套回 `DefaultConfig()` 的對應預設值。

## 動態轉發到 UDP

在執行中把 log 同時送往遠端,支援 JSON 與純文字兩種格式,可隨時增減:

```go
// 轉送 JSON(適合給 log server / 結構化收集器)
logrotate.AddJSONUDP("10.0.0.5:5140")

// 轉送純文字(格式與檔案一致:<time> <LEVEL> <message> key=value …)
logrotate.AddTextUDP("10.0.0.6:5141")

logrotate.Info().Str("ev", "login").Msg("user signed in") // 同時寫檔 + 送兩個 UDP

logrotate.Targets()              // => ["10.0.0.5:5140", "10.0.0.6:5141"]
logrotate.Remove("10.0.0.5:5140") // 移除單一目標
```

所有 UDP 目標共用一個 send-only socket,增減目標只動內部清單,不開關連線。以相同 `addr` 重複 `Add` 會直接覆蓋舊設定。

### 觀測轉發錯誤

單一 target 寫失敗不會中斷其他 target,預設靜默。需要觀測時設定 callback:

```go
logrotate.SetOnError(func(key string, err error) {
	// 注意:此 callback 在背景寫 log 的 goroutine 上同步呼叫,
	// 不要在裡面再寫同一個 logger,以免遞迴。
	fmt.Fprintf(os.Stderr, "udp target %s failed: %v\n", key, err)
})
```

## 關閉

`Close()` 會先排空非同步緩衝(沖出仍在 channel 裡的 entry)、關閉 UDP 連線,再 flush/關閉檔案。建議 `defer logrotate.Close()`。Close 後若要再記錄,需重新 `Init()`。

## 進階存取

需要更底層的物件時:

```go
logrotate.Logger()  // *log.Logger,需要把 logger 傳進其他套件時用
logrotate.Default() // *DynamicWriter,需要更細的 target 操作時用
```

## 設計重點

- **讀寫分離**:`WriteEntry`(讀路徑)無鎖,用 atomic 快照載入目標清單;`Add`/`Remove`(寫路徑)用 copy-on-write,mutex 只序列化彼此,不擋寫 log。
- **檔案 vs UDP 分流**:檔案輸出是同步寫(Linux 上走 `writev`,夠快),底層 `FileWriter` 負責 size-cap 輪替;UDP fan-out 走 `AsyncWriter` 背景 goroutine。
- **錯誤隔離**:任一 target 寫失敗只透過 `OnError` 觀測,不向上拋、不影響其他 target。

## License

MIT

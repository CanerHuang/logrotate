package logrotate

import "github.com/phuslu/log"

// 套件層級的預設實例:import 後即可直接用 logrotate.Info()…
//
//   - std        預設 logger(寫檔 + 非同步 UDP fan-out)。
//   - defaultDyn 預設動態 writer,對應 AddJSONUDP() 等轉發函式。
//   - asyncW     把 UDP fan-out 移到背景 goroutine,不拖累 app 的 logging 路徑。
//   - fileW      由 newLogger 設定,供 Close 時 flush/關閉。
//
// 不呼叫 Init 也能用,會落在 DefaultConfig() 的預設值。
var (
	defaultDyn = NewDynamicWriter()
	asyncW     = newAsync(defaultConfig)
	fileW      *log.FileWriter
	std        = newLogger(defaultConfig)
)

// Config 設定預設 logger 的寫檔行為、等級,以及 UDP 非同步緩衝。
type Config struct {
	Filename   string    // 輸出檔名
	MaxSize    int64     // 單檔最大位元組,超過即輪替
	MaxBackups int       // 保留的舊檔數量
	LocalTime  bool      // 輪替檔名是否用本地時間
	Level      log.Level // 最低輸出等級

	// EnsureFolder 為 true 時,寫檔前自動建出 Filename 的上層目錄(0755)。
	EnsureFolder bool

	// ChannelSize 是 UDP 非同步緩衝的長度。
	ChannelSize uint
	// DiscardOnFull 為 true 時緩衝滿就丟棄新 entry(永不阻塞);false 則背壓等待。
	DiscardOnFull bool
}

var defaultConfig = Config{
	Filename:      "app.log",
	MaxSize:       1 << 20,
	MaxBackups:    7,
	LocalTime:     false,
	Level:         log.InfoLevel,
	EnsureFolder:  true,
	ChannelSize:   4096,
	DiscardOnFull: true,
}

// DefaultConfig 回傳一份帶預設值的 Config,建議從這裡開始改你在意的欄位。
func DefaultConfig() Config { return defaultConfig }

func newAsync(c Config) *log.AsyncWriter {
	return &log.AsyncWriter{
		Writer:        defaultDyn,
		ChannelSize:   c.ChannelSize,
		DiscardOnFull: c.DiscardOnFull,
	}
}

func newLogger(c Config) *log.Logger {
	fileW = &log.FileWriter{
		Filename:     c.Filename,
		MaxSize:      c.MaxSize,
		MaxBackups:   c.MaxBackups,
		LocalTime:    c.LocalTime,
		EnsureFolder: c.EnsureFolder,
	}
	return &log.Logger{
		Level: c.Level,
		Writer: &log.MultiEntryWriter{
			// 檔案輸出走人類可讀文字,底層由 FileWriter 負責 size-cap 輪替;同步寫。
			&log.ConsoleWriter{
				ColorOutput: false,
				Formatter:   textFormatter,
				Writer:      fileW,
			},
			asyncW, // UDP fan-out 走非同步
		},
	}
}

// Init 以自訂設定重建預設 logger,請在開始寫 log 之前呼叫。
// 字串/數值欄位若為零值會自動套用 DefaultConfig() 的對應預設。
func Init(c Config) {
	if c.Filename == "" {
		c.Filename = defaultConfig.Filename
	}
	if c.MaxSize == 0 {
		c.MaxSize = defaultConfig.MaxSize
	}
	if c.MaxBackups == 0 {
		c.MaxBackups = defaultConfig.MaxBackups
	}
	if c.ChannelSize == 0 {
		c.ChannelSize = defaultConfig.ChannelSize
	}
	asyncW = newAsync(c)
	std = newLogger(c)
}

// SetOnError 設定預設 DynamicWriter 的錯誤觀測 callback。
func SetOnError(fn func(key string, err error)) {
	defaultDyn.OnError = fn
}

// Default 回傳預設的 DynamicWriter。
func Default() *DynamicWriter { return defaultDyn }

// Logger 回傳預設 logger。
func Logger() *log.Logger { return std }

// ---- 動態 target 轉發 ----

func AddJSONUDP(addr string) error { return defaultDyn.AddJSONUDP(addr) }
func AddTextUDP(addr string) error { return defaultDyn.AddTextUDP(addr) }
func Remove(key string)            { defaultDyn.Remove(key) }
func Targets() []string            { return defaultDyn.Targets() }

// Close 先排空非同步緩衝並關閉 UDP 連線,再 flush/關閉檔案。Close 後需重新 Init 才能再記錄。
func Close() error {
	errAsync := asyncW.Close()
	errFile := fileW.Close()
	if errAsync != nil {
		return errAsync
	}
	return errFile
}

// ---- 寫 log 轉發 ----

func Trace() *log.Entry { return std.Trace() }
func Debug() *log.Entry { return std.Debug() }
func Info() *log.Entry  { return std.Info() }
func Warn() *log.Entry  { return std.Warn() }
func Error() *log.Entry { return std.Error() }
func Fatal() *log.Entry { return std.Fatal() }
func Panic() *log.Entry { return std.Panic() }

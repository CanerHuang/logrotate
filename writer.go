// Package logrotate 提供以 phuslu/log 為基礎、可在執行中動態增減輸出目標的
// log.Writer。讀路徑無鎖,寫路徑 copy-on-write。
package logrotate

import (
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/phuslu/log"
)

// udpTarget 把每次 Write 當成一個 datagram 送到固定 addr,所有 target 共用一個 socket。
type udpTarget struct {
	conn *net.UDPConn
	addr *net.UDPAddr
}

func (u udpTarget) Write(p []byte) (int, error) { return u.conn.WriteToUDP(p, u.addr) }

type target struct {
	key    string
	writer log.Writer
}

// DynamicWriter 是可在執行中動態增減目標的 log.Writer。
//
//   - 讀路徑(WriteEntry)無鎖,用 atomic.Pointer 載入目標快照。
//   - 寫路徑(Add/Remove)用 copy-on-write,mu 只序列化彼此,不擋讀。
//   - 所有 UDP target 共用一個 send-only socket。
//   - 單一 target 寫失敗不中斷其他 target,錯誤可選擇性丟給 OnError。
type DynamicWriter struct {
	mu      sync.Mutex
	conn    *net.UDPConn
	targets atomic.Pointer[[]target]

	// OnError 在某個 target 寫入失敗時被呼叫(nil = 靜默)。會在寫 log 的
	// goroutine 上同步呼叫,不要在裡面再寫同一個 logger。
	OnError func(key string, err error)
}

func NewDynamicWriter() *DynamicWriter {
	d := &DynamicWriter{}
	empty := make([]target, 0)
	d.targets.Store(&empty)
	return d
}

// WriteEntry 實作 log.Writer,無鎖讀取快照後 fan-out。
func (d *DynamicWriter) WriteEntry(e *log.Entry) (n int, err error) {
	p := d.targets.Load()
	if p == nil {
		return 0, nil
	}
	for _, t := range *p {
		m, werr := t.writer.WriteEntry(e)
		if werr != nil {
			if d.OnError != nil {
				d.OnError(t.key, werr)
			}
			continue
		}
		n = m
	}
	return n, nil
}

// ensureConn 惰性建立共用的 send-only UDP socket。
func (d *DynamicWriter) ensureConn() (*net.UDPConn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn == nil {
		conn, err := net.ListenUDP("udp", nil)
		if err != nil {
			return nil, err
		}
		d.conn = conn
	}
	return d.conn, nil
}

// add 複製現有清單、移除同 key 舊項、附加新項、原子替換。
func (d *DynamicWriter) add(t target) {
	d.mu.Lock()
	defer d.mu.Unlock()

	cur := *d.targets.Load()
	next := make([]target, 0, len(cur)+1)
	for _, old := range cur {
		if old.key == t.key {
			continue // 同 key 用新的取代
		}
		next = append(next, old)
	}
	next = append(next, t)
	d.targets.Store(&next)
}

// AddJSONUDP 新增一個輸出 JSON 的 UDP target。
func (d *DynamicWriter) AddJSONUDP(addr string) error {
	uaddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}
	conn, err := d.ensureConn()
	if err != nil {
		return err
	}
	d.add(target{
		key:    addr,
		writer: &log.IOWriter{Writer: udpTarget{conn: conn, addr: uaddr}},
	})
	return nil
}

// AddTextUDP 新增一個輸出純文字的 UDP target,格式與檔案一致(textFormatter)。
func (d *DynamicWriter) AddTextUDP(addr string) error {
	uaddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}
	conn, err := d.ensureConn()
	if err != nil {
		return err
	}
	d.add(target{
		key: addr,
		writer: &log.ConsoleWriter{
			ColorOutput: false,
			Writer:      udpTarget{conn: conn, addr: uaddr},
			Formatter:   textFormatter,
		},
	})
	return nil
}

// Remove 移除指定 key 的 target。
func (d *DynamicWriter) Remove(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	cur := *d.targets.Load()
	next := make([]target, 0, len(cur))
	for _, t := range cur {
		if t.key == key {
			continue
		}
		next = append(next, t)
	}
	d.targets.Store(&next)
}

// Targets 回傳目前的 target keys(快照)。
func (d *DynamicWriter) Targets() []string {
	cur := *d.targets.Load()
	keys := make([]string, 0, len(cur))
	for _, t := range cur {
		keys = append(keys, t.key)
	}
	return keys
}

// Close 清空所有 target 並關閉共用 socket。
func (d *DynamicWriter) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	empty := make([]target, 0)
	d.targets.Store(&empty)
	if d.conn != nil {
		err := d.conn.Close()
		d.conn = nil
		return err
	}
	return nil
}

// textFormatter 是檔案與文字 UDP target 共用的人類可讀格式:
//
//	<time> <LEVEL> <message> key=value …
//
// 一次組好整行再單次寫出,避免對 FileWriter 多次 Write。
func textFormatter(w io.Writer, a *log.FormatterArgs) (int, error) {
	var b strings.Builder
	b.WriteString(a.Time)
	b.WriteByte(' ')
	b.WriteString(strings.ToUpper(a.Level))
	b.WriteByte(' ')
	b.WriteString(a.Message)
	for _, kv := range a.KeyValues {
		b.WriteByte(' ')
		b.WriteString(kv.Key)
		b.WriteByte('=')
		b.WriteString(kv.Value)
	}
	b.WriteByte('\n')
	return io.WriteString(w, b.String())
}

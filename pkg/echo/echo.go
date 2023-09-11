package echo

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/lxzan/concurrency"
	"github.com/lxzan/gws"
	"github.com/lxzan/wsbench/internal"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const M = 10000

var (
	Serial      = int64(0) // 序列号
	Urls        []string   // 服务器地址列表
	Compress    bool       // 是否压缩
	PayloadSize int        // 载荷大小
	NumClient   int        // 客户端数量
	NumMessage  int64      // 消息数量
	Output      string     // 输出JSON文件目录
	Stats       [M]uint64  // 统计
	Payload     []byte     // 载荷
	Concurrency int        // 单连接并发度
)

func SelectURL() string {
	nextId := atomic.AddInt64(&Serial, 1)
	return Urls[nextId%int64(len(Urls))]
}

func NewCommand() *cli.Command {
	return &cli.Command{
		Name: "echo",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:    "u",
				Aliases: []string{"urls"},
				Usage:   "server address",
			},
			&cli.IntFlag{
				Name:        "c",
				Usage:       "number of multiple requests to make at a time",
				DefaultText: "100",
				Value:       100,
				Aliases:     []string{"connection"},
			},
			&cli.IntFlag{
				Name:        "n",
				Usage:       "number of requests per connection to perform",
				DefaultText: "10000",
				Value:       10000,
				Aliases:     []string{"message_num"},
			},
			&cli.IntFlag{
				Name:        "p",
				Usage:       "payload Size",
				DefaultText: "4000",
				Value:       4000,
				Aliases:     []string{"payload_size"},
			},
			&cli.StringFlag{
				Name:        "f",
				Usage:       "load payload content from file",
				DefaultText: "",
				Aliases:     []string{"file"},
			},
			&cli.BoolFlag{
				Name:        "compress",
				Usage:       "whether to turn on compression",
				DefaultText: "false",
				Value:       false,
			},
			&cli.IntFlag{
				Name:        "concurrency",
				Usage:       "single-connection concurrency",
				DefaultText: "8",
				Value:       8,
			},
			&cli.StringFlag{
				Name:        "o",
				Usage:       "output json file path",
				DefaultText: "",
				Value:       "",
				Aliases:     []string{"output"},
			},
		},
		Action: Run,
	}
}

func Run(ctx *cli.Context) error {
	Urls = ctx.StringSlice("urls")
	NumClient = ctx.Int("connection")
	NumMessage = ctx.Int64("message_num")
	PayloadSize = ctx.Int("payload_size")
	Compress = ctx.Bool("compress")
	Output = ctx.String("output")
	Concurrency = ctx.Int("concurrency")
	Payload = internal.AlphabetNumeric.Generate(PayloadSize)

	if dir := ctx.String("file"); dir != "" {
		b, err := os.ReadFile(dir)
		if err != nil {
			return err
		}
		Payload = b
		PayloadSize = len(b)
	}

	var handler = &Handler{
		pool: &sync.Pool{New: func() any {
			return bytes.NewBuffer(make([]byte, 0, PayloadSize+8))
		}},
		done:     make(chan struct{}),
		sessions: &sync.Map{},
	}

	var cc = concurrency.NewWorkerGroup[int]()
	for i := 0; i < NumClient; i++ {
		cc.Push(i)
	}
	cc.OnMessage = func(args int) error {
		socket, _, err := gws.NewClient(handler, &gws.ClientOption{
			ReadAsyncEnabled: true,
			ReadAsyncGoLimit: Concurrency,
			ReadBufferSize:   8 * 1024,
			CompressEnabled:  Compress,
			Addr:             SelectURL(),
			TlsConfig:        &tls.Config{InsecureSkipVerify: true},
		})
		if err != nil {
			return err
		}
		handler.sessions.Store(socket, 1)
		go socket.ReadLoop()
		return nil
	}
	if err := cc.Start(); err != nil {
		return err
	}

	var t0 = time.Now()
	handler.sessions.Range(func(key, value any) bool {
		go func() {
			for i := 0; i < Concurrency; i++ {
				handler.SendMessage(key.(*gws.Conn))
			}
		}()
		return true
	})

	go handler.ShowProgress()

	<-handler.done
	log.Info().Str("Percentage", "100.00%").Int("Requests", int(NumMessage)).Msg("")

	var iops = int(float64(NumMessage) / time.Since(t0).Seconds())
	var p50 = handler.Report(50)
	var p90 = handler.Report(90)
	var p99 = handler.Report(99)
	log.
		Info().
		Int("IOPS", iops).
		Str("Duration", time.Since(t0).String()).
		Str("P50", p50).
		Str("P90", p90).
		Str("P99", p99).
		Msg("")

	if Output != "" {
		file, err := os.OpenFile(Output, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
		if err != nil {
			return err
		}
		b, _ := json.Marshal(map[string]any{
			"iops":    iops,
			"payload": PayloadSize,
			"p50":     p50,
			"p90":     p90,
			"p99":     p99,
		})
		b = append(b, '\n')
		_, _ = file.Write(b)
	}
	return nil
}

type Handler struct {
	pool        *sync.Pool
	numReceived int64
	numSend     int64
	sessions    *sync.Map
	done        chan struct{}
}

func (c *Handler) OnOpen(socket *gws.Conn) { _ = socket.SetNoDelay(false) }

func (c *Handler) OnClose(socket *gws.Conn, err error) {
	if _, ok := err.(*gws.CloseError); !ok {
		log.Error().Msg(err.Error())
	}
	os.Exit(0)
}

func (c *Handler) OnPing(socket *gws.Conn, payload []byte) { _ = socket.WritePong(nil) }

func (c *Handler) OnPong(socket *gws.Conn, payload []byte) {}

func (c *Handler) OnMessage(socket *gws.Conn, message *gws.Message) {
	defer message.Close()

	message.Data.Next(PayloadSize)
	cost := (uint64(time.Now().UnixNano()) - binary.BigEndian.Uint64(message.Bytes())) / 1000000
	if cost >= M {
		cost = M - 1
	}
	atomic.AddUint64(&Stats[cost], 1)

	if x := atomic.AddInt64(&c.numReceived, 1); x == NumMessage {
		c.done <- struct{}{}
		return
	}
	c.SendMessage(socket)
}

func (c *Handler) SendMessage(socket *gws.Conn) {
	if atomic.AddInt64(&c.numSend, 1) <= NumMessage {
		buf := c.pool.Get().(*bytes.Buffer)
		if buf.Len() == 0 {
			buf.Write(Payload)
		}
		p := binary.BigEndian.AppendUint64(buf.Bytes(), uint64(time.Now().UnixNano()))
		_ = socket.WriteMessage(gws.OpcodeBinary, p)
		c.pool.Put(buf)
	}
}

func (c *Handler) Report(rate int) string {
	sum := uint64(0)
	threshold := uint64(int64(rate) * NumMessage / 100)
	for i, v := range Stats {
		if v == 0 {
			continue
		}
		sum += v
		if sum >= threshold {
			if i == M-1 {
				return "∞"
			}
			return fmt.Sprintf("%dms", i)
		}
	}
	return ""
}

func (c *Handler) ShowProgress() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		<-ticker.C
		requests := atomic.LoadInt64(&c.numReceived)
		percentage := fmt.Sprintf("%.2f", float64(100*requests)/float64(NumMessage)) + "%"
		log.Info().Str("Percentage", percentage).Int64("Requests", requests).Msg("")
	}
}

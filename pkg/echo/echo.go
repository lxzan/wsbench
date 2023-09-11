package echo

import (
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
	serial       = int64(0)
	urls         []string
	compress     bool
	payloadSize  int
	numClient    int
	numMessage   int32
	fileContents []byte
	N            int
	output       string
	stats        [M]uint64
)

func SelectURL() string {
	nextId := atomic.AddInt64(&serial, 1)
	return urls[nextId%int64(len(urls))]
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
				Usage:       "connections number",
				DefaultText: "100",
				Value:       100,
				Aliases:     []string{"connection"},
			},
			&cli.IntFlag{
				Name:        "n",
				Usage:       "messages number",
				DefaultText: "10000",
				Value:       10000,
				Aliases:     []string{"message_num"},
			},
			&cli.IntFlag{
				Name:        "p",
				Usage:       "payload size",
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
				Usage:       "Whether to turn on compression",
				DefaultText: "false",
				Value:       false,
			},
			&cli.StringFlag{
				Name:        "o",
				Usage:       "output",
				DefaultText: "",
				Value:       "",
				Aliases:     []string{"output"},
			},
		},
		Action: Run,
	}
}

func Run(ctx *cli.Context) error {
	urls = ctx.StringSlice("urls")
	numClient = ctx.Int("connection")
	numMessage = int32(ctx.Int("message_num"))
	payloadSize = ctx.Int("payload_size")
	compress = ctx.Bool("compress")
	output = ctx.String("output")
	N = int(numMessage)

	if dir := ctx.String("file"); dir != "" {
		b, err := os.ReadFile(dir)
		fileContents = b
		if err != nil {
			return err
		}
		fileContents = append(fileContents, "12345678"...)
	}

	var handler = &Handler{
		done:     make(chan struct{}),
		sessions: &sync.Map{},
	}

	var cc = concurrency.NewWorkerGroup[int]()
	for i := 0; i < numClient; i++ {
		cc.Push(i)
	}
	cc.OnMessage = func(args int) error {
		socket, _, err := gws.NewClient(handler, &gws.ClientOption{
			ReadBufferSize:  8 * 1024,
			CompressEnabled: compress,
			Addr:            SelectURL(),
			TlsConfig:       &tls.Config{InsecureSkipVerify: true},
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
			socket := key.(*gws.Conn)
			payload := internal.AlphabetNumeric.Generate(payloadSize)
			if len(fileContents) > 0 {
				payload = fileContents
				payloadSize = len(fileContents)
			}
			waitCh := make(chan struct{}, 1)
			socket.SessionStorage.Store("waitChKey", waitCh)
			for {
				if atomic.AddInt32(&numMessage, -1) < 0 {
					break
				}
				waitCh <- struct{}{}
				var b [8]byte
				binary.LittleEndian.PutUint64(b[0:], uint64(time.Now().UnixNano()))
				payload = append(payload[:payloadSize], b[0:]...)
				_ = socket.WriteMessage(gws.OpcodeBinary, payload)
			}
		}()
		return true
	})

	go handler.ShowProgress()

	<-handler.done
	log.Info().Str("Percentage", "100.00%").Int("Requests", N).Msg("")

	var iops = int(float64(N) / time.Since(t0).Seconds())
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

	if output != "" {
		file, err := os.OpenFile(output, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
		if err != nil {
			return err
		}
		b, _ := json.Marshal(map[string]any{
			"iops":    iops,
			"payload": payloadSize,
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
	num      int64
	sessions *sync.Map
	done     chan struct{}
}

func (c *Handler) OnOpen(socket *gws.Conn) { socket.SetNoDelay(false) }

func (c *Handler) OnClose(socket *gws.Conn, err error) {
	if _, ok := err.(*gws.CloseError); !ok {
		log.Error().Msg(err.Error())
	}
	os.Exit(0)
}

func (c *Handler) OnPing(socket *gws.Conn, payload []byte) {}

func (c *Handler) OnPong(socket *gws.Conn, payload []byte) {}

func (c *Handler) OnMessage(socket *gws.Conn, message *gws.Message) {
	defer message.Close()

	size := message.Data.Len()
	p := message.Bytes()[size-8:]
	cost := (uint64(time.Now().UnixNano()) - binary.LittleEndian.Uint64(p)) / 1000000
	if cost >= M {
		cost = M - 1
	}
	atomic.AddUint64(&stats[cost], 1)

	if atomic.AddInt64(&c.num, 1) == int64(N) {
		c.done <- struct{}{}
	}
	wait, _ := socket.SessionStorage.Load("waitChKey")
	waitCh := wait.(chan struct{})
	<-waitCh
}

func (c *Handler) Report(rate int) string {
	sum := uint64(0)
	threshold := uint64(rate * N / 100)
	for i, v := range stats {
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
		requests := atomic.LoadInt64(&c.num)
		percentage := fmt.Sprintf("%.2f", float64(100*requests)/float64(N)) + "%"
		log.Info().Str("Percentage", percentage).Int64("Requests", requests).Msg("")
	}
}

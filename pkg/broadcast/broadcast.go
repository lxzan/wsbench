package broadcast

import (
	"crypto/tls"
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
	serial      = int64(0)
	urls        []string
	compress    bool
	payloadSize int
	numClient   int
	numMessage  int
	N           int
	interval    int
	fileContent []byte
	stats       [M]uint64
)

func SelectURL() string {
	nextId := atomic.AddInt64(&serial, 1)
	return urls[nextId%int64(len(urls))]
}

func NewCommand() *cli.Command {
	return &cli.Command{
		Name: "broadcast",
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
			&cli.BoolFlag{
				Name:        "compress",
				Usage:       "Whether to turn on compression",
				DefaultText: "false",
				Value:       false,
			},
			&cli.IntFlag{
				Name:        "i",
				Usage:       "message delivery interval",
				DefaultText: "10s",
				Value:       10,
				Aliases:     []string{"interval"},
			},
			&cli.StringFlag{
				Name:        "f",
				Usage:       "load payload content from file",
				DefaultText: "",
				Aliases:     []string{"file"},
			},
		},
		Action: Run,
	}
}

func Run(ctx *cli.Context) error {
	urls = ctx.StringSlice("urls")
	numClient = ctx.Int("connection")
	numMessage = ctx.Int("message_num")
	payloadSize = ctx.Int("payload_size")
	compress = ctx.Bool("compress")
	interval = ctx.Int("interval")
	N = numClient * numMessage
	if s := ctx.String("file"); len(s) > 0 {
		content, err := os.ReadFile(s)
		if err != nil {
			return err
		}
		fileContent = content
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

	go func() {
		ticker := time.NewTicker(time.Duration(interval) * time.Second)
		defer ticker.Stop()

		payload := internal.AlphabetNumeric.Generate(payloadSize)
		if size := len(fileContent); size > 0 {
			payload = fileContent
			payloadSize = size
		}
		broadcaster := gws.NewBroadcaster(gws.OpcodeBinary, payload)

		for {
			<-ticker.C

			handler.sessions.Range(func(key, value any) bool {
				socket := key.(*gws.Conn)
				for i := 0; i < numMessage; i++ {
					_ = broadcaster.Broadcast(socket)
				}
				return true
			})
		}
	}()

	handler.ShowProgress()
	return nil
}

type Handler struct {
	num      int64
	sessions *sync.Map
	done     chan struct{}
}

func (c *Handler) OnOpen(socket *gws.Conn) { _ = socket.SetNoDelay(false) }

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
	atomic.AddInt64(&c.num, 1)
}

func (c *Handler) ShowProgress() {
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()
	for {
		<-ticker.C
		requests := atomic.LoadInt64(&c.num)
		log.Info().Int64("Requests", requests).Msg("")
	}
}

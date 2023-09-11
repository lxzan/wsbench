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

var (
	Serial      = int64(0)
	Urls        []string
	Compress    bool
	Payload     []byte
	PayloadSize int
	NumClient   int
	NumMessage  int
	Interval    int
)

func SelectURL() string {
	nextId := atomic.AddInt64(&Serial, 1)
	return Urls[nextId%int64(len(Urls))]
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
				Usage:       "number of multiple requests to make at a time",
				DefaultText: "100",
				Value:       100,
				Aliases:     []string{"connection"},
			},
			&cli.IntFlag{
				Name:        "n",
				Usage:       "number of requests to perform",
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
				Usage:       "whether to turn on compression",
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
	Urls = ctx.StringSlice("urls")
	NumClient = ctx.Int("connection")
	NumMessage = ctx.Int("message_num")
	PayloadSize = ctx.Int("payload_size")
	Payload = internal.AlphabetNumeric.Generate(PayloadSize)
	Compress = ctx.Bool("compress")
	Interval = ctx.Int("interval")
	if s := ctx.String("file"); len(s) > 0 {
		content, err := os.ReadFile(s)
		if err != nil {
			return err
		}
		Payload = content
		PayloadSize = len(content)
	}

	var handler = &Handler{
		done:     make(chan struct{}),
		sessions: &sync.Map{},
	}

	var cc = concurrency.NewWorkerGroup[int]()
	for i := 0; i < NumClient; i++ {
		cc.Push(i)
	}
	cc.OnMessage = func(args int) error {
		socket, _, err := gws.NewClient(handler, &gws.ClientOption{
			ReadBufferSize:  8 * 1024,
			CompressEnabled: Compress,
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
		ticker := time.NewTicker(time.Duration(Interval) * time.Second)
		defer ticker.Stop()

		broadcaster := gws.NewBroadcaster(gws.OpcodeBinary, Payload)
		for {
			<-ticker.C

			handler.sessions.Range(func(key, value any) bool {
				socket := key.(*gws.Conn)
				for i := 0; i < NumMessage; i++ {
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
	ticker := time.NewTicker(time.Duration(Interval) * time.Second)
	defer ticker.Stop()
	for {
		<-ticker.C
		requests := atomic.LoadInt64(&c.num)
		log.Info().Int64("Requests", requests).Msg("")
	}
}

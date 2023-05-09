package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"github.com/lxzan/concurrency"
	"github.com/lxzan/gws"
	"github.com/lxzan/wsbench/internal"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const M = 10000

var (
	url         string
	compress    bool
	payloadSize int
	numClient   int
	numMessage  int
	N           int
	stats       [M]uint64
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	flag.StringVar(&url, "u", "ws://127.0.0.1/", "url")
	flag.IntVar(&payloadSize, "p", 1000, "payload size")
	flag.IntVar(&numClient, "c", 100, "num of client")
	flag.IntVar(&numMessage, "n", 10000, "num of message per connection")
	flag.BoolVar(&compress, "compress", false, "compress")
	flag.Parse()

	N = numClient * numMessage
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
			CompressEnabled: compress,
			Addr:            url,
		})
		handler.sessions.Store(socket, 1)
		go socket.ReadLoop()
		return err
	}
	if err := cc.Start(); err != nil {
		log.Fatal().Msg(err.Error())
		return
	}

	var t0 = time.Now()
	handler.sessions.Range(func(key, value any) bool {
		go func() {
			socket := key.(*gws.Conn)
			payload := internal.AlphabetNumeric.Generate(payloadSize)
			for i := 0; i < numMessage; i++ {
				var b [8]byte
				binary.LittleEndian.PutUint64(b[0:], uint64(time.Now().UnixNano()))
				payload = append(payload[:payloadSize], b[0:]...)
				_ = socket.WriteMessage(gws.OpcodeText, payload)
			}
		}()
		return true
	})

	go handler.ShowProgress()

	<-handler.done
	log.Info().Str("Percentage", "100.00%").Int("Requests", N).Msg("")
	log.
		Info().
		Int("IOPS", int(float64(N)/time.Since(t0).Seconds())).
		Str("Duration", time.Since(t0).String()).
		Str("P50", handler.Report(50)).
		Str("P90", handler.Report(90)).
		Str("P99", handler.Report(99)).
		Msg("")
}

type Handler struct {
	num      int64
	sessions *sync.Map
	done     chan struct{}
}

func (c *Handler) OnOpen(socket *gws.Conn) {}

func (c *Handler) OnError(socket *gws.Conn, err error) {
	log.Error().Msg(err.Error())
	os.Exit(0)
}

func (c *Handler) OnClose(socket *gws.Conn, code uint16, reason []byte) {}

func (c *Handler) OnPing(socket *gws.Conn, payload []byte) {}

func (c *Handler) OnPong(socket *gws.Conn, payload []byte) {}

func (c *Handler) OnMessage(socket *gws.Conn, message *gws.Message) {
	defer message.Close()

	p := message.Bytes()[payloadSize:]
	cost := (uint64(time.Now().UnixNano()) - binary.LittleEndian.Uint64(p)) / 1000000
	if cost >= M {
		cost = M - 1
	}
	atomic.AddUint64(&stats[cost], 1)

	if atomic.AddInt64(&c.num, 1) == int64(N) {
		c.done <- struct{}{}
	}
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

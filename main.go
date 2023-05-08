package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"github.com/lxzan/gws"
	"github.com/lxzan/wsbench/internal"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var (
	url         string
	compress    bool
	payloadSize int
	numClient   int
	numMessage  int
	N           int
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
		stats:    make([]uint64, 0, N),
	}

	for i := 0; i < numClient; i++ {
		socket, _, err := gws.NewClient(handler, &gws.ClientOption{
			CompressEnabled: compress,
			Addr:            url,
		})
		if err != nil {
			log.Fatal().Msg(err.Error())
			return
		}
		handler.sessions.Store(socket, 1)
		go socket.ReadLoop()
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

	<-handler.done
	fmt.Printf("IOPS: %.0f\n", float64(N)/time.Since(t0).Seconds())
	fmt.Printf("Cost: %.2fms\n", float64(time.Since(t0).Microseconds())/1000)
	handler.Report()
}

type Handler struct {
	sync.Mutex
	num      int64
	stats    []uint64
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
	cost := uint64(time.Now().UnixNano()) - binary.LittleEndian.Uint64(p)
	c.Lock()
	c.stats = append(c.stats, cost)
	c.Unlock()

	num := atomic.AddInt64(&c.num, 1)
	if num == int64(N) {
		c.done <- struct{}{}
	}
}

func (c *Handler) Report() {
	sort.Slice(c.stats, func(i, j int) bool {
		return c.stats[i] < c.stats[j]
	})

	idx1 := int(float64(N) * 0.50)
	fmt.Printf("P50:  %.2fms\n", float64(c.stats[idx1])/1000000)

	idx2 := int(float64(N) * 0.90)
	fmt.Printf("P90:  %.2fms\n", float64(c.stats[idx2])/1000000)

	idx3 := int(float64(N) * 0.99)
	fmt.Printf("P99:  %.2fms\n", float64(c.stats[idx3])/1000000)
}

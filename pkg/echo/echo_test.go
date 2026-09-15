package echo

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lxzan/gws"
	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli/v2"
)

type echoServer struct {
	gws.BuiltinEventHandler
}

func (s *echoServer) OnMessage(socket *gws.Conn, message *gws.Message) {
	defer message.Close()
	_ = socket.WriteMessage(message.Opcode, message.Bytes())
}

func TestEchoBasic(t *testing.T) {
	upgrader := gws.NewUpgrader(&echoServer{}, nil)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := upgrader.Upgrade(w, r)
		if err != nil {
			return
		}
		go socket.ReadLoop()
	}))
	defer ts.Close()

	wsURL := "ws://" + strings.TrimPrefix(ts.URL, "http://")

	app := &cli.App{
		Commands: []*cli.Command{
			NewCommand(),
		},
	}

	err := app.Run([]string{"wsbench", "echo", "-u", wsURL, "-c", "4", "-n", "2000"})
	assert.NoError(t, err)
}

func TestEchoLatency(t *testing.T) {
	upgrader := gws.NewUpgrader(&echoServer{}, nil)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := upgrader.Upgrade(w, r)
		if err != nil {
			return
		}
		go socket.ReadLoop()
	}))
	defer ts.Close()

	wsURL := "ws://" + strings.TrimPrefix(ts.URL, "http://")

	outputFile := filepath.Join(t.TempDir(), "output.json")

	app := &cli.App{
		Commands: []*cli.Command{
			NewCommand(),
		},
	}

	err := app.Run([]string{"wsbench", "echo", "-u", wsURL, "-c", "2", "-n", "1000", "--latency=true", "-o", outputFile})
	assert.NoError(t, err)

	data, err := os.ReadFile(outputFile)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestEchoFromFile(t *testing.T) {
	upgrader := gws.NewUpgrader(&echoServer{}, nil)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := upgrader.Upgrade(w, r)
		if err != nil {
			return
		}
		go socket.ReadLoop()
	}))
	defer ts.Close()

	wsURL := "ws://" + strings.TrimPrefix(ts.URL, "http://")

	payloadFile := filepath.Join(t.TempDir(), "payload.txt")
	err := os.WriteFile(payloadFile, []byte("custom benchmark payload"), 0644)
	assert.NoError(t, err)

	app := &cli.App{
		Commands: []*cli.Command{
			NewCommand(),
		},
	}

	err = app.Run([]string{"wsbench", "echo", "-u", wsURL, "-c", "2", "-n", "500", "-f", payloadFile})
	assert.NoError(t, err)
}

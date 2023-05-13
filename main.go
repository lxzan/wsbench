package main

import (
	"github.com/lxzan/wsbench/pkg/broadcast"
	"github.com/lxzan/wsbench/pkg/iops"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	cli "github.com/urfave/cli/v2"
	"os"
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	app := cli.App{
		Name:  "wsbench",
		Usage: "testing websocket server iops and latency",
		Commands: []*cli.Command{
			iops.NewCommand(),
			broadcast.NewCommand(),
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal().Msg(err.Error())
	}
}

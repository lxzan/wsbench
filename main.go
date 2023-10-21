package main

import (
	"github.com/lxzan/wsbench/pkg/broadcast"
	"github.com/lxzan/wsbench/pkg/echo"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	cli "github.com/urfave/cli/v2"
	"os"
)

const Version = "v1.0.13"

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	app := cli.App{
		Name:  "wsbench",
		Usage: "testing websocket server iops and latency",
		Commands: []*cli.Command{
			echo.NewCommand(),
			broadcast.NewCommand(),
			{
				Name: "version",
				Action: func(context *cli.Context) error {
					println(Version)
					return nil
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal().Msg(err.Error())
	}
}

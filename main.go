package main

import (
	"os"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/common/promslog"
	promslogflag "github.com/prometheus/common/promslog/flag"
	"github.com/prometheus/common/version"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"
	"github.com/ymhhh/prome_exporters/agent"
	"github.com/ymhhh/prome_exporters/cmd/command"
	"github.com/ymhhh/prome_exporters/cmd/server"
	"github.com/ymhhh/prome_exporters/conf"

	_ "github.com/ymhhh/prome_exporters/plugins/inputs/all"
	_ "github.com/ymhhh/prome_exporters/plugins/outputs/all"
	_ "github.com/ymhhh/prome_exporters/plugins/serializers/all"
)

func main() {
	os.Exit(run())
}

var (
	promslogConfig = &promslog.Config{}
)

func run() int {
	var (
		cfgFile   = kingpin.Flag("config.file", "Exporters configuration file name.").Default("prome_exporters.yaml").String()
		webConfig = webflag.AddFlags(kingpin.CommandLine, ":10031")
	)

	promslogflag.AddFlags(kingpin.CommandLine, promslogConfig)
	kingpin.CommandLine.UsageWriter(os.Stdout)

	kingpin.Version(version.Print("prome_exporters"))
	kingpin.CommandLine.GetFlag("help").Short('h')
	kingpin.Parse()

	logger := promslog.New(promslogConfig)

	ec, err := conf.GetConfigWithFile(*cfgFile)
	if err != nil {
		logger.Error("failed_read_config", "cfgFile", *cfgFile, "err", err)
		return 1
	}

	a, err := agent.NewAgent(ec, logger)
	if err != nil {
		// Avoid logging the whole config object here; some value types may panic during slog formatting.
		logger.Error("failed_new_agent", "err", err)
		return 2
	}

	switch ec.Exporter.CommandType {
	case 0:
		return command.Run(a)
	case 1:
		return server.Run(a, webConfig)
	default:
		logger.Error("invalid_command_type", "command_type", ec.Exporter.CommandType)
		return 10
	}
}

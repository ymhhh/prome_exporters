package command

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/ymhhh/prome_exporters/agent"
)

func Run(a *agent.Agent) int {
	if err := a.Run(); err != nil {
		a.Logger.Error("failed_run_agent", "error", err)
		return 3
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGUSR1, syscall.SIGUSR2)
	<-ch
	a.Stop()
	return 0
}

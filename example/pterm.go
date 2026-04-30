package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/pterm/pterm"
)

func main() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		pterm.Println()
		pterm.Info.Println("Exiting...")
		os.Exit(0)
	}()

	for {
		result, err := pterm.DefaultInteractiveTextInput.Show()
		if err != nil {
			pterm.Println()
			pterm.Info.Println("Exiting...")
			return
		}
		pterm.Printfln("You answered: %s", result)
	}
}

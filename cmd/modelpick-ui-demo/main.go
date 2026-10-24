// Command modelpick-ui-demo serves the v2 page over an in-memory fixture, so
// the page can be designed and driven without a real config or vendor.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/meatlover/modelpick/internal/web"
	"github.com/meatlover/modelpick/internal/web/demo"
)

func main() {
	port := flag.Int("port", 0, "port to listen on (0 takes a free one)")
	noOpen := flag.Bool("no-open", false, "do not open the browser")
	flag.Parse()
	if err := run(*port, *noOpen); err != nil {
		fmt.Fprintln(os.Stderr, "modelpick-ui-demo:", err)
		os.Exit(1)
	}
}

func run(port int, noOpen bool) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	actual := ln.Addr().(*net.TCPAddr).Port
	token, err := web.NewToken()
	if err != nil {
		return err
	}
	d := demo.New(time.Now)
	srv := &http.Server{Handler: web.NewServer(d, actual, token), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "modelpick-ui-demo:", err)
			os.Exit(1)
		}
	}()
	url := fmt.Sprintf("http://127.0.0.1:%d/?t=%s", actual, token)
	fmt.Println("modelpick-ui-demo: " + url)
	fmt.Println("demo: type edit, break, fix or error + Enter")
	if !noOpen {
		openURL(url)
	}
	go readCommands(d)
	return waitForSignal(srv)
}

func waitForSignal(srv *http.Server) error {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx) // a slow client does not delay exit
	return nil
}

// readCommands applies one demo command per stdin line; EOF just stops reading.
func readCommands(d *demo.Demo) {
	commands := map[string]func(){
		"edit":  d.HandEdit,
		"break": func() { d.SetBroken("config.json: line 12: invalid character '}'") },
		"fix":   func() { d.SetBroken("") },
		"error": d.AddError,
	}
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		w := sc.Text()
		if w == "" {
			continue
		}
		if fn, ok := commands[w]; ok {
			fn()
			continue
		}
		fmt.Println("demo: unknown command " + w)
	}
}

func openURL(url string) {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	if err := exec.Command(name, url).Start(); err != nil {
		fmt.Println("demo: could not open the browser:", err)
	}
}

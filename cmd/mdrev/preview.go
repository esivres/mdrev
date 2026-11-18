package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"

	"github.com/esivres/mdrev/internal/preview"
)

// previewDocument serves the document and its review on localhost and opens a
// browser. One process per document; interrupt it to stop.
func previewDocument(args []string) error {
	fs := flag.NewFlagSet("preview", flag.ExitOnError)
	port := fs.Int("port", 0, "port to listen on; 0 picks a free one")
	noOpen := fs.Bool("no-open", false, "do not open the browser")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: mdrev preview <file.md> [--port N] [--no-open]")
	}
	document := rest[0]
	if err := requireDocument(document); err != nil {
		return err
	}

	server, err := preview.New(document)
	if err != nil {
		return err
	}
	// Loopback only: the review is private and the server has no auth.
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(*port)))
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s", ln.Addr())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Println(url)

	// Serve first. A browser that stays in the foreground (or a launcher that
	// blocks) must not keep the URL from answering, so its failure is reported
	// from a goroutine while the preview runs.
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Run(ctx, ln) }()

	if !*noOpen {
		go func() {
			if err := openBrowser(url); err != nil {
				// A missing or failing browser must not take the preview down.
				fmt.Fprintf(os.Stderr, "mdrev: %v (the preview stays up at %s)\n", err, url)
			}
		}()
	}
	return <-serveErr
}

// openBrowser opens the URL in a browser: $BROWSER, else the platform opener.
func openBrowser(url string) error {
	if browser := os.Getenv("BROWSER"); browser != "" {
		// $BROWSER may carry flags, and a %s placeholder names where the URL
		// goes; without one it is appended, as the spec says.
		argv := make([]string, 0, 4)
		placed := false
		for _, field := range strings.Fields(browser) {
			if strings.Contains(field, "%s") {
				field = strings.ReplaceAll(field, "%s", url)
				placed = true
			}
			argv = append(argv, field)
		}
		if !placed {
			argv = append(argv, url)
		}
		return exec.Command(argv[0], argv[1:]...).Run()
	}
	if runtime.GOOS == "windows" {
		// start is a cmd builtin; the empty title keeps the URL out of it.
		return exec.Command("cmd", "/c", "start", "", url).Run()
	}
	for _, opener := range []string{"xdg-open", "open"} {
		if _, err := exec.LookPath(opener); err != nil {
			continue
		}
		return exec.Command(opener, url).Run()
	}
	return fmt.Errorf("no browser to open: set $BROWSER")
}

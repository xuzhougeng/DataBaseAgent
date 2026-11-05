// Browser development host. The distributed desktop app uses Wails, not this server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"databaseagent/frontend"
	"databaseagent/internal/workbench"
)

func main() {
	root := flag.String("workspace", ".tmp/dev-workspace", "工作空间")
	port := flag.Int("port", 3415, "本地端口")
	flag.Parse()
	store, e := workbench.Open(*root)
	if e != nil {
		log.Fatal(e)
	}
	service := workbench.NewService(store)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	defer service.Stop()
	go service.Run(ctx)
	listener, e := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if e != nil {
		log.Fatal(e)
	}
	server := &http.Server{Handler: workbench.Handler(service, frontend.Assets), ReadHeaderTimeout: 10 * time.Second}
	go func() { <-ctx.Done(); _ = server.Close() }()
	log.Printf("DBA 本地开发预览: http://%s", listener.Addr())
	if e = server.Serve(listener); e != nil && e != http.ErrServerClosed {
		log.Fatal(e)
	}
}

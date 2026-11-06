package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"databaseagent/frontend"
	"databaseagent/internal/workbench"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func main() {
	config, e := os.UserConfigDir()
	if e != nil {
		log.Fatal(e)
	}
	root := flag.String("workspace", filepath.Join(config, "DataBaseAgent"), "本地工作空间目录")
	flag.Parse()
	store, e := workbench.Open(*root)
	if e != nil {
		log.Fatal(e)
	}
	service := workbench.NewService(store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer service.Stop()
	go service.Run(ctx)
	var window *application.WebviewWindow
	instanceID := fmt.Sprintf("org.databaseagent.%x", sha256.Sum256([]byte(strings.ToLower(store.Root()))))
	app := application.New(application.Options{Name: "DataBaseAgent", Description: "主题数据库构建智能体", Assets: application.AssetOptions{Handler: workbench.Handler(service, frontend.Assets)}, OnShutdown: service.Stop, SingleInstance: &application.SingleInstanceOptions{UniqueID: instanceID, OnSecondInstanceLaunch: func(application.SecondInstanceData) {
		if window != nil {
			window.Show()
			window.Focus()
		}
	}}})
	window = app.Window.NewWithOptions(application.WebviewWindowOptions{Title: "DBA · 数据库构建工作台", Width: 1440, Height: 960, MinWidth: 1000, MinHeight: 680, BackgroundColour: application.NewRGB(246, 248, 249), URL: "/"})
	if e = app.Run(); e != nil {
		log.Fatal(e)
	}
}

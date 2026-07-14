package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/keenetic-mtproto/keenetic-mtproto/internal/config"
	"github.com/keenetic-mtproto/keenetic-mtproto/internal/log"
	"github.com/keenetic-mtproto/keenetic-mtproto/internal/mtproto"
	"github.com/keenetic-mtproto/keenetic-mtproto/internal/web"
)

func main() {
	log.Init()
	cfgPath := flag.String("config", "/opt/etc/keenetic-mtproto/config.json", "path to config.json")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Errorf("load config: %v", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		_ = mtproto.RefreshDCs(cfg.System.MTProto.DCFallbackEnabled, cfg.System.MTProto.DCFallbackURL)
	}()
	mtproto.StartCFProxyRefresh(ctx, cfg.System.MTProto.CFProxyURL)

	proxy := mtproto.NewServer(cfg)
	if err := proxy.Start(); err != nil {
		log.Errorf("MTProto server: %v", err)
	}

	ui := web.New(cfg, proxy, func(c *config.Config) error {
		if err := c.SaveToFile(c.ConfigPath); err != nil {
			return err
		}
		return nil
	})
	if err := ui.Start(); err != nil {
		log.Errorf("web UI: %v", err)
		os.Exit(1)
	}

	log.Infof("keenetic-mtproto running (proxy enabled=%v port=%d)", cfg.System.MTProto.Enabled, cfg.System.MTProto.Port)
	<-ctx.Done()

	shCtx, shCancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer shCancel()
	_ = ui.Stop(shCtx)
	proxy.Stop()
	log.Infof("stopped")
}

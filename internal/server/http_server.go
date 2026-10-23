package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func (s *Server) serveHTTP(router http.Handler) error {
	addr := s.cfg.Server.Addr()
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}
	serverErrors := make(chan error, 2)
	var uiServer *http.Server
	if s.pluginUI != nil {
		uiServer = &http.Server{Addr: s.cfg.Plugin.UIListen, Handler: s.pluginUI, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
		defer uiServer.Close()
		go func() { serverErrors <- uiServer.ListenAndServe() }()
	}
	defer httpServer.Close()
	go func() {
		serverErrors <- httpServer.ListenAndServe()
	}()

	shutdownSignal, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-shutdownSignal.Done():
		log.Printf("🛑 CampusOS API 正在优雅停止")
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if uiServer != nil {
			_ = uiServer.Shutdown(shutdownContext)
		}
		if err := httpServer.Shutdown(shutdownContext); err != nil {
			_ = httpServer.Close()
			return err
		}
		if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

package wsserver

import (
	"context"
	"fmt"
	appcfg "github.com/ice-blockchain/wintr/config"
	"github.com/ice-blockchain/wintr/log"
	"github.com/ice-blockchain/wintr/wsserver/internal"
	"github.com/ice-blockchain/wintr/wsserver/internal/http3webtransport"
	"github.com/pkg/errors"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func New(service Service, cfgKey string) Server {
	var cfg internal.Config
	appcfg.MustLoadFromKey(cfgKey, &cfg)
	s := &srv{cfg: &cfg, service: service}
	s.server = http3webtransport.New(s.cfg, s.service.HandleWS)
	return s
}

func (s *srv) ListenAndServe(ctx context.Context, cancel context.CancelFunc) {
	go s.startServer()
	s.wait(ctx)
	s.shutDown() //nolint:contextcheck // Nope, we want to gracefully shutdown on a different context.
}

func (s *srv) startServer() {
	defer log.Info("server stopped listening")
	log.Info(fmt.Sprintf("server started listening on %v...", s.cfg.WSServer.Port))

	isUnexpectedError := func(err error) bool {
		return err != nil &&
			!errors.Is(err, io.EOF) &&
			!errors.Is(err, http.ErrServerClosed)
	}

	if err := s.server.ListenAndServeTLS(s.cfg.WSServer.CertPath, s.cfg.WSServer.KeyPath); isUnexpectedError(err) {
		s.quit <- syscall.SIGTERM
		log.Error(errors.Wrap(err, "server.ListenAndServeTLS failed"))
	}
}

func (s *srv) wait(ctx context.Context) {
	quit := make(chan os.Signal, 1)
	s.quit = quit
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done():
	case <-quit:
	}
}

func (s *srv) shutDown() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log.Info("shutting down server...")

	if err := s.server.Shutdown(ctx); err != nil && !errors.Is(err, io.EOF) {
		log.Error(errors.Wrap(err, "server shutdown failed"))
	} else {
		log.Info("server shutdown succeeded")
	}

	if err := s.service.Close(ctx); err != nil && !errors.Is(err, io.EOF) {
		log.Error(errors.Wrap(err, "state close failed"))
	} else {
		log.Info("state close succeeded")
	}
}
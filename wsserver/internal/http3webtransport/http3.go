// SPDX-License-Identifier: ice License 1.0

package http3webtransport

import (
	"context"
	"fmt"
	appcfg "github.com/ice-blockchain/wintr/config"
	"github.com/ice-blockchain/wintr/log"
	"github.com/ice-blockchain/wintr/wsserver/internal"
	"github.com/pkg/errors"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/webtransport-go"
	"net/http"
)

func New(cfg *internal.Config, wshandler internal.WsHandlerFunc, handler http.Handler) internal.Server {
	appcfg.MustLoadFromKey("development", &development)
	s := &srv{}
	wtserver := &webtransport.Server{
		H3: http3.Server{
			Addr:    fmt.Sprintf(":%v", cfg.WSServer.Port),
			Handler: s.handleWebTransport(wshandler, handler),
			QuicConfig: &quic.Config{
				Tracer: qlog.DefaultTracer,
			},
		},
	}
	if development {
		noCors := func(r *http.Request) bool {
			return true
		}
		wtserver.CheckOrigin = noCors
	}
	s.server = wtserver
	return s
}

func (s *srv) ListenAndServeTLS(certFile, keyFile string) error {
	return s.server.ListenAndServeTLS(certFile, keyFile)
}
func (s *srv) handleWebTransport(wsHandlerFunc internal.WsHandlerFunc, handler http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if r.Method == http.MethodConnect {
			conn, err := s.server.Upgrade(w, r)
			if err != nil {
				log.Error(errors.Wrapf(err, "upgrading failed"))
				w.WriteHeader(500)
				return
			}
			stream, err := conn.AcceptStream(ctx)
			if err != nil {
				log.Error(errors.Wrapf(err, "getting stream failed"))
				w.WriteHeader(500)
				return
			}
			defer stream.Close()
			wsHandlerFunc(ctx, stream)
		} else {
			if handler != nil {
				handler.ServeHTTP(w, r)
			}
		}
	}
}

func (s *srv) Shutdown(ctx context.Context) error {
	return s.server.Close()
}

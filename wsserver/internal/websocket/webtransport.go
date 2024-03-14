// SPDX-License-Identifier: ice License 1.0

package websocket

import (
	"context"
	"net/http"

	"github.com/pkg/errors"

	"github.com/ice-blockchain/wintr/log"
	"github.com/ice-blockchain/wintr/wsserver/internal"
	h2ec "github.com/ice-blockchain/wintr/wsserver/internal/websocket/h2extendedconnect"
)

func (s *srv) handleWebTransport(writer http.ResponseWriter, req *http.Request) (h2wt internal.WS, ctx context.Context, err error) {
	if upgrader, ok := writer.(h2ec.WebTransportUpgrader); ok {
		var session h2ec.Session
		session, err = upgrader.UpgradeWebTransport()
		if err != nil {
			err = errors.Wrapf(err, "upgrading http2/webtransport stream failed")
			log.Error(err)
			writer.WriteHeader(http.StatusBadRequest)

			return nil, nil, err
		}
		acceptCtx, acceptCancel := context.WithTimeout(req.Context(), acceptStreamTimeout)
		stream := session.AcceptStream(acceptCtx)
		acceptCancel()
		h2wt, ctx = internal.NewWebTransportAdapter(req.Context(), stream, s.cfg.WSServer.ReadTimeout, s.cfg.WSServer.WriteTimeout)

		return h2wt, ctx, nil
	}
	err = errors.Wrapf(err, "upgrading webtransport is not implemented for http2")
	log.Error(err)

	return nil, nil, err
}
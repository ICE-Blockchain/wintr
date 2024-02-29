// SPDX-License-Identifier: ice License 1.0

package http3webtransport

import (
	"github.com/ice-blockchain/wintr/wsserver/internal"
	"github.com/quic-go/webtransport-go"
	"net/http"
	stdlibtime "time"
)

var development bool

type (
	srv struct {
		server  *webtransport.Server
		handler http.HandlerFunc
		cfg     *internal.Config
	}
	wsAdapter struct {
		conn         *webtransport.Session
		stream       webtransport.Stream
		writeTimeout stdlibtime.Duration
		readTimeout  stdlibtime.Duration
		closeChannel chan struct{}
	}
)

const acceptStreamTimeout = 30 * stdlibtime.Second

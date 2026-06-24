package hysteria2

// HyPanel fork patch — base: github.com/sagernet/sing-quic@v0.6.1 hysteria2/service.go
//
// Changes vs upstream (search "HyPanel:"):
//   1. userMap reads/writes are guarded by userAccess (RWMutex). Upstream
//      reassigns s.userMap in UpdateUsers with no lock while ServeHTTP reads it
//      on per-connection goroutines — a data race. See go.mod replace + forks/README.md.
//   2. A session registry (sessions/sessionAccess) tracks authenticated sessions
//      so RetainUsers can force-disconnect users no longer in the set by their
//      auth password (instant ban / kick). Upstream keeps no session list, so a
//      banned user kept flowing until QUIC idle-timeout.
//
// Only this file and protocol/hysteria2/inbound.go (in the sing-box fork) differ
// from upstream. Keep this header on every re-sync after a version bump.

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/sagernet/quic-go"
	"github.com/sagernet/quic-go/congestion"
	"github.com/sagernet/quic-go/http3"
	"github.com/sagernet/quic-go/quicvarint"
	qtls "github.com/sagernet/sing-quic"
	congestion_meta1 "github.com/sagernet/sing-quic/congestion_meta1"
	congestion_meta2 "github.com/sagernet/sing-quic/congestion_meta2"
	"github.com/sagernet/sing-quic/hysteria"
	hyCC "github.com/sagernet/sing-quic/hysteria/congestion"
	"github.com/sagernet/sing-quic/hysteria2/internal/protocol"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/auth"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/common/ntp"
	aTLS "github.com/sagernet/sing/common/tls"
)

type ServiceOptions struct {
	Context               context.Context
	Logger                logger.Logger
	BrutalDebug           bool
	SendBPS               uint64
	ReceiveBPS            uint64
	IgnoreClientBandwidth bool
	SalamanderPassword    string
	TLSConfig             aTLS.ServerConfig
	UDPDisabled           bool
	UDPTimeout            time.Duration
	Handler               ServerHandler
	MasqueradeHandler     http.Handler
}

type ServerHandler interface {
	N.TCPConnectionHandlerEx
	N.UDPConnectionHandlerEx
}

type Service[U comparable] struct {
	ctx                   context.Context
	logger                logger.Logger
	brutalDebug           bool
	sendBPS               uint64
	receiveBPS            uint64
	ignoreClientBandwidth bool
	salamanderPassword    string
	tlsConfig             aTLS.ServerConfig
	quicConfig            *quic.Config
	userAccess            sync.RWMutex // HyPanel: guards userMap
	userMap               map[string]U
	udpDisabled           bool
	udpTimeout            time.Duration
	handler               ServerHandler
	masqueradeHandler     http.Handler
	quicListener          io.Closer
	sessionAccess         sync.Mutex                         // HyPanel: guards sessions
	sessions              map[*serverSession[U]]struct{}     // HyPanel: live authenticated sessions
}

func NewService[U comparable](options ServiceOptions) (*Service[U], error) {
	quicConfig := &quic.Config{
		DisablePathMTUDiscovery:        !(runtime.GOOS == "windows" || runtime.GOOS == "linux" || runtime.GOOS == "android" || runtime.GOOS == "darwin"),
		EnableDatagrams:                !options.UDPDisabled,
		MaxIncomingStreams:             1 << 60,
		InitialStreamReceiveWindow:     hysteria.DefaultStreamReceiveWindow,
		MaxStreamReceiveWindow:         hysteria.DefaultStreamReceiveWindow,
		InitialConnectionReceiveWindow: hysteria.DefaultConnReceiveWindow,
		MaxConnectionReceiveWindow:     hysteria.DefaultConnReceiveWindow,
		MaxIdleTimeout:                 hysteria.DefaultMaxIdleTimeout,
		KeepAlivePeriod:                hysteria.DefaultKeepAlivePeriod,
		DisablePathManager:             true,
	}
	if options.MasqueradeHandler == nil {
		options.MasqueradeHandler = http.NotFoundHandler()
	}
	if len(options.TLSConfig.NextProtos()) == 0 {
		options.TLSConfig.SetNextProtos([]string{http3.NextProtoH3})
	}
	return &Service[U]{
		ctx:                   options.Context,
		logger:                options.Logger,
		brutalDebug:           options.BrutalDebug,
		sendBPS:               options.SendBPS,
		receiveBPS:            options.ReceiveBPS,
		ignoreClientBandwidth: options.IgnoreClientBandwidth,
		salamanderPassword:    options.SalamanderPassword,
		tlsConfig:             options.TLSConfig,
		quicConfig:            quicConfig,
		userMap:               make(map[string]U),
		udpDisabled:           options.UDPDisabled,
		udpTimeout:            options.UDPTimeout,
		handler:               options.Handler,
		masqueradeHandler:     options.MasqueradeHandler,
		sessions:              make(map[*serverSession[U]]struct{}), // HyPanel
	}, nil
}

func (s *Service[U]) UpdateUsers(userList []U, passwordList []string) {
	userMap := make(map[string]U)
	for i, user := range userList {
		userMap[passwordList[i]] = user
	}
	// HyPanel: guard the swap; ServeHTTP reads userMap concurrently.
	s.userAccess.Lock()
	s.userMap = userMap
	s.userAccess.Unlock()
}

// HyPanel: RetainUsers force-disconnects every currently-authenticated session
// whose auth password is NOT in passwords. Call right after UpdateUsers so that
// users removed from the set (disabled / banned / deleted) are evicted
// immediately instead of lingering on their live connection until the QUIC
// idle-timeout. New handshakes are already blocked by the UpdateUsers swap;
// this reclaims the existing connection now.
func (s *Service[U]) RetainUsers(passwords []string) {
	keep := make(map[string]struct{}, len(passwords))
	for _, pw := range passwords {
		keep[pw] = struct{}{}
	}
	// Lock ordering: sessionAccess > authAccess > connAccess. We hold
	// sessionAccess while taking each session's authAccess to read its auth
	// fields, and close victims only AFTER releasing sessionAccess (closeWithError
	// takes connAccess). ServeHTTP takes only authAccess, never sessionAccess, so
	// there is no reverse-order acquisition.
	var victims []*serverSession[U]
	s.sessionAccess.Lock()
	for session := range s.sessions {
		session.authAccess.Lock()
		authed, pw := session.authenticated, session.authPassword
		session.authAccess.Unlock()
		if !authed {
			continue
		}
		if _, ok := keep[pw]; !ok {
			victims = append(victims, session)
		}
	}
	s.sessionAccess.Unlock()
	// Close outside the lock: closeWithError takes the per-session lock and
	// touches the QUIC connection.
	for _, session := range victims {
		session.closeWithError(E.New("user removed by panel"))
	}
}

func (s *Service[U]) addSession(session *serverSession[U]) {
	s.sessionAccess.Lock()
	s.sessions[session] = struct{}{}
	s.sessionAccess.Unlock()
}

func (s *Service[U]) removeSession(session *serverSession[U]) {
	s.sessionAccess.Lock()
	delete(s.sessions, session)
	s.sessionAccess.Unlock()
}

func (s *Service[U]) Start(conn net.PacketConn) error {
	if s.salamanderPassword != "" {
		conn = NewSalamanderConn(conn, []byte(s.salamanderPassword))
	}
	err := qtls.ConfigureHTTP3(s.tlsConfig)
	if err != nil {
		return err
	}
	listener, err := qtls.Listen(conn, s.tlsConfig, s.quicConfig)
	if err != nil {
		return err
	}
	s.quicListener = listener
	go s.loopConnections(listener)
	return nil
}

func (s *Service[U]) Close() error {
	return common.Close(
		s.quicListener,
	)
}

func (s *Service[U]) loopConnections(listener qtls.Listener) {
	for {
		connection, err := listener.Accept(s.ctx)
		if err != nil {
			if E.IsClosedOrCanceled(err) || errors.Is(err, quic.ErrServerClosed) {
				s.logger.Debug(E.Cause(err, "listener closed"))
			} else {
				s.logger.Error(E.Cause(err, "listener closed"))
			}
			return
		}
		go s.handleConnection(connection)
	}
}

func (s *Service[U]) handleConnection(connection *quic.Conn) {
	session := &serverSession[U]{
		Service:    s,
		ctx:        s.ctx,
		quicConn:   connection,
		connDone:   make(chan struct{}),
		udpConnMap: make(map[uint32]*udpPacketConn),
	}
	s.addSession(session)          // HyPanel
	defer s.removeSession(session) // HyPanel
	httpServer := http3.Server{
		Handler:          session,
		StreamDispatcher: session.dispatchStream,
	}
	_ = httpServer.ServeQUICConn(connection)
	_ = connection.CloseWithError(0, "")
}

type serverSession[U comparable] struct {
	*Service[U]
	ctx           context.Context
	quicConn      *quic.Conn
	connAccess    sync.Mutex
	connDone      chan struct{}
	connErr       error
	authAccess    sync.Mutex // HyPanel: guards authenticated/authUser/authPassword
	authenticated bool
	authUser      U
	authPassword  string // HyPanel: stable kick key
	udpAccess     sync.RWMutex
	udpConnMap    map[uint32]*udpPacketConn
}

func (s *serverSession[U]) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && r.Host == protocol.URLHost && r.URL.Path == protocol.URLPath {
		if s.authenticated {
			protocol.AuthResponseToHeader(w.Header(), protocol.AuthResponse{
				UDPEnabled: !s.udpDisabled,
				Rx:         s.receiveBPS,
				RxAuto:     s.receiveBPS == 0 && s.ignoreClientBandwidth,
			})
			w.WriteHeader(protocol.StatusAuthOK)
			return
		}
		request := protocol.AuthRequestFromHeader(r.Header)
		// HyPanel: guard the concurrent map read against UpdateUsers swaps.
		s.userAccess.RLock()
		user, loaded := s.userMap[request.Auth]
		s.userAccess.RUnlock()
		if !loaded {
			s.masqueradeHandler.ServeHTTP(w, r)
			return
		}
		// HyPanel: guard the auth-field writes — RetainUsers reads
		// authenticated/authPassword concurrently from the panel goroutine.
		s.authAccess.Lock()
		s.authUser = user
		s.authPassword = request.Auth
		s.authenticated = true
		s.authAccess.Unlock()
		var rxAuto bool
		if s.receiveBPS > 0 && s.ignoreClientBandwidth && request.Rx == 0 {
			s.logger.Debug("process connection from ", r.RemoteAddr, ": BBR disabled by server")
			s.masqueradeHandler.ServeHTTP(w, r)
			return
		} else if !(s.receiveBPS == 0 && s.ignoreClientBandwidth) && request.Rx > 0 {
			rx := request.Rx
			if s.sendBPS > 0 && rx > s.sendBPS {
				rx = s.sendBPS
			}
			s.quicConn.SetCongestionControl(hyCC.NewBrutalSender(rx, s.brutalDebug, s.logger))
		} else {
			timeFunc := ntp.TimeFuncFromContext(s.ctx)
			if timeFunc == nil {
				timeFunc = time.Now
			}
			s.quicConn.SetCongestionControl(congestion_meta2.NewBbrSender(
				congestion_meta2.DefaultClock{TimeFunc: timeFunc},
				congestion.ByteCount(s.quicConn.Config().InitialPacketSize),
				congestion.ByteCount(congestion_meta1.InitialCongestionWindow),
			))
			rxAuto = true
		}
		protocol.AuthResponseToHeader(w.Header(), protocol.AuthResponse{
			UDPEnabled: !s.udpDisabled,
			Rx:         s.receiveBPS,
			RxAuto:     rxAuto,
		})
		w.WriteHeader(protocol.StatusAuthOK)
		if s.ctx.Done() != nil {
			go func() {
				select {
				case <-s.ctx.Done():
					s.closeWithError(s.ctx.Err())
				case <-s.connDone:
				}
			}()
		}
		if !s.udpDisabled {
			go s.loopMessages()
		}
	} else {
		s.masqueradeHandler.ServeHTTP(w, r)
	}
}

func (s *serverSession[U]) dispatchStream(frameType http3.FrameType, stream *quic.Stream, err error) (bool, error) {
	if !s.authenticated || err != nil {
		return false, nil
	}
	if frameType != protocol.FrameTypeTCPRequest {
		return false, nil
	}
	_, err = quicvarint.Read(quicvarint.NewReader(stream))
	if err != nil {
		s.logger.Error(E.Cause(err, "seek frame type"))
		return true, nil
	}
	go func() {
		hErr := s.handleStream(stream)
		if hErr != nil {
			stream.CancelRead(0)
			stream.Close()
			s.logger.Error(E.Cause(hErr, "handle stream request"))
		}
	}()
	return true, nil
}

func (s *serverSession[U]) handleStream(stream *quic.Stream) error {
	destinationString, err := protocol.ReadTCPRequest(stream)
	if err != nil {
		return E.New("read TCP request")
	}
	s.handler.NewConnectionEx(auth.ContextWithUser(s.ctx, s.authUser), &serverConn{Stream: stream}, M.SocksaddrFromNet(s.quicConn.RemoteAddr()).Unwrap(), M.ParseSocksaddr(destinationString).Unwrap(), nil)
	return nil
}

func (s *serverSession[U]) closeWithError(err error) {
	s.connAccess.Lock()
	defer s.connAccess.Unlock()
	select {
	case <-s.connDone:
		return
	default:
		s.connErr = err
		close(s.connDone)
	}
	if E.IsClosedOrCanceled(err) {
		s.logger.Debug(E.Cause(err, "connection failed"))
	} else {
		s.logger.Error(E.Cause(err, "connection failed"))
	}
	_ = s.quicConn.CloseWithError(0, "")
}

type serverConn struct {
	*quic.Stream
	responseWritten bool
}

func (c *serverConn) HandshakeFailure(err error) error {
	if c.responseWritten {
		return os.ErrInvalid
	}
	c.responseWritten = true
	buffer := protocol.WriteTCPResponse(false, err.Error(), nil)
	defer buffer.Release()
	return common.Error(c.Stream.Write(buffer.Bytes()))
}

func (c *serverConn) HandshakeSuccess() error {
	if c.responseWritten {
		return nil
	}
	c.responseWritten = true
	buffer := protocol.WriteTCPResponse(true, "", nil)
	defer buffer.Release()
	return common.Error(c.Stream.Write(buffer.Bytes()))
}

func (c *serverConn) Read(p []byte) (n int, err error) {
	n, err = c.Stream.Read(p)
	return n, qtls.WrapError(err)
}

func (c *serverConn) Write(p []byte) (n int, err error) {
	if !c.responseWritten {
		c.responseWritten = true
		buffer := protocol.WriteTCPResponse(true, "", p)
		defer buffer.Release()
		_, err = c.Stream.Write(buffer.Bytes())
		if err != nil {
			return 0, qtls.WrapError(err)
		}
		return len(p), nil
	}
	n, err = c.Stream.Write(p)
	return n, qtls.WrapError(err)
}

func (c *serverConn) LocalAddr() net.Addr {
	return M.Socksaddr{}
}

func (c *serverConn) RemoteAddr() net.Addr {
	return M.Socksaddr{}
}

func (c *serverConn) Close() error {
	c.Stream.CancelRead(0)
	return c.Stream.Close()
}

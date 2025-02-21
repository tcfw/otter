package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/emersion/go-smtp"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/tcfw/otter/internal/utils"
	"github.com/tcfw/otter/pkg/id"
	"github.com/tcfw/otter/pkg/otter"
	"go.uber.org/zap"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

const (
	smtpGatewayDomain = "pois.pds.directory"
	tlsCachePrefix    = "acme"
	maxMessageBytes   = 25 * 1024 * 1024 //25 MB

	lookupTimeout   = 10 * time.Second
	dataReadTimeout = 1 * time.Minute

	smtpRequireTLS = false
)

type smtpLogger struct {
	log *zap.Logger
}

func (s *smtpLogger) Printf(format string, v ...interface{}) {
	s.log.Sugar().Infof(format, v...)
}

func (s *smtpLogger) Println(v ...interface{}) {
	s.log.Sugar().Infoln(v...)
}

func (e *EmailHandler) gwListen() {
	be := &Backend{e}

	s := smtp.NewServer(be)

	s.Addr = ":2525"
	s.Domain = smtpGatewayDomain
	s.WriteTimeout = 10 * time.Second
	s.ReadTimeout = 10 * time.Second
	s.MaxMessageBytes = maxMessageBytes
	s.MaxRecipients = 50
	s.AllowInsecureAuth = false
	s.EnableREQUIRETLS = true
	s.ErrorLog = &smtpLogger{log: e.l.Named("smtp-errors")}

	sc, err := e.o.Storage().System()
	if err != nil {
		panic("getting public store for autocert cache: " + err.Error())
	}

	autocert := &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		Cache:  utils.NewAutoCertDSCache(sc, tlsCachePrefix),
		// Client: &acme.Client{DirectoryURL: "https://acme-staging-v02.api.letsencrypt.org/directory"},
	}

	s.TLSConfig = &tls.Config{
		GetCertificate: autocert.GetCertificate,
		NextProtos:     []string{acme.ALPNProto},
	}

	e.l.Info("Starting server at", zap.Any("addr", s.Addr))

	if err := s.ListenAndServe(); err != nil {
		e.l.Error("starting smtp listener", zap.Error(err))
	}
}

// The Backend implements SMTP server methods.
type Backend struct {
	eh *EmailHandler
}

// NewSession is called after client greeting (EHLO, HELO).
func (bkd *Backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &Session{
		log:       bkd.eh.l.Named("smtp_session"),
		conn:      c,
		o:         bkd.eh.o,
		workQueue: bkd.eh.workQueue,
	}, nil
}

// A Session is returned after successful login.
type Session struct {
	log           *zap.Logger
	conn          *smtp.Conn
	o             otter.Otter
	resolvedPeers []peer.ID
	firstPeer     peer.ID
	workQueue     workQueue

	from string
	to   string
}

func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	if _, isTLS := s.conn.TLSConnectionState(); smtpRequireTLS && !isTLS {
		s.log.Info("SMTP sender tried to send mail without STARTTLS first")
		return &smtp.SMTPError{Code: 530, EnhancedCode: smtp.EnhancedCode{5, 7, 0}, Message: "Must issue a STARTTLS command first"}
	}

	s.from = from

	if opts.Size != 0 && opts.Size > maxMessageBytes {
		return &smtp.SMTPError{Code: 450, Message: "Body size too large"}
	}

	return nil
}

func (s *Session) Rcpt(to string, opts *smtp.RcptOptions) error {
	if _, isTLS := s.conn.TLSConnectionState(); smtpRequireTLS && !isTLS {
		s.log.Info("SMTP sender tried to send mail without STARTTLS first")
		return &smtp.SMTPError{Code: 530, EnhancedCode: smtp.EnhancedCode{5, 7, 0}, Message: "Must issue a STARTTLS command first"}
	}

	addr, err := mail.ParseAddress(to)
	if err != nil {
		return &smtp.SMTPError{Code: 501, Message: "Is that even an email address?"}
	}

	publicID, domain, err := SplitAddrUserDomain(addr.Address)
	if err != nil {
		return &smtp.SMTPError{Code: 501, Message: "Bad mailbox format"}
	}
	if domain != smtpGatewayDomain {
		return &smtp.SMTPError{Code: 553, Message: "Mailbox name not allowed"}
	}

	s.to = to

	//eager lookup - don't connect yet, just see if it's routable
	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
	defer cancel()

	peers, err := s.o.ResolveOtterNodesForKey(ctx, id.PublicID(publicID))
	if err != nil {
		s.log.Error("resolving otter node", zap.Error(err))
		return &smtp.SMTPError{Code: 451, Message: "Unable to lookup that address"}
	}

	if len(peers) == 0 {
		return &smtp.SMTPError{Code: 450, Message: "No routable nodes found to receive mail"}
	}

	s.resolvedPeers = peers

	s.firstPeer, err = utils.FirstOnlinePeer(ctx, peers, s.o.Protocols().P2P())
	if err != nil {
		return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 4, 1}, Message: "No nodes responded"}
	}

	return nil
}

func (s *Session) Data(envr io.Reader) error {
	if _, isTLS := s.conn.TLSConnectionState(); smtpRequireTLS && !isTLS {
		s.log.Info("SMTP sender tried to send mail without STARTTLS first")
		return &smtp.SMTPError{Code: 530, EnhancedCode: smtp.EnhancedCode{5, 7, 0}, Message: "Must issue a STARTTLS command first"}
	}

	// ctx, cancel := context.WithTimeout(context.Background(), dataReadTimeout)
	// defer cancel()

	// stream, err := s.o.Protocols().P2P().NewStream(ctx, s.firstPeer, protoID)
	// if err != nil {
	// 	s.log.Error("failed to open stream to remove node", zap.Error(err))
	// 	return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 4, 1}, Message: "Node stream failed to open"}
	// }

	// scope := stream.Scope()

	var done func()

	err := s.o.Protocols().P2P().Network().ResourceManager().ViewProtocol(protoID, func(ps network.ProtocolScope) error {
		if err := ps.ReserveMemory(maxMessageBytes, network.ReservationPriorityMedium); err != nil {
			return err
		}

		done = func() { ps.ReleaseMemory(maxMessageBytes) }

		return nil
	})
	if err != nil {
		s.log.Error("failed to reserve scope for handling email body", zap.Error(err))
		return err
	}

	b, err := io.ReadAll(io.LimitReader(envr, maxMessageBytes))
	if err != nil {
		return err
	}

	header, err := s.receivedHeader()
	if err != nil {
		s.log.Error("making received header", zap.Error(err))
		return err
	}

	envelope := fmt.Sprintf("%s\r\n%s", header, string(b))

	ip := s.conn.Conn().RemoteAddr().(*net.TCPAddr)

	s.workQueue <- &queueJob{
		Hello:    s.conn.Hostname(),
		RemoteIP: *ip,
		From:     s.from,
		To:       s.to,
		Envl:     []byte(envelope),
		Tries:    1,
		Release:  done,
	}

	return nil
}

func (s *Session) receivedHeader() (string, error) {
	heloHost := s.conn.Hostname()

	ip := s.conn.Conn().RemoteAddr().(*net.TCPAddr)

	host := ""
	if ip.IP.IsLoopback() {
		host = "localhost"
	}

	if host == "" {
		realHosts, err := net.LookupAddr(ip.IP.String())
		if err != nil {
			return "", fmt.Errorf("looking up ip: %w", err)
		}
		if len(realHosts) > 0 {
			host = realHosts[0]
		}
	}

	envlFor := s.to
	if !strings.HasSuffix(envlFor, ">") {
		envlFor = fmt.Sprintf("<%s>", envlFor)
	}

	localHostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("getting hostname: %w", err)
	}
	with := "ESMTP"

	now := time.Now().Format(time.RFC1123)

	parts := []string{
		"Received:",
		"from", heloHost, fmt.Sprintf(`(%s [%s])`, host, ip.IP.String()),
		"by", localHostname, "with", with,
		"for", envlFor,
	}
	header := strings.Join(parts, " ")
	header += fmt.Sprintf("; %s", now)

	return header, nil
}

func (s *Session) Reset() {}

func (s *Session) Logout() error {
	return nil
}

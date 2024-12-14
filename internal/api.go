package internal

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/tcfw/otter/internal/version"
	v1api "github.com/tcfw/otter/pkg/api"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"

	"github.com/gorilla/mux"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/tcfw/otter/pkg/config"
	"go.uber.org/zap"
)

func (o *Otter) setupAPI(ctx context.Context) error {
	r, err := o.initAPIRouter()
	if err != nil {
		return fmt.Errorf("initing API router: %w", err)
	}

	listenAddrs := o.GetConfigAs([]string{}, config.API_ListenAddrs).([]string)

	for _, addr := range listenAddrs {
		ma, err := multiaddr.NewMultiaddr(addr)
		if err != nil {
			return fmt.Errorf("parsing multiaddr: %w", err)
		}
		if !(manet.IsIP6LinkLocal(ma) || manet.IsIPLoopback(ma)) {
			return fmt.Errorf("refusing to listen to not local link addr: %s", ma)
		}

		lis, err := manet.Listen(ma)
		if err != nil {
			return fmt.Errorf("adding manet listener: %w", err)
		}

		go http.Serve(manet.NetListener(lis), http.HandlerFunc(r.ServeHTTP))

		o.logger.Debug("api listening", zap.Any("addr", ma.String()))

		go func() {
			<-ctx.Done()
			if err := lis.Close(); err != nil {
				o.logger.Error("closing api listener", zap.Error(err))
			}
		}()
	}

	return nil
}

func (o *Otter) initAPIRouter() (*mux.Router, error) {
	r := mux.NewRouter()

	r.Use(o.authMiddleware)

	apis := r.PathPrefix("/api").Subrouter()

	apis.HandleFunc("/version", o.apiHandle_Version)

	apis.HandleFunc("/oauth/token", o.apiHandle_OAuth_Token)

	apis.HandleFunc("/keys/new", o.apiHandle_Keys_NewKey).Methods(http.MethodPost)
	apis.HandleFunc("/keys", o.apiHandle_Keys_List).Methods(http.MethodGet)
	apis.HandleFunc("/keys", o.apiHandle_Keys_Delete).Methods(http.MethodDelete)
	apis.HandleFunc("/keys/sign", o.apiHandle_Keys_Sign).Methods(http.MethodPost)

	apis.HandleFunc("/p2p/peers", o.apiHandle_P2P_Peers).Methods(http.MethodGet)

	apis.HandleFunc("/otter/providers", o.apiHandle_Otter_Providers).Methods(http.MethodGet)

	o.apiRouter = apis
	return r, nil
}

func apiJSONError(w http.ResponseWriter, err error) {
	apiJSONErrorWithStatus(w, err, http.StatusInternalServerError)
}

func apiJSONErrorWithStatus(w http.ResponseWriter, err error, code int) {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v1api.ErrorResponse{Error: err.Error()})
}

func (o *Otter) apiJSONResponse(w http.ResponseWriter, body any) {
	w.Header().Add("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		o.logger.Error("sending api response", zap.Error(err))
	}
}

func (o *Otter) apiHandle_Version(w http.ResponseWriter, r *http.Request) {
	o.apiJSONResponse(w, v1api.Version{
		Version:    version.Version(),
		CommitHash: version.FullVersion(),
		BuildTime:  version.BuildTime(),
	})
}

func (o *Otter) setupPOISGW(ctx context.Context) error {
	if err := o.initPOISRouter(); err != nil {
		return fmt.Errorf("initing API router: %w", err)
	}

	listenAddrs := o.GetConfigAs([]string{}, config.POIS_ListenAddrs).([]string)
	useTLS := o.GetConfigAs(true, config.POIS_EnableTLS).(bool)

	for _, addr := range listenAddrs {
		ma, err := multiaddr.NewMultiaddr(addr)
		if err != nil {
			return fmt.Errorf("parsing multiaddr: %w", err)
		}

		lis, err := manet.Listen(ma)
		if err != nil {
			return fmt.Errorf("adding manet listener: %w", err)
		}

		netLis := manet.NetListener(lis)

		if useTLS {
			autocert := &autocert.Manager{
				Prompt: autocert.AcceptTOS,
			}

			netLis = tls.NewListener(netLis, &tls.Config{
				NextProtos: []string{"h2", "http/1.1", acme.ALPNProto},
				GetCertificate: func(chi *tls.ClientHelloInfo) (*tls.Certificate, error) {
					cert, err := autocert.GetCertificate(chi)
					if err != nil {
						o.logger.Named("autocert").Error("getting autocert", zap.Error(err))
					}
					return cert, err
				},
			})
		}

		go http.Serve(netLis, http.HandlerFunc(o.poisRouter.ServeHTTP))

		o.logger.Debug("POIS gw listening", zap.Any("addr", ma.String()))

		go func() {
			<-ctx.Done()
			if err := lis.Close(); err != nil {
				o.logger.Error("closing POIS gw listener", zap.Error(err))
			}
		}()
	}

	return nil
}

func (o *Otter) initPOISRouter() error {
	r := mux.NewRouter()

	r.Use(func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			o.logger.Info("pois request", zap.Any("url", r.URL.String()))
			h.ServeHTTP(w, r)
		})
	})

	r.HandleFunc("/version", o.apiHandle_Version)

	o.poisRouter = r
	return nil
}

func (o *Otter) RegisterPOISHandler(rr func(r *mux.Route)) {
	rr(o.poisRouter.NewRoute())
}

func (o *Otter) RegisterPOISHandlers(rr func(r *mux.Router)) {
	rr(o.poisRouter)
}

func (o *Otter) RegisterAPIHandler(rr func(r *mux.Route)) {
	rr(o.apiRouter.NewRoute())
}

func (o *Otter) RegisterAPIHandlers(rr func(r *mux.Router)) {
	rr(o.apiRouter)
}

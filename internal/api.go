package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/tcfw/otter/internal/version"
	v1api "github.com/tcfw/otter/pkg/api"

	"github.com/gorilla/mux"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/tcfw/otter/pkg/config"
	"go.uber.org/zap"
)

func (o *Otter) setupAPI(ctx context.Context) error {
	if err := o.initAPIRouter(); err != nil {
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

		go http.Serve(manet.NetListener(lis), http.HandlerFunc(o.apiRouter.ServeHTTP))

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

func (o *Otter) initAPIRouter() error {
	r := mux.NewRouter()

	r.HandleFunc("/version", o.apiHandle_Version)
	r.HandleFunc("/keys/new", o.apiHandle_Keys_NewKey).Methods(http.MethodPost)
	r.HandleFunc("/keys", o.apiHandle_Keys_List).Methods(http.MethodGet)
	r.HandleFunc("/keys", o.apiHandle_Keys_Delete).Methods(http.MethodDelete)

	o.apiRouter = r
	return nil
}

func apiJSONError(w http.ResponseWriter, err error) {
	apiJSONErrorWithStatus(w, err, http.StatusInternalServerError)
}

func apiJSONErrorWithStatus(w http.ResponseWriter, err error, code int) {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v1api.ErrorResponse{Error: err.Error()})
}

func (o *Otter) apiHandle_Version(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v1api.Version{
		Version:    version.Version(),
		CommitHash: version.FullVersion(),
		BuildTime:  version.BuildTime(),
	})
}

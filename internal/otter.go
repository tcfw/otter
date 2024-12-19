package internal

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gorilla/mux"
	ipfslite "github.com/hsanjuan/ipfs-lite"
	"github.com/ipfs/boxo/path"
	"github.com/ipfs/go-datastore"
	ipld "github.com/ipfs/go-ipld-format"
	dualdht "github.com/libp2p/go-libp2p-kad-dht/dual"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	"github.com/tcfw/otter/pkg/config"
	"github.com/tcfw/otter/pkg/id"
	"github.com/tcfw/otter/pkg/ipns"
	"github.com/tcfw/otter/pkg/otter"
	"github.com/tcfw/otter/pkg/plugins"
	"go.uber.org/zap"
)

type Otter struct {
	ctx    context.Context
	logger *zap.Logger

	p2p    host.Host
	dht    *dualdht.DHT
	pubsub *pubsub.PubSub
	ipld   ipld.DAGService
	mdns   mdns.Service
	rm     network.ResourceManager
	ping   *ping.PingService

	ui *uiConnector
	ds datastore.Batching

	sc *StorageClasses

	apiRouter  *mux.Router
	poisRouter *mux.Router
}

func (o *Otter) Crypto() otter.Cryptography     { return o }
func (o *Otter) Protocols() otter.Protocols     { return o }
func (o *Otter) Logger(comp string) *zap.Logger { return o.logger.Named(comp) }
func (o *Otter) UI() otter.UI                   { return o.ui }
func (o *Otter) Storage() otter.StorageClasses  { return o.sc }
func (o *Otter) IPLD() ipld.DAGService          { return o.ipld }

func NewOtter(ctx context.Context, logger *zap.Logger) (*Otter, error) {
	o := &Otter{
		ctx:    ctx,
		logger: logger,
	}
	o.sc = &StorageClasses{o}

	ds, err := NewDiskDatastoreStorage(o)
	if err != nil {
		return nil, fmt.Errorf("starting datastore: %w", err)
	}
	o.ds = ds

	if err := o.setupLibP2P(); err != nil {
		return nil, fmt.Errorf("initing libp2p: %w", err)
	}

	ping.NewPingService(o.p2p)

	if err := o.setupPubSub(ctx); err != nil {
		return nil, fmt.Errorf("initing pubsub: %w", err)
	}

	if err := o.setupAPI(ctx); err != nil {
		return nil, fmt.Errorf("initing api: %w", err)
	}

	if err := o.setupPOISGW(ctx); err != nil {
		return nil, fmt.Errorf("initing pois gw: %w", err)
	}

	if err := o.setupMdns(); err != nil {
		o.logger.Error("setting up mDNS discovery service: %w", zap.Error(err))
	}

	o.ipld, err = ipfslite.New(ctx, ds, nil, o.p2p, o.dht, nil)
	if err != nil {
		return nil, fmt.Errorf("initing ipfs-lite: %w", err)
	}

	go func() {
		sub, err := o.p2p.EventBus().Subscribe(&event.EvtLocalProtocolsUpdated{})
		if err != nil {
			o.logger.Error("getting eventbus sub for protocols", zap.Error(err))
			return
		}

		for evt := range sub.Out() {
			o.logger.Debug("Protocols updated", zap.Any("protocols", evt))
		}
	}()

	go func() {
		for {
			time.Sleep(60 * time.Second)
			keys, err := o.privateKeys(ctx)
			if err != nil {
				o.logger.Error("getting keys for ipns publish", zap.Error(err))
				return
			}

			for _, key := range keys {
				pubk, err := key.PublicKey()
				if err != nil {
					o.logger.Error("getting public key for ipns publish", zap.Error(err))
					return
				}

				dpk, err := id.DecodeCryptoMaterial(string(key))
				if err != nil {
					o.logger.Error("decoding private key for ipns publish", zap.Error(err))
					return
				}

				edpk := dpk.(ed25519.PrivateKey)

				cpk, cppk, err := crypto.KeyPairFromStdKey(&edpk)
				if err != nil {
					o.logger.Error("unmarshalling private key for ipns publish", zap.Error(err))
					return
				}

				p, err := path.NewPath("/ipfs/baguqeeravj3jdht6ccttqaaritwhorjhcupdyueu72x2q473atcegs4ep3tq")
				if err != nil {
					o.logger.Error("decoding path for ipns publish", zap.Error(err))
					return
				}

				sc, err := o.Storage().Public(pubk)
				if err != nil {
					o.logger.Error("getting public storage for key for ipns publish", zap.Error(err))
					return
				}

				rawPeerList, err := sc.Get(ctx, datastore.NewKey("storagePeers"))
				if err != nil && !errors.Is(err, datastore.ErrNotFound) {
					o.logger.Error("getting storage peer allow list", zap.Error(err))
					return
				}
				if rawPeerList == nil {
					rawPeerList = []byte(`[]`)
				}

				peerList := []peer.ID{}
				if err := json.Unmarshal(rawPeerList, &peerList); err != nil {
					o.logger.Error("decoding storage peer list", zap.Error(err))
					continue
				}

				record, err := ipns.NewRecord(cpk, p, 1, time.Now().Add(ipns.DefaultRecordLifetime), ipns.DefaultRecordTTL, ipns.WithOtterNodes(peerList))
				if err != nil {
					o.logger.Error("encoding ipns record", zap.Error(err))
					return
				}

				bytes, err := ipns.MarshalRecord(record)
				if err != nil {
					o.logger.Error("marshalling ipns record", zap.Error(err))
					return
				}

				pid, err := peer.IDFromPublicKey(cppk)
				if err != nil {
					o.logger.Error("getting publish ipns name", zap.Error(err))
					return
				}

				ns := ipns.NameFromPeer(pid)
				path := ns.AsPath()
				rk := ns.RoutingKey()

				go func() {
					ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
					defer cancel()

					o.logger.Debug("publishing IPNS record for key", zap.Any("RK", path.String()))

					if err := o.dht.PutValue(ctx, string(rk), bytes); err != nil {
						if errors.Is(err, context.DeadlineExceeded) {
							o.logger.Debug("publishing ipns name timed out")
						} else {
							o.logger.Error("publishing ipns name", zap.Error(err))
						}
						return
					}

					o.logger.Debug("published IPNS record for key", zap.Any("RK", path.String()))
				}()
			}
		}
	}()

	if err := startBuiltinPlugins(o); err != nil {
		o.logger.Error("loading builtin plugins", zap.Error(err))
	}

	loadExternalPlugins(o)

	return o, nil
}

func loadExternalPlugins(o *Otter) {
	pluginLoadDirs := o.GetConfigAs([]string{}, config.Plugins_LoadDir).([]string)
	if len(pluginLoadDirs) > 0 {
		for _, plugDir := range pluginLoadDirs {
			dir, err := os.ReadDir(plugDir)
			if err != nil {
				o.logger.Error("failed to list plugin dir: %s", zap.Error(err))
				continue
			}
			for _, dirent := range dir {
				name := dirent.Name()
				if !strings.HasSuffix(name, ".so") {
					continue
				}
				path := plugDir
				if !strings.HasSuffix(path, "/") {
					path += "/"
				}
				if err := plugins.LoadAndStart(path+name, o); err != nil {
					o.logger.Error("!! failed to load plugin: %s\n", zap.Error(err))
				}
			}
		}
	}
}

func (o *Otter) P2P() host.Host {
	return o.p2p
}

func (o *Otter) DHT() *dualdht.DHT {
	return o.dht
}

func (o *Otter) HostID() peer.ID {
	return o.p2p.ID()
}

func (o *Otter) Stop() error {
	if err := o.mdns.Close(); err != nil {
		o.logger.Error("stopping mdns discovery service", zap.Error(err))
	}

	if err := o.stopP2P(); err != nil {
		o.logger.Error("stopping p2p", zap.Error(err))
	}

	if err := o.ds.Close(); err != nil {
		o.logger.Error("closing main datastore", zap.Error(err))
	}

	if err := o.rm.Close(); err != nil {
		o.logger.Error("closing resource manager", zap.Error(err))
	}

	return nil
}

package internal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/tcfw/otter/internal/metrics"
	"github.com/tcfw/otter/pkg/id"
	"go.uber.org/zap"
)

const (
	metricsBroadcastRate = 30 * time.Second
	metricsTopicPrefix   = "/otter/metrics/"
)

var (
	collectors   = map[id.PublicID]*Collector{}
	collectorsMu sync.Mutex
)

type Collector struct {
	pk     id.PublicID
	o      *Otter
	logger *zap.Logger
	pubsub *pubsub.Topic

	last   metrics.PeerCollectorLastSet
	lastMu sync.Mutex
}

func (o *Otter) metricsPubSubFilter(pid peer.ID, topic string) bool {
	if pid == o.HostID() {
		return true
	}

	<-o.WaitForBootstrap(o.ctx)

	ctx, cancel := context.WithTimeout(o.ctx, 5*time.Second)
	defer cancel()

	logger := o.logger.Named("metrics.pubsub-filter")
	logger.Debug("checking peer", zap.Any("topic", topic), zap.Any("peer", pid.String()))

	if !strings.HasPrefix(topic, metricsTopicPrefix) {
		logger.Debug("skipping topic validation, unexpected prefix", zap.Any("topic", topic), zap.Any("peer", pid.String()))
		return false
	}

	keys, err := o.Keys(o.ctx)
	if err != nil {
		logger.Error("getting key list", zap.Error(err))
	}

	var account id.PublicID

	for _, k := range keys {
		if metricName(k) == topic {
			account = k
			break
		}
	}

	if account == "" {
		logger.Debug("no account for associated metrics pubsub")
		return false
	}

	logger.Debug("checking allowed peers...", zap.Any("topic", topic), zap.Any("peer", pid.String()))

	peers, err := o.getAllowedSyncerPeers(ctx, account)
	if err != nil {
		logger.Error("getting allows syncer peers", zap.Error(err))
		return false
	}

	logger.Debug("matching allowed peers...", zap.Any("topic", topic), zap.Any("peer", pid.String()))

	for _, peer := range peers {
		if peer == pid {
			logger.Debug("peer allowed", zap.Any("remote", pid.String()), zap.Any("allowed", peer.String()))
			return true
		}
	}

	logger.Debug("peer not allowed for metrics", zap.Any("remote", pid.String()))

	return false
}

func (c *Collector) watch() {
	sub, err := c.pubsub.Subscribe()
	if err != nil {
		c.logger.Error("subscribing to peer metrics", zap.Error(err))
	}

	for {
		msg, err := sub.Next(c.o.ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				c.logger.Error("getting metric", zap.Error(err))
			}
		}

		if err := c.processMsg(msg); err != nil {
			c.logger.Error("processing metric msg", zap.Error(err))
		}
	}
}

func (c *Collector) processMsg(msg *pubsub.Message) error {
	ctx, cancel := context.WithTimeout(c.o.ctx, 10*time.Second)
	defer cancel()

	data, err := c.o.KeyStore().PrivateUnseal(ctx, c.pk, msg.Data)
	if err != nil {
		return err
	}

	s := metrics.Set{}

	if err := s.Unmarshal(data); err != nil {
		return err
	}

	c.lastMu.Lock()
	c.last[msg.GetFrom()] = metrics.CollectorLastSet{Ts: time.Now(), Set: s}
	c.lastMu.Unlock()

	c.logger.Debug("updated last metrics", zap.String("node", msg.GetFrom().String()), zap.Any("metrics", s))

	return nil
}

func (c *Collector) publishSet(ms metrics.Set) error {
	ctx, cancel := context.WithTimeout(c.o.ctx, 30*time.Second)
	defer cancel()

	b, err := ms.Marshal()
	if err != nil {
		return err
	}

	msg, err := c.o.KeyStore().PrivateSeal(ctx, c.pk, b)
	if err != nil {
		return err
	}

	c.last[c.o.HostID()] = metrics.CollectorLastSet{Set: ms, Ts: time.Now()}

	return c.pubsub.Publish(ctx, msg)
}

func (o *Otter) publishMetrics(ctx context.Context) {
	t := time.NewTicker(1)
	for {
		select {
		case <-t.C:
			err := o.doPublishMetrics()
			if err != nil {
				o.logger.Named("metrics.publisher").Error("publishing", zap.Error(err))
			}

			t.Reset(metricsBroadcastRate)
		case <-ctx.Done():
			return
		}
	}
}

func (o *Otter) doPublishMetrics() error {
	set, err := metrics.Collect()
	if err != nil {
		return err
	}

	keys, err := o.Keys(o.ctx)
	if err != nil {
		return err
	}

	errs := []error{}

	for _, k := range keys {
		if err := o.publishMetricsForKey(k, set); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (o *Otter) publishMetricsForKey(k id.PublicID, m metrics.Set) error {
	o.logger.Named("metrics.publisher").Debug("publishing metrics", zap.String("p", string(k)))

	c, err := o.getCollectorOrNew(k)
	if err != nil {
		return err
	}

	return c.publishSet(m)
}

func (o *Otter) getCollectorOrNew(p id.PublicID) (*Collector, error) {
	collectorsMu.Lock()
	defer collectorsMu.Unlock()

	c, ok := collectors[p]
	if ok {
		return c, nil
	}

	topic, err := o.pubsub.Join(metricName(p))
	if err != nil {
		return nil, err
	}

	n := &Collector{
		logger: o.logger.Named("metrics." + string(p) + ".collector"),
		o:      o,
		pubsub: topic,
		pk:     p,
		last:   make(metrics.PeerCollectorLastSet),
	}
	collectors[p] = n

	go n.watch()

	return n, nil
}

func metricName(p id.PublicID) string {
	return fmt.Sprintf("%s%s", metricsTopicPrefix, string(p))
}

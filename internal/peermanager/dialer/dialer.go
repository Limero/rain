package dialer

import (
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peerlist"
	"github.com/cenkalti/rain/internal/peermanager/dialer/handler"
	"github.com/cenkalti/rain/internal/peermanager/peerids"
	"github.com/cenkalti/rain/internal/worker"
)

const maxDial = 40

type Dialer struct {
	peerList *peerlist.PeerList
	peerIDs  *peerids.PeerIDs
	peerID   [20]byte
	infoHash [20]byte
	newPeers chan *peer.Peer
	workers  worker.Workers
	limiter  chan struct{}
	log      logger.Logger
}

func New(peerList *peerlist.PeerList, peerIDs *peerids.PeerIDs, peerID, infoHash [20]byte, newPeers chan *peer.Peer, l logger.Logger) *Dialer {
	return &Dialer{
		peerList: peerList,
		peerIDs:  peerIDs,
		peerID:   peerID,
		infoHash: infoHash,
		newPeers: newPeers,
		limiter:  make(chan struct{}, maxDial),
		log:      l,
	}
}

func (d *Dialer) Run(stopC chan struct{}) {
	for {
		select {
		case d.limiter <- struct{}{}:
			select {
			case addr := <-d.peerList.Get():
				h := handler.New(addr, d.peerIDs, d.peerID, d.infoHash, d.newPeers, d.log)
				d.workers.StartWithOnFinishHandler(h, func() { <-d.limiter })
			case <-stopC:
				d.workers.Stop()
				return
			}
		case <-stopC:
			d.workers.Stop()
			return
		}
	}
}

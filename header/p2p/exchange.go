package p2p

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"

	logging "github.com/ipfs/go-log/v2"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"

	"github.com/celestiaorg/go-libp2p-messenger/serde"

	"github.com/celestiaorg/celestia-node/header"

	p2p_pb "github.com/celestiaorg/celestia-node/header/p2p/pb"
	header_pb "github.com/celestiaorg/celestia-node/header/pb"
)

var log = logging.Logger("header/p2p")

// PubSubTopic hardcodes the name of the ExtendedHeader
// gossipsub topic.
const PubSubTopic = "header-sub"

var exchangeProtocolID = protocol.ID("/header-ex/v0.0.1")

// Exchange enables sending outbound ExtendedHeaderRequests to the network as well as
// handling inbound ExtendedHeaderRequests from the network.
type Exchange struct {
	host host.Host

	trustedPeers peer.IDSlice
}

func NewExchange(host host.Host, peers peer.IDSlice) *Exchange {
	return &Exchange{
		host:         host,
		trustedPeers: peers,
	}
}

func (ex *Exchange) RequestHead(ctx context.Context) (*header.ExtendedHeader, error) {
	log.Debug("requesting head")
	// create request
	req := &p2p_pb.ExtendedHeaderRequest{
		Origin: uint64(0),
		Amount: 1,
	}
	headers, err := ex.performRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	return headers[0], nil
}

func (ex *Exchange) RequestHeader(ctx context.Context, height uint64) (*header.ExtendedHeader, error) {
	log.Debugw("requesting header", "height", height)
	// sanity check height
	if height == 0 {
		return nil, fmt.Errorf("specified request height must be greater than 0")
	}
	// create request
	req := &p2p_pb.ExtendedHeaderRequest{
		Origin: height,
		Amount: 1,
	}
	headers, err := ex.performRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	return headers[0], nil
}

func (ex *Exchange) RequestHeaders(ctx context.Context, from, amount uint64) ([]*header.ExtendedHeader, error) {
	log.Debugw("requesting headers", "from", from, "to", from+amount)
	// create request
	req := &p2p_pb.ExtendedHeaderRequest{
		Origin: from,
		Amount: amount,
	}
	return ex.performRequest(ctx, req)
}

func (ex *Exchange) RequestByHash(ctx context.Context, hash tmbytes.HexBytes) (*header.ExtendedHeader, error) {
	log.Debugw("requesting header", "hash", hash.String())
	// create request
	req := &p2p_pb.ExtendedHeaderRequest{
		Hash:   hash.Bytes(),
		Amount: 1,
	}
	headers, err := ex.performRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	if !bytes.Equal(headers[0].Hash().Bytes(), hash) {
		return nil, fmt.Errorf("incorrect hash in header: expected %x, got %x", hash, headers[0].Hash().Bytes())
	}
	return headers[0], nil
}

func (ex *Exchange) performRequest(
	ctx context.Context,
	req *p2p_pb.ExtendedHeaderRequest,
) ([]*header.ExtendedHeader, error) {
	if req.Amount == 0 {
		return make([]*header.ExtendedHeader, 0), nil
	}

	if len(ex.trustedPeers) == 0 {
		return nil, fmt.Errorf("no trusted peers")
	}

	// nolint:gosec // G404: Use of weak random number generator
	index := rand.Intn(len(ex.trustedPeers))
	stream, err := ex.host.NewStream(ctx, ex.trustedPeers[index], exchangeProtocolID)
	if err != nil {
		return nil, err
	}
	// send request
	_, err = serde.Write(stream, req)
	if err != nil {
		stream.Reset() //nolint:errcheck
		return nil, err
	}
	// read responses
	headers := make([]*header.ExtendedHeader, req.Amount)
	for i := 0; i < int(req.Amount); i++ {
		resp := new(header_pb.ExtendedHeader)
		_, err := serde.Read(stream, resp)
		if err != nil {
			stream.Reset() //nolint:errcheck
			return nil, err
		}

		header, err := header.ProtoToExtendedHeader(resp)
		if err != nil {
			stream.Reset() //nolint:errcheck
			return nil, err
		}

		headers[i] = header
	}
	// ensure at least one header was retrieved
	if len(headers) == 0 {
		return nil, header.ErrNotFound
	}
	return headers, stream.Close()
}

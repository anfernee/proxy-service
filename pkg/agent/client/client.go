package client

import (
	"context"
	"errors"
	"io"
	"math/rand"
	"net"
	"time"

	"github.com/anfernee/proxy-service/proto/agent"
	"github.com/golang/glog"

	"google.golang.org/grpc"
)

// Tunnel provides ability to dial a connection through itself
type Tunnel interface {
	// Dial dials a connection
	Dial(protocol, address string) (net.Conn, error)
}

type dialResult struct {
	err    string
	connid int64
}

type grpcTunnel struct {
	grpcConn    *grpc.ClientConn
	client      agent.ProxyServiceClient
	stream      agent.ProxyService_ProxyClient
	pendingDial map[int64]chan<- dialResult
	conns map[int64]*conn
}

// CreateGrpcTunnel creates a grpc based tunnel
func CreateGrpcTunnel(address string) (Tunnel, error) {
	// TODO: mTLS
	c, err := grpc.Dial(address, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	client := agent.NewProxyServiceClient(c)

	stream, err := client.Proxy(context.Background())
	if err != nil {
		return nil, err
	}

	tunnel := &grpcTunnel{
		grpcConn:    c,
		client:      client,
		stream:      stream,
		pendingDial: make(map[int64]chan<- dialResult),
		conns: make(map[int64]*conn),
	}

	go tunnel.serve()

	return tunnel, nil
}

func (t *grpcTunnel) serve() {
	for {
		pkt, err := t.stream.Recv()
		if err == io.EOF {
			return
		}
		if err != nil {
			glog.Warningf("stream read error: %v", err)
			return
		}

		glog.Infof("[tracing] recv packet %+v", pkt)

		switch pkt.Type {
		case agent.PacketType_DIAL_RSP:
			resp := pkt.GetDialResponse()
			if ch, ok := t.pendingDial[resp.Random]; !ok {
				glog.Warning("DialResp not recognized; dropped")
			} else {
				ch <- dialResult{
					err:    resp.Error,
					connid: resp.ConnectID,
				}
			}
		case agent.PacketType_DATA:
			resp := pkt.GetData()
			// TODO: flow control
			if conn, ok := t.conns[resp.ConnectID]; ok {
				conn.readCh <- resp.Data
			} else {
				glog.Warningf("connection id %d not recognized", resp.ConnectID)
			}
		}
	}
}

func (t *grpcTunnel) Dial(protocol, address string) (net.Conn, error) {
	random := rand.Int63()
	resCh := make(chan dialResult)
	t.pendingDial[random] = resCh
	defer func() {
		delete(t.pendingDial, random)
	}()

	req := &agent.Packet{
		Type: agent.PacketType_DIAL_REQ,
		Payload: &agent.Packet_DialRequest{
			DialRequest: &agent.DialRequest{
				Protocol: protocol,
				Address:  address,
				Random:   random,
			},
		},
	}
	glog.Infof("[tracing] send packet %+v", req)

	err := t.stream.Send(req)
	if err != nil {
		return nil, err
	}

	c := &conn{stream: t.stream}

	select {
	case res := <-resCh:
		if res.err != "" {
			return nil, errors.New(res.err)
		}
		c.connID = res.connid
		c.readCh = make(chan []byte, 10)
		t.conns[res.connid] = c
		// TODO: remove connection from the map
	case <-time.After(30 * time.Second):
		return nil, errors.New("dial timeout")
	}

	return c, nil
}

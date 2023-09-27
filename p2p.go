package go_ipfs_p2p

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	ds "github.com/ipfs/go-datastore"
	dsync "github.com/ipfs/go-datastore/sync"
	ipfsp2p "github.com/ipfs/go-ipfs/p2p"
	"github.com/libp2p/go-libp2p"
	connmgr "github.com/libp2p/go-libp2p-connmgr"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	pstore "github.com/libp2p/go-libp2p-core/peerstore"
	"github.com/libp2p/go-libp2p-core/pnet"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/libp2p/go-libp2p-core/routing"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	rhost "github.com/libp2p/go-libp2p/p2p/host/routed"
	ma "github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
	"github.com/samber/lo"
	"math/rand"
	"regexp"
	"strconv"
	"time"
)

var resolveTimeout = 10 * time.Second

// NewRoutedHost create a p2p routing client
func newRoutedHost(listenPort int, privstr string, swarmkey []byte, peers []string) (host.Host, *rhost.RoutedHost, *dht.IpfsDHT, error) {
	ctx := context.Background()

	skbytes, err := base64.StdEncoding.DecodeString(privstr)
	if err != nil {
		fmt.Println(err)
		return nil, nil, nil, err
	}
	priv, err := crypto.UnmarshalPrivateKey(skbytes)
	if err != nil {
		fmt.Println(err)
		return nil, nil, nil, err
	}
	bootstrapPeers := convertPeers(peers)

	// load private key swarm.key

	psk, err := pnet.DecodeV1PSK(bytes.NewReader(swarmkey))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to configure private network: %s", err)
	}

	// Generate a key pair for this host. We will use it at least
	// to obtain a valid host ID.
	opts := []libp2p.Option{
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", listenPort)),
		libp2p.DefaultTransports,
		libp2p.DefaultMuxers,
		libp2p.DefaultSecurity,
		libp2p.NATPortMap(),
		libp2p.PrivateNetwork(psk),
		libp2p.ConnectionManager(connmgr.NewConnManager(
			100,         // Lowwater
			400,         // HighWater,
			time.Minute, // GracePeriod
		)),
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			idht, err := dht.New(ctx, h)
			return idht, err
		}),
		libp2p.EnableAutoRelay(),
		// If you want to help other peers to figure out if they are behind
		// NATs, you can launch the server-side of AutoNAT too (AutoRelay
		// already runs the client)
		//
		// This service is highly rate-limited and should not cause any
		// performance issues.
		libp2p.EnableNATService(),
	}

	basicHost, err := libp2p.New(ctx, opts...)
	if err != nil {
		return nil, nil, nil, err
	}

	// Construct a datastore (needed by the DHT). This is just a simple, in-memory thread-safe datastore.
	dstore := dsync.MutexWrap(ds.NewMapDatastore())

	// Make the DHT
	DHT := dht.NewDHT(ctx, basicHost, dstore)

	// Make the routed host
	routedHost := rhost.Wrap(basicHost, DHT)

	cfg := DefaultBootstrapConfig
	cfg.BootstrapPeers = func() []peer.AddrInfo {
		return bootstrapPeers
	}

	id, err := peer.IDFromPrivateKey(priv)
	_, err = Bootstrap(id, routedHost, DHT, cfg)

	// connect to the chosen ipfs nodes
	if err != nil {
		return nil, nil, nil, err
	}

	// Bootstrap the host
	err = DHT.Bootstrap(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	// Build host multiaddress
	hostAddr, _ := ma.NewMultiaddr(fmt.Sprintf("/ipfs/%s", routedHost.ID().Pretty()))

	// Now we can build a full multiaddress to reach this host
	// by encapsulating both addresses:
	// addr := routedHost.Addrs()[0]
	addrs := routedHost.Addrs()
	fmt.Println("I can be reached at:")
	for _, addr := range addrs {
		fmt.Println(addr.Encapsulate(hostAddr))
	}

	return basicHost, routedHost, DHT, nil
}

// MakeIpfsP2p create ipfs p2p object
func newIpfsP2p(h host.Host) *ipfsp2p.P2P {
	return ipfsp2p.New(h.ID(), h, h.Peerstore())
}

// P2pClient p2p client
type P2pClient struct {
	Host       host.Host
	P2P        *ipfsp2p.P2P
	DHT        *dht.IpfsDHT
	RoutedHost *rhost.RoutedHost
	Peers      []string
}

func NewP2pClient(listenPort int, privstr string, swarmkey string, peers []string) (*P2pClient, error) {
	host, routedHost, DHT, err := newRoutedHost(listenPort, privstr, []byte(swarmkey), peers)
	if err != nil {
		return nil, err
	}
	P2P := newIpfsP2p(host)
	return &P2pClient{
		Host:       host,
		P2P:        P2P,
		DHT:        DHT,
		RoutedHost: routedHost,
		Peers:      peers,
	}, nil
}

// P2PListenerInfoOutput  p2p monitoring or mapping information
type P2PListenerInfoOutput struct {
	Protocol      string
	ListenAddress string
	TargetAddress string
}

// P2PLsOutput p2p monitor or map information output
type P2PLsOutput struct {
	Listeners []P2PListenerInfoOutput
}

// List p2p monitor message list
func (c *P2pClient) List() *P2PLsOutput {
	output := &P2PLsOutput{}

	c.P2P.ListenersLocal.Lock()
	for _, listener := range c.P2P.ListenersLocal.Listeners {
		output.Listeners = append(output.Listeners, P2PListenerInfoOutput{
			Protocol:      string(listener.Protocol()),
			ListenAddress: listener.ListenAddress().String(),
			TargetAddress: listener.TargetAddress().String(),
		})
	}
	c.P2P.ListenersLocal.Unlock()

	c.P2P.ListenersP2P.Lock()
	for _, listener := range c.P2P.ListenersP2P.Listeners {
		output.Listeners = append(output.Listeners, P2PListenerInfoOutput{
			Protocol:      string(listener.Protocol()),
			ListenAddress: listener.ListenAddress().String(),
			TargetAddress: listener.TargetAddress().String(),
		})
	}
	c.P2P.ListenersP2P.Unlock()

	return output
}

// Listen map local ports to p2p networks
func (c *P2pClient) Listen(proto, targetOpt string) error {
	fmt.Println("listening for connections")

	//targetOpt := fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", port)
	protoId := protocol.ID(proto)

	target, err := ma.NewMultiaddr(targetOpt)
	if err != nil {
		fmt.Println(err)
	}
	_, err = c.P2P.ForwardRemote(context.Background(), protoId, target, false)
	fmt.Println("local port" + targetOpt + ",mapping to p2p network succeeded")
	return err
}

// Forward connect p2p network to remote nodes / map to local port
func (c *P2pClient) Forward(protoOpt string, port int, peerId string) error {

	if peerId == "" {
		return fmt.Errorf("peer id cannot be empty")
	}

	if err := c.CheckForwardHealth(protoOpt, peerId); err != nil {
		// recover
		defer func() {
			if r := recover(); r != nil {
				fmt.Println("Recovered in f", r)
			}
		}()
		fmt.Println("CheckForwardHealth:", peerId)
		fmt.Println("c.Peers:", c.Peers)
		bootstrapPeers := randomSubsetOfPeers(convertPeers(c.Peers), 1)
		if len(bootstrapPeers) == 0 {
			return errors.New("not enough bootstrap peers")
		}
		circuitPeerId := bootstrapPeers[0].ID.Pretty()
		err = c.ConnectCircuit(circuitPeerId, peerId)
		if err != nil {
			return err
		}
	}

	listenOpt := fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", port)
	targetOpt := fmt.Sprintf("/p2p/%s", peerId)
	listen, err := ma.NewMultiaddr(listenOpt)

	if err != nil {
		fmt.Println(err)
		return err
	}

	targetAddrInfo, err := parseIpfsAddr(targetOpt)
	protoId := protocol.ID(protoOpt)

	c.P2P.ListenersP2P.Lock()
	defer c.P2P.ListenersP2P.Unlock()

	target, err := ma.NewMultiaddr(targetOpt)

	listeners := c.filterListener(c.P2P.ListenersLocal, func(listener ipfsp2p.Listener) bool {
		return listener.Protocol() == protoId && listener.ListenAddress().String() == listen.String() && listener.TargetAddress().String() == target.String()
	})

	if len(listeners) > 0 {
		return nil
	}
	err = forwardLocal(context.Background(), c.P2P, c.Host.Peerstore(), protoId, listen, targetAddrInfo)
	if err != nil {
		fmt.Println(err)
		return err
	}
	fmt.Println("======================")
	fmt.Println("forward : protoOpt: ", protoOpt)
	fmt.Println("forward : port: ", port)
	fmt.Println("forward : peerId: ", peerId)
	fmt.Println("======================")
	fmt.Println("remote_node" + peerId + ",forward to" + listenOpt + "success")
	return err
}

// CheckForwardHealth check if the remote node is connected
func (c *P2pClient) CheckForwardHealth(proto, peerId string) error {
	targetOpt := fmt.Sprintf("/p2p/%s", peerId)
	targets, err := parseIpfsAddr(targetOpt)
	protoId := protocol.ID(proto)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(context.Background(), time.Second*30) //TODO: configurable?
	defer cancel()
	stream, err := (c.Host).NewStream(cctx, targets.ID, protoId)
	if err != nil {
		return err
	} else {
		stream.Close()
		return nil
	}
}

func (c *P2pClient) filterListener(listeners *ipfsp2p.Listeners, matchFunc func(listener ipfsp2p.Listener) bool) []ipfsp2p.Listener {
	todo := make([]ipfsp2p.Listener, 0)
	for _, l := range listeners.Listeners {
		if matchFunc(l) {
			todo = append(todo, l)
		}
	}
	return todo

}

func (c *P2pClient) ConnectCircuit(circuitPeer, targetPeer string) error {
	maddr := ma.StringCast(fmt.Sprintf("/p2p/%s/p2p-circuit/p2p/%s", circuitPeer, targetPeer))
	pi, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return err
	}
	err = c.Host.Connect(context.Background(), *pi)
	if err != nil {
		return err
	}
	return nil
}

// Close turn off p2p listening connection
func (c *P2pClient) Close(target string) (int, error) {
	targetAddress, err := ma.NewMultiaddr(target)
	if err != nil {
		return 0, err
	}
	match := func(listener ipfsp2p.Listener) bool {

		if !targetAddress.Equal(listener.TargetAddress()) {
			return false
		}
		return true
	}

	done := c.P2P.ListenersLocal.Close(match)
	done += c.P2P.ListenersP2P.Close(match)

	return done, nil

}

// Destroy: destroy and close the p2p client, including all subordinate listeners, stream objects
func (c *P2pClient) Destroy() error {
	for _, stream := range c.P2P.Streams.Streams {
		c.P2P.Streams.Close(stream)
	}
	match := func(listener ipfsp2p.Listener) bool {
		return true
	}
	c.P2P.ListenersP2P.Close(match)
	c.P2P.ListenersLocal.Close(match)
	err := (c.Host).Close()
	c.P2P = nil
	c.Host = nil
	return err
}

// forwardLocal forwards local connections to a libp2p service
func forwardLocal(ctx context.Context, p *ipfsp2p.P2P, ps pstore.Peerstore, proto protocol.ID, bindAddr ma.Multiaddr, addr *peer.AddrInfo) error {

	ps.AddAddrs(addr.ID, addr.Addrs, pstore.TempAddrTTL)
	// TODO: return some info
	_, err := p.ForwardLocal(ctx, addr.ID, proto, bindAddr)
	return err
}

// parseIpfsAddr is a function that takes in addr string and return ipfsAddrs
func parseIpfsAddr(addr string) (*peer.AddrInfo, error) {
	multiaddr, err := ma.NewMultiaddr(addr)
	if err != nil {
		return nil, err
	}

	pi, err := peer.AddrInfoFromP2pAddr(multiaddr)
	if err == nil {
		return pi, nil
	}

	// resolve multiaddr whose protocol is not ma.P_IPFS
	ctx, cancel := context.WithTimeout(context.Background(), resolveTimeout)
	defer cancel()
	addrs, err := madns.Resolve(ctx, multiaddr)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, errors.New("fail to resolve the multiaddr:" + multiaddr.String())
	}
	var info peer.AddrInfo
	for _, addr := range addrs {
		taddr, id := peer.SplitAddr(addr)
		if id == "" {
			// not an ipfs addr, skipping.
			continue
		}
		switch info.ID {
		case "":
			info.ID = id
		case id:
		default:
			return nil, fmt.Errorf(
				"ambiguous multiaddr %s could refer to %s or %s",
				multiaddr,
				info.ID,
				id,
			)
		}
		info.Addrs = append(info.Addrs, taddr)
	}
	return &info, nil
}

func (s *P2pClient) ForwardWithRandomPort(peerId string) (string, string, error) {
	list, err := s.ListListen()
	if err != nil {
		fmt.Println("创建容器部署指令失败")
		fmt.Println("查询p2p 列表失败")
		return "", "", nil
	}

	t, find := lo.Find(list, func(item *ListenReply) bool {
		if item == nil {
			return false
		}
		return item.TargetAddress == fmt.Sprintf("/p2p/%s", peerId)
	})

	if find {
		listenAddress := t.ListenAddress
		// 定义正则表达式模式，用于匹配IP地址和端口号
		pattern := `\/ip4\/([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)\/tcp\/([0-9]+)`

		// 编译正则表达式
		regex := regexp.MustCompile(pattern)

		// 使用正则表达式来提取IP地址和端口号
		matches := regex.FindStringSubmatch(listenAddress)
		if len(matches) >= 3 {
			ip := matches[1]   // 第一个匹配组为IP地址
			port := matches[2] // 第二个匹配组为端口号

			fmt.Printf("IP地址: %s\n", ip)
			fmt.Printf("端口号: %s\n", port)
			return ip, port, nil
		} else {
			fmt.Println("无法提取IP地址和端口号")
		}
	}

	listenIp := "127.0.0.1"
	listenPort := rand.Intn(9999) + 30000

	if err != nil {
		return "", "", nil
	}
	targetOpt := fmt.Sprintf("/p2p/%s", peerId)
	proto := "/x/ssh"

	err = s.Forward(proto, listenPort, targetOpt)
	if err != nil {
		fmt.Println("创建容器部署指令失败")
		fmt.Println(err)
		return "", "", nil
	}
	return listenIp, strconv.Itoa(listenPort), nil

}

func (s *P2pClient) ListListen() ([]*ListenReply, error) {
	output := []*ListenReply{}

	s.P2P.ListenersLocal.Lock()
	for _, listener := range s.P2P.ListenersLocal.Listeners {
		output = append(output, &ListenReply{
			Protocol:      string(listener.Protocol()),
			ListenAddress: listener.ListenAddress().String(),
			TargetAddress: listener.TargetAddress().String(),
		})
	}
	s.P2P.ListenersLocal.Unlock()

	s.P2P.ListenersP2P.Lock()
	for _, listener := range s.P2P.ListenersP2P.Listeners {
		output = append(output, &ListenReply{
			Protocol:      string(listener.Protocol()),
			ListenAddress: listener.ListenAddress().String(),
			TargetAddress: listener.TargetAddress().String(),
		})
	}
	s.P2P.ListenersP2P.Unlock()
	return output, nil
}

type ListenReply struct {
	Protocol      string
	ListenAddress string
	TargetAddress string
}

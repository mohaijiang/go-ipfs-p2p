package p2p

import (
	"context"
	"fmt"
	config "github.com/ipfs/go-ipfs-config"
	"github.com/ipfs/go-ipfs/core"
	"github.com/ipfs/go-ipfs/core/node/libp2p"
	loader "github.com/ipfs/go-ipfs/plugin/loader"
	fsrepo "github.com/ipfs/go-ipfs/repo/fsrepo"
	"github.com/ipfs/interface-go-ipfs-core/options"
	"log"
	"os"
	"path/filepath"
	"sort"
)

const SwarmKey = "/key/swarm/psk/1.0.0/\n/base16/\n2108249f85354ed11ecf999a4500e9b616f71516b6c222ce630d14e434ef5562"

func init() {
	ipfsPath, err := fsrepo.BestKnownPath()
	plugins, err := loader.NewPluginLoader(ipfsPath)
	if err != nil {
		fmt.Errorf("error loading plugins: %s", err)
	}

	if err := plugins.Initialize(); err != nil {
		log.Default().Printf("error initializing plugins: %s", err)
	}

	if err := plugins.Inject(); err != nil {
		log.Default().Printf("error initializing plugins: %s", err)
	}
}

func RunDaemon() (*core.IpfsNode, func(), error) {

	ctx := context.Background()
	ipfsPath, err := fsrepo.BestKnownPath()

	if !fsrepo.IsInitialized(ipfsPath) {
		identity, err := config.CreateIdentity(os.Stdout, []options.KeyGenerateOption{
			options.Key.Type(options.Ed25519Key),
		})
		if err != nil {
			log.Default().Println("create identity error : ", err)
			return nil, nil, err
		}
		conf, err := config.InitWithIdentity(identity)
		if err != nil {
			log.Default().Println("InitWithIdentity error: ", err)
			return nil, nil, err
		}

		conf.Bootstrap = []string{"/ip4/61.172.179.6/tcp/32002/p2p/12D3KooWJtZ7RNoMavfcS2HnRfgp7wXxtXrukpsHaHprF2kzma6u"}
		//conf.Swarm.RelayClient.Enabled = config.True
		//conf.Swarm.RelayService.Enabled = config.True
		conf.Experimental.Libp2pStreamMounting = true

		err = fsrepo.Init(
			ipfsPath,
			conf,
		)
		if err != nil {
			log.Default().Println("fsrepo  init fail : ", err)
			return nil, nil, err
		}
	}
	swarmKeyFile := filepath.Join(ipfsPath, "swarm.key")

	_, err = os.Lstat(swarmKeyFile)
	if err != nil {
		err = os.WriteFile(swarmKeyFile, []byte(SwarmKey), 0644)
		if err != nil {
			log.Default().Println("init swarm.key fail", err)
			return nil, nil, err
		}
	}

	repo, err := fsrepo.Open(ipfsPath)
	if err != nil {
		log.Default().Println("fsrepo is not initialization: ", err)
		return nil, nil, err
	}
	ncfg := &core.BuildCfg{
		Repo:                        repo,
		Permanent:                   true,
		Online:                      true,
		DisableEncryptedConnections: false,
		ExtraOpts: map[string]bool{
			"pubsub": false,
			"ipnsps": false,
		},
		Routing: libp2p.DHTOption,
	}

	node, err := core.NewNode(ctx, ncfg)
	if err != nil {
		log.Default().Println("error from node construction: ", err)
		return nil, nil, err
	}
	node.IsDaemon = true

	printSwarmAddrs(node)
	cleanup := func() {
		_ = node.Close()
	}
	return node, cleanup, nil
}

// printSwarmAddrs prints the addresses of the host

func printSwarmAddrs(node *core.IpfsNode) {
	if !node.IsOnline {
		fmt.Println("Swarm not listening, running in offline mode.")
		return
	}

	var lisAddrs []string
	ifaceAddrs, err := node.PeerHost.Network().InterfaceListenAddresses()
	if err != nil {
		log.Default().Printf("failed to read listening addresses: %s", err)
	}
	for _, addr := range ifaceAddrs {
		lisAddrs = append(lisAddrs, addr.String())
	}
	sort.Strings(lisAddrs)
	for _, addr := range lisAddrs {
		fmt.Printf("Swarm listening on %s\n", addr)
	}

	var addrs []string
	for _, addr := range node.PeerHost.Addrs() {
		addrs = append(addrs, addr.String())
	}
	sort.Strings(addrs)
	for _, addr := range addrs {
		fmt.Printf("Swarm announcing %s\n", addr)
	}
}

type P2pService struct {
	node *core.IpfsNode
}

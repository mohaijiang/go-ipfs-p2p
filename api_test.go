package go_ipfs_p2p

import (
	"encoding/base64"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestProvider(t *testing.T) {

	const SwarmKey = "/key/swarm/psk/1.0.0/\n/base16/\n55158d9b6b7e5a8e41aa8b34dd057ff1880e38348613d27ae194ad7c5b9670d7"

	const BootStrap = "/ip4/34.139.126.73/tcp/4001/p2p/12D3KooWRsKNAgbGaQkVbbzg5xEw2FtvPRF7MiYtmRvFPYegNVnu"
	priv, _, err := crypto.GenerateKeyPair(crypto.RSA, 2048)
	skbytes, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		panic(err)
	}
	privateKey := base64.StdEncoding.EncodeToString(skbytes)

	node, err := NewP2pClient(4002, privateKey, SwarmKey, []string{BootStrap})
	assert.NoError(t, err)

	err = node.Listen("/x/ssh", "/ip4/127.0.0.1/tcp/80")

	if err != nil {
		panic(err)
	}

	select {}

}

func TestClient(t *testing.T) {

	const SwarmKey = "/key/swarm/psk/1.0.0/\n/base16/\n55158d9b6b7e5a8e41aa8b34dd057ff1880e38348613d27ae194ad7c5b9670d7"

	const BootStrap = "/ip4/34.139.126.73/tcp/4001/p2p/12D3KooWRsKNAgbGaQkVbbzg5xEw2FtvPRF7MiYtmRvFPYegNVnu"
	priv, _, err := crypto.GenerateKeyPair(crypto.RSA, 2048)
	skbytes, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		panic(err)
	}
	privateKey := base64.StdEncoding.EncodeToString(skbytes)

	node, err := NewP2pClient(4003, privateKey, SwarmKey, []string{BootStrap})
	assert.NoError(t, err)

	err = node.Forward("/x/ssh", 8000, "QmVPfFi4j2MnDnxAFfT8rBVMsq9jfte2Ti5RJPBRRiskKi")

	if err != nil {
		panic(err)
	}

	select {}

}

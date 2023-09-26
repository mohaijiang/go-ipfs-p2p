package go_ipfs_p2p

import (
	"fmt"
	"github.com/mohaijiang/go-ipfs-p2p/p2p"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestDaemon(t *testing.T) {

	node, _, err := p2p.RunDaemon()
	assert.NoError(t, err)

	fmt.Println(node)

}

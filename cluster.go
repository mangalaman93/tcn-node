package main

import (
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"path"
	"strconv"

	config "github.com/ipfs/go-ipfs/repo/config"
	fsrepo "github.com/ipfs/go-ipfs/repo/fsrepo"
	ci "gx/ipfs/QmUBogf4nUefBjmYjn6jfsfPJRkmDGSeMhNj4usRKq69f4/go-libp2p/p2p/crypto"
	peer "gx/ipfs/QmUBogf4nUefBjmYjn6jfsfPJRkmDGSeMhNj4usRKq69f4/go-libp2p/p2p/peer"
)

const (
	nBitsForKeypairDefault = 2048
	defaultSwarmPort       = 4000
	defaultAPIPort         = 5000
	defaultGWPort          = 8080
)

// initializes a new identity
func identityConfig(nbits int) (config.Identity, error) {
	// TODO guard higher up
	ident := config.Identity{}
	if nbits < 1024 {
		return ident, errors.New("Bitsize less than 1024 is considered unsafe.")
	}

	fmt.Printf("generating %v-bit RSA keypair...", nbits)
	sk, pk, err := ci.GenerateKeyPair(ci.RSA, nbits)
	if err != nil {
		return ident, err
	}
	fmt.Println("done")

	// currently storing key unencrypted. in the future we need to encrypt it.
	// TODO(security)
	skbytes, err := sk.Bytes()
	if err != nil {
		return ident, err
	}
	ident.PrivKey = base64.StdEncoding.EncodeToString(skbytes)

	id, err := peer.IDFromPublicKey(pk)
	if err != nil {
		return ident, err
	}
	ident.PeerID = id.Pretty()
	fmt.Println("peer identity:", ident.PeerID)
	return ident, nil
}

func InitConfig(identity config.Identity, bootstrapPeers []config.BootstrapPeer,
	swarmPort, apiPort, gwPort int) (*config.Config, error) {
	return &config.Config{
		Addresses: config.Addresses{
			Swarm:   []string{fmt.Sprintf("/ip4/0.0.0.0/tcp/%v", swarmPort)},
			API:     fmt.Sprintf("/ip4/0.0.0.0/tcp/%v", apiPort),
			Gateway: fmt.Sprintf("/ip4/0.0.0.0/tcp/%v", gwPort),
		},

		Bootstrap: config.BootstrapPeerStrings(bootstrapPeers),
		// SupernodeRouting: *snr,
		Identity: identity,
		Discovery: config.Discovery{config.MDNS{
			Enabled:  true,
			Interval: 10,
		}},

		// setup the node mount points.
		Mounts: config.Mounts{
			IPFS: "/ipfs",
			IPNS: "/ipns",
		},

		Ipns: config.Ipns{
			ResolveCacheSize: 128,
		},

		// tracking ipfs version used to generate the init folder and adding
		// update checker default setting.
		Version: config.VersionDefaultValue(),

		Gateway: config.Gateway{
			RootRedirect: "",
			Writable:     false,
		},
	}, nil
}

func main() {
	var nnodes int
	var rootFolder string
	flag.IntVar(&nnodes, "n", 5, "number of nodes in cluster")
	flag.StringVar(&rootFolder, "p", ".tcn", "root folder to place all the ipfs content")
	flag.Parse()

	// generate the list of default peers
	BootstrapAddresses := []string{}
	AllIdentities := []config.Identity{}
	for i := 0; i < nnodes; i++ {
		identity, err := identityConfig(nBitsForKeypairDefault)
		if err != nil {
			panic(err)
		}

		peer := fmt.Sprintf("/ip4/127.0.0.1/tcp/%v/ipfs/%s", defaultSwarmPort+i, identity.PeerID)
		BootstrapAddresses = append(BootstrapAddresses, peer)
		AllIdentities = append(AllIdentities, identity)
	}

	bootstrapPeers, err := config.ParseBootstrapPeers(BootstrapAddresses)
	if err != nil {
		panic(err)
	}

	for i := 0; i < nnodes; i++ {
		fmt.Println("Setting up config for node", i)
		repoRoot := path.Join(rootFolder, strconv.Itoa(i))
		if fsrepo.IsInitialized(repoRoot) {
			panic(fmt.Sprintf("Node %v already initialized", i))
		}

		fmt.Println("Initializing ipfs node at:", repoRoot)
		conf, err := InitConfig(AllIdentities[i], bootstrapPeers, defaultSwarmPort+i,
			defaultAPIPort+i, defaultGWPort+i)
		if err != nil {
			panic(err)
		}

		if err := fsrepo.Init(repoRoot, conf); err != nil {
			panic(err)
		}
		fmt.Println("Initialization complete for node", i)
	}
}

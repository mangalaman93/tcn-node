package main

import (
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"strconv"
	"syscall"

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
	swarmPort, apiPort, gwPort int, ipfs_mount_path, ipns_mount_path string) (*config.Config, error) {
	return &config.Config{
		Addresses: config.Addresses{
			Swarm:   []string{fmt.Sprintf("/ip4/127.0.0.1/tcp/%v", swarmPort)},
			API:     fmt.Sprintf("/ip4/127.0.0.1/tcp/%v", apiPort),
			Gateway: fmt.Sprintf("/ip4/127.0.0.1/tcp/%v", gwPort),
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
			IPFS: ipfs_mount_path,
			IPNS: ipns_mount_path,
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

func runDaemon(repoPath string) (*exec.Cmd, io.ReadCloser, io.ReadCloser) {
	ipfsbin, err := exec.LookPath("ipfs")
	if err != nil {
		panic(err)
	}

	cmd := exec.Command(ipfsbin, "daemon", "--writable")
	env := os.Environ()
	env = append(env, fmt.Sprintf("IPFS_PATH=%s", repoPath))
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		panic(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		panic(err)
	}
	err = cmd.Start()
	if err != nil {
		panic(err)
	}

	return cmd, stdout, stderr
}

func cleanupDaemon(cmd *exec.Cmd, stdout, stderr io.ReadCloser) {
	cmd.Process.Signal(syscall.SIGINT)
	fmt.Println("--------------------------------------")

	out, _ := ioutil.ReadAll(stdout)
	fmt.Println("STDOUT:")
	fmt.Println(string(out))

	out, _ = ioutil.ReadAll(stderr)
	fmt.Println("STDERR:")
	fmt.Println(string(out))

	err := cmd.Wait()
	fmt.Println("exiting daemon with status:", err)
}

func main() {
	var nnodes int
	var rootFolder string
	flag.IntVar(&nnodes, "n", 5, "number of nodes in cluster")
	flag.StringVar(&rootFolder, "p", ".tcn", "root folder to place all the ipfs content")
	flag.Parse()

	BootstrapAddresses := []string{}
	AllIdentities := []config.Identity{}
	if _, err := os.Stat(rootFolder); err == nil {
		for i := 0; i < nnodes; i++ {
			repoRoot := path.Join(rootFolder, strconv.Itoa(i))
			if !fsrepo.IsInitialized(repoRoot) {
				panic(fmt.Sprintf("Node %v is not initialized", i))
			}
		}
	} else {
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
			repoRoot := path.Join(rootFolder, strconv.Itoa(i))
			ipfs_mount_path := path.Join(rootFolder, fmt.Sprintf("ipfs%v", i))
			ipns_mount_path := path.Join(rootFolder, fmt.Sprintf("ipns%v", i))
			conf, err := InitConfig(AllIdentities[i], bootstrapPeers, defaultSwarmPort+i,
				defaultAPIPort+i, defaultGWPort+i, ipfs_mount_path, ipns_mount_path)
			if err != nil {
				panic(err)
			}

			if err := fsrepo.Init(repoRoot, conf); err != nil {
				panic(err)
			}
			fmt.Println("Initialized ipfs node at:", repoRoot)
		}
	}

	// run ipfs daemons
	for i := 0; i < nnodes; i++ {
		repoPath := path.Join(rootFolder, strconv.Itoa(i))
		defer cleanupDaemon(runDaemon(repoPath))
		fmt.Println("Started node", i)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT)
	<-sigs
	fmt.Println("cleaning up ...")
}

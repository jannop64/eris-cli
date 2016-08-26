package chains

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	// "strings"

	"github.com/eris-ltd/eris-cli/config"
	// "github.com/eris-ltd/eris-cli/data"
	"github.com/eris-ltd/eris-cli/definitions"
	"github.com/eris-ltd/eris-cli/loaders"
	"github.com/eris-ltd/eris-cli/perform"
	"github.com/eris-ltd/eris-cli/services"
	"github.com/eris-ltd/eris-cli/util"
	// "github.com/eris-ltd/eris-cli/version"

	. "github.com/eris-ltd/common/go/common"
	"github.com/eris-ltd/common/go/ipfs"
	log "github.com/eris-ltd/eris-logger"
	cm_definitions "github.com/eris-ltd/eris-cm/definitions"
	cm_maker "github.com/eris-ltd/eris-cm/maker"
	cm_util "github.com/eris-ltd/eris-cm/util"
	keys "github.com/eris-ltd/eris-cm/Godeps/_workspace/src/github.com/eris-ltd/eris-keys/eris-keys"
)

// MakeChain runs the `eris-cm make` command in a docker container.
// It returns an error. Note that if do.Known, do.AccountTypes
// or do.ChainType are not set the command will run via interactive
// shell.
//
//  do.Name          - name of the chain to be created (required)
//  do.Known         - will use the mintgen tool to parse csv's and create a genesis.json (requires do.ChainMakeVals and do.ChainMakeActs) (optional)
//  do.Output        - [csk] don't remember what this does
//  do.ChainMakeVals - csv file to use for validators (optional)
//  do.ChainMakeActs - csv file to use for accounts (optional)
//  do.AccountTypes  - use eris-cm make account-types paradigm (example: Root:1,Participants:25,...) (optional)
//  do.ChainType     - use eris-cm make chain-types paradigm (example: simplechain) (optional)
//  do.Tarball       - instead of outputing raw files in directories, output packages of tarbals (optional)
//  do.ZipFile       - similar to do.Tarball except uses zipfiles (optional)
//  do.Verbose       - verbose output (optional)
//  do.Debug         - debug output (optional)
//
func MakeChain(do *definitions.Do) error {
	// precursor functions
	if err := checkKeysRunningOrStart(); err != nil {
		return err
	}
	// loop through chains directories to make sure they exist & are appropriately populated
	for _, d := range ChainsDirs {
		if _, err := os.Stat(d); os.IsNotExist(err) {
			os.MkdirAll(d, 0755)
		}
	}
	if err := cm_util.CheckDefaultTypes(AccountsTypePath, "account-types"); err != nil {
		return err
	}
	if err := cm_util.CheckDefaultTypes(ChainTypePath, "chain-types"); err != nil {
		return err
	}

	// announce.
	log.Info("Hello! I'm the marmot who makes eris chains.")
	maker := cm_definitions.NowDo()
	keys.DaemonAddr = "http://172.17.0.2:4767" // tmp

	// todo. clean this up... struct merge them or something
	maker.Name = do.Name
	maker.Verbose = do.Verbose
	maker.Debug = do.Debug
	maker.ChainType = do.ChainType
	maker.AccountTypes = do.AccountTypes
	maker.Zip = do.ZipFile
	maker.Tarball = do.Tarball
	maker.Output = do.Output
	if do.Known {
		maker.CSV = fmt.Sprintf("%s,%s", do.ChainMakeVals, do.ChainMakeActs)
	}

	// make it
	if err := cm_maker.MakeChain(maker); err != nil {
		return err
	}

	// cm currently is not opinionated about its writers.
	if maker.Tarball {
		if err := cm_util.Tarball(maker); err != nil {
			return err
		}
	} else if maker.Zip {
		if err := cm_util.Zip(maker); err != nil {
			return err
		}
	}
	if maker.Output {
		if err := cm_util.SaveAccountResults(maker); err != nil {
			return err
		}
	}

	return nil
}

// InspectChain is eris' version of docker inspect. It returns
// an error.
//
//  do.Name            - name of the chain to inspect (required)
//  do.Operations.Args - fields to inspect in the form Major.Minor or "all" (required)
//
func InspectChain(do *definitions.Do) error {
	chain, err := loaders.LoadChainDefinition(do.Name)
	if err != nil {
		return err
	}

	if util.IsChain(chain.Name, false) {
		log.WithField("=>", chain.Service.Name).Debug("Inspecting chain")
		err := services.InspectServiceByService(chain.Service, chain.Operations, do.Operations.Args[0])
		if err != nil {
			return err
		}
	}

	return nil
}

// LogsChain returns the logs of a chains' service container
// for display by the user.
//
//  do.Name    - name of the chain (required)
//  do.Follow  - follow the logs until the user sends SIGTERM (optional)
//  do.Tail    - number of lines to display (can be "all") (optional)
//
func LogsChain(do *definitions.Do) error {
	chain, err := loaders.LoadChainDefinition(do.Name)
	if err != nil {
		return err
	}

	err = perform.DockerLogs(chain.Service, chain.Operations, do.Follow, do.Tail)
	if err != nil {
		return err
	}

	return nil
}

// CheckoutChain writes to the ChainPath/HEAD file the name
// of the chain to be "checked out". It returns an error. This
// operates similar to git branches and is predominantly a
// scoping function which is used by other portions of the
// platform where a --chain flag may otherwise be used.
//
//  do.Name - the name of the chain to checkout; if blank will "uncheckout" current chain (optional)
//
func CheckoutChain(do *definitions.Do) error {
	if do.Name == "" {
		do.Result = "nil"
		return util.NullHead()
	}

	curHead, _ := util.GetHead()
	if do.Name == curHead {
		do.Result = "no change"
		return nil
	}

	return util.ChangeHead(do.Name)
}

// CurrentChain displays the currently in scope (or checked out) chain. It
// returns an error (which should never be triggered)
//
func CurrentChain(do *definitions.Do) error {
	head, _ := util.GetHead()

	if head == "" {
		head = "There is no chain checked out."
	}

	log.Warn(head)
	do.Result = head

	return nil
}

// CatChain displays chain information. It returns nil on success, or input/output
// errors otherwise.
//
//  do.Name - chain name
//  do.Type - "toml", "genesis", "status", "validators", or "toml"
//
func CatChain(do *definitions.Do) error {
	rootDir := path.Join(ErisContainerRoot, "chains", do.Name)
	switch do.Type {
	case "genesis":
		do.Operations.Args = []string{"cat", path.Join(rootDir, "genesis.json")}
	case "config":
		do.Operations.Args = []string{"cat", path.Join(rootDir, "config.toml")}
	case "status":
		do.Operations.Args = []string{"mintinfo", "--node-addr", "http://chain:46657", "status"}
	case "validators":
		do.Operations.Args = []string{"mintinfo", "--node-addr", "http://chain:46657", "validators"}
	case "toml":
		cat, err := ioutil.ReadFile(filepath.Join(ChainsPath, do.Name, do.Name+".toml"))
		if err != nil {
			return err
		}
		config.GlobalConfig.Writer.Write(cat)
		return nil
	default:
		return fmt.Errorf("unknown cat subcommand %q", do.Type)
	}
	do.Operations.PublishAllPorts = true
	log.WithField("args", do.Operations.Args).Debug("Executing command")

	buf, err := ExecChain(do)

	if buf != nil {
		io.Copy(config.GlobalConfig.Writer, buf)
	}

	return err
}

// PortsChain displays the port mapping for a particular chain.
// It returns an error.
//
//  do.Name - name of the chain to display port mappings for (required)
//
func PortsChain(do *definitions.Do) error {
	chain, err := loaders.LoadChainDefinition(do.Name)
	if err != nil {
		return err
	}

	if util.IsChain(chain.Name, false) {
		log.WithField("=>", chain.Name).Debug("Getting chain port mapping")
		return util.PrintPortMappings(chain.Operations.SrvContainerName, do.Operations.Args)
	}

	return nil
}

func UpdateChain(do *definitions.Do) error {
	chain, err := loaders.LoadChainDefinition(do.Name)
	if err != nil {
		return err
	}

	// set the right env vars and command
	if util.IsChain(chain.Name, true) {
		chain.Service.Environment = []string{fmt.Sprintf("CHAIN_ID=%s", do.Name)}
		chain.Service.Environment = append(chain.Service.Environment, do.Env...)
		chain.Service.Links = append(chain.Service.Links, do.Links...)
		chain.Service.Command = loaders.ErisChainStart
	}

	err = perform.DockerRebuild(chain.Service, chain.Operations, do.Pull, do.Timeout)
	if err != nil {
		return err
	}
	return nil
}

func RemoveChain(do *definitions.Do) error {
	chain, err := loaders.LoadChainDefinition(do.Name)
	if err != nil {
		return err
	}

	if util.IsChain(chain.Name, false) {
		if err = perform.DockerRemove(chain.Service, chain.Operations, do.RmD, do.Volumes, do.Force); err != nil {
			return err
		}
	} else {
		log.Info("Chain container does not exist")
	}

	if do.RmHF {
		dirPath := filepath.Join(ChainsPath, do.Name) // the dir

		log.WithField("directory", dirPath).Warn("Removing directory")
		if err := os.RemoveAll(dirPath); err != nil {
			return err
		}
	}

	return nil
}

func exportFile(chainName string) (string, error) {
	fileName := util.GetFileByNameAndType("chains", chainName)

	return ipfs.SendToIPFS(fileName, "", "")
}

// TODO: remove
func RegisterChain(do *definitions.Do) error {
	// do.Name is mandatory
	if do.Name == "" {
		return fmt.Errorf("RegisterChain requires a chainame")
	}
	etcbChain := do.ChainID
	do.ChainID = do.Name

	// NOTE: registration expects you to have the data container
	if !util.IsData(do.Name) {
		return fmt.Errorf("Registration requires you to have a data container for the chain. Could not find data for %s", do.Name)
	}

	chain, err := loaders.LoadChainDefinition(do.Name)
	if err != nil {
		return err
	}
	log.WithField("image", chain.Service.Image).Debug("Chain loaded")

	// set chainid and other vars
	envVars := []string{
		fmt.Sprintf("CHAIN_ID=%s", do.ChainID),                 // of the etcb chain
		fmt.Sprintf("PUBKEY=%s", do.Pubkey),                    // pubkey to register chain with
		fmt.Sprintf("ETCB_CHAIN_ID=%s", etcbChain),             // chain id of the etcb chain
		fmt.Sprintf("NODE_ADDR=%s", do.Gateway),                // etcb node to send the register tx to
		fmt.Sprintf("NEW_P2P_SEEDS=%s", do.Operations.Args[0]), // seeds to register for the chain // TODO: deal with multi seed (needs support in tendermint)
	}
	envVars = append(envVars, do.Env...)

	log.WithFields(log.Fields{
		"environment": envVars,
		"links":       do.Links,
	}).Debug("Registering chain with")
	chain.Service.Environment = append(chain.Service.Environment, envVars...)
	chain.Service.Links = append(chain.Service.Links, do.Links...)

	if err := bootDependencies(chain, do); err != nil {
		return err
	}

	log.WithFields(log.Fields{
		"=>":    chain.Service.Name,
		"image": chain.Service.Image,
	}).Debug("Performing chain container start")
	chain.Operations = loaders.LoadDataDefinition(chain.Service.Name)
	chain.Operations.Args = []string{loaders.ErisChainRegister}

	_, err = perform.DockerRunData(chain.Operations, chain.Service)

	return err
}

func checkKeysRunningOrStart() error {
	srv, err := loaders.LoadServiceDefinition("keys")
	if err != nil {
		return err
	}

	if !util.IsService(srv.Service.Name, true) {
		do := definitions.NowDo()
		do.Operations.Args = []string{"keys"}
		if err := services.StartService(do); err != nil {
			return err
		}
	}
	return nil
}

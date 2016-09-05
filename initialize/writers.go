package initialize

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/eris-ltd/eris-cli/config"
	"github.com/eris-ltd/eris-cli/util"

	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/docker/pkg/term"

	ver "github.com/eris-ltd/eris-cli/version"

	"github.com/eris-ltd/common/go/common"
	log "github.com/eris-ltd/eris-logger"
	docker "github.com/fsouza/go-dockerclient"
)

// XXX all files in this sequence must be added to both
// the respective GH repo & mindy testnet (pinkpenguin.interblock.io:46657/list_names)
func dropServiceDefaults(dir string, services []string) error {
	for _, service := range services {
		var err error

		switch service {
		case "keys":
			err = writeDefaultFile(common.ServicesPath, "keys.toml", defServiceKeys)
		case "ipfs":
			err = writeDefaultFile(common.ServicesPath, "ipfs.toml", defServiceIPFS)
		default:
			err = drops([]string{service}, "services", dir)
		}
		if err != nil {
			return fmt.Errorf("Cannot add default %s: %v", service, err)
		}
	}

	return nil
}

func dropChainDefaults(dir string) error {
	if err := drops(ver.CHAIN_DEFINITIONS, "chains", dir); err != nil {
		return err
	}

	// common.DefaultChainDir goes to $HOME/.eris
	// rather than /tmp/eris/.eris
	// XXX something wonky with ResolveErisRoot()?
	// TODO: refactor so it uses chainsMake .... somehow
	chnDir := filepath.Join(dir, "default")
	if err := writeDefaultFile(chnDir, "genesis.json", DefChainGen); err != nil {
		return fmt.Errorf("Cannot add default genesis.json: %s.\n", err)
	}
	if err := writeDefaultFile(chnDir, "priv_validator.json", DefChainKeys); err != nil {
		return fmt.Errorf("Cannot add default priv_validator.json: %s.\n", err)
	}
	if err := writeDefaultFile(chnDir, "genesis.csv", DefChainCSV); err != nil {
		return fmt.Errorf("Cannot add default genesis.csv: %s.\n", err)
	}

	//insert version into default chain service definition
	versionDefault := filepath.Join(dir, "default.toml")
	read, err := ioutil.ReadFile(versionDefault)
	if err != nil {
		return err
	}
	withVersion := strings.Replace(string(read), "version", ver.VERSION, 2)
	if err := ioutil.WriteFile(versionDefault, []byte(withVersion), 0); err != nil {
		return err
	}

	//move things to where they ought to be
	config := filepath.Join(dir, "config.toml")
	configDef := filepath.Join(chnDir, "config.toml")
	if err := os.Rename(config, configDef); err != nil {
		return err
	}

	server := filepath.Join(dir, "server_conf.toml")
	serverDef := filepath.Join(chnDir, "server_conf.toml")
	if err := os.Rename(server, serverDef); err != nil {
		return err
	}
	return nil
}

func pullDefaultImages() error {
	images := []string{
		config.Global.ImageData,
		config.Global.ImageKeys,
		config.Global.ImageIPFS,
		config.Global.ImageDB,
		config.Global.ImagePM,
		config.Global.ImageCM,
	}

	// Spacer.
	log.Warn()

	log.Warn("Pulling default Docker images from quay.io")

	// XXX can't use perform.PullImage b/c import cycle :(
	// it's essentially re-implemented here w/ a bit more opinion
	// fail over to docker hub is quay is down/firewalled
	auth := docker.AuthConfiguration{}

	for i, image := range images {
		var tag string = "latest"

		nameSplit := strings.Split(image, ":")
		if len(nameSplit) == 2 {
			tag = nameSplit[1]
		}
		if len(nameSplit) == 3 {
			tag = nameSplit[2]
		}
		image = nameSplit[0]
		img := path.Join(config.Global.DefaultRegistry, image)

		r, w := io.Pipe()
		opts := docker.PullImageOptions{
			Repository:    img,
			Registry:      config.Global.DefaultRegistry,
			Tag:           tag,
			OutputStream:  w,
			RawJSONStream: true,
		}

		if os.Getenv("ERIS_PULL_APPROVE") == "true" {
			opts.OutputStream = ioutil.Discard
		}

		log.WithField("image", img).Warnf("Pulling image %d out of %d", i+1, len(images))

		ch := make(chan error)
		timeout := make(chan error)
		go func() {
			defer w.Close()
			defer close(ch)

			if err := util.DockerClient.PullImage(opts, auth); err != nil {
				opts.Repository = image
				opts.Registry = ver.BackupRegistry //not in global config...(also, won't even work unless we build & push updated images to dockerhub in addition to quay (which we should do)
				if err := util.DockerClient.PullImage(opts, auth); err != nil {
					ch <- util.DockerError(err)
				}
			}
		}()
		go func() {
			defer w.Close()
			defer close(timeout)

			select {
			case <-time.After(5 * time.Minute):
				timeout <- fmt.Errorf(`
It looks like marmots are taking too long to download the necessary images...
Please, try restarting the [eris init] command one more time now or a bit later.
This is likely a network performance issue with our Docker hosting provider`)
			}
		}()
		go jsonmessage.DisplayJSONMessagesStream(r, os.Stdout, os.Stdout.Fd(), term.IsTerminal(os.Stdout.Fd()), nil)
		select {
		case err := <-ch:
			if err != nil {
				return err
			}
		case err := <-timeout:
			return err
		}

		// Spacer.
		log.Warn()
	}
	return nil
}

func drops(files []string, typ, dir string) error {
	//to get from github
	var repo string
	if typ == "services" {
		repo = "eris-services"
	} else if typ == "chains" {
		repo = "eris-chains"
	}
	// on different arch
	archPrefix := ""
	if runtime.GOARCH == "arm" {
		archPrefix = "arm/"
	}

	if !util.DoesDirExist(dir) {
		if err := os.MkdirAll(dir, 0777); err != nil {
			return err
		}
	}

	for _, file := range files {
		log.WithField(file, dir).Debug("Getting file from GitHub, dropping into")
		if err := util.GetFromGithub("eris-ltd", repo, "master", archPrefix+file+".toml", dir, file+".toml"); err != nil {
			return err
		}
	}
	return nil
}

//TODO eventually eliminate this
func writeDefaultFile(savePath, fileName string, toWrite func() string) error {
	if err := os.MkdirAll(savePath, 0777); err != nil {
		return err
	}
	pth := filepath.Join(savePath, fileName)
	writer, err := os.Create(pth)
	defer writer.Close()
	if err != nil {
		return err
	}
	writer.Write([]byte(toWrite()))
	return nil
}

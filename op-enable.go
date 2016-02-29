package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Azure/azure-docker-extension/pkg/driver"
	"github.com/Azure/azure-docker-extension/pkg/executil"
	"github.com/Azure/azure-docker-extension/pkg/util"
	"github.com/Azure/azure-docker-extension/pkg/vmextension"

	yaml "github.com/cloudfoundry-incubator/candiedyaml"
)

const (
	composeUrl         = "https://github.com/docker/compose/releases/download/1.6.2/docker-compose-Linux-x86_64"
	composeBin         = "docker-compose"
	composeTimeoutSecs = 600

	composeYml     = "docker-compose.yml"
	composeYmlDir  = "/etc/docker/compose"
	composeProject = "compose" // prefix for compose-created containers

	dockerCfgDir  = "/etc/docker"
	dockerCaCert  = "ca.pem"
	dockerSrvCert = "cert.pem"
	dockerSrvKey  = "key.pem"
)

func enable(he vmextension.HandlerEnvironment, d driver.DistroDriver) error {
	// Install docker daemon
	log.Printf("++ install docker")
	if _, err := exec.LookPath("docker"); err == nil {
		log.Printf("docker already installed. not re-installing")
	} else {
		// TODO(ahmetb) Temporary retry logic around installation for serialization
		// problem in Azure VM Scale Sets. In case of scale-up event, the new VM with
		// multiple extensions (such as Linux Diagnostics and Docker Extension) will install
		// the extensions in parallel and that will result in non-deterministic
		// acquisition of dpkg lock (apt-get install) and thus causing one of the
		// extensions to fail.
		//
		// Adding this temporary retry logic just for Linux Diagnostics extension
		// assuming it will take at most 5 minutes to be done with apt-get lock.
		//
		// This retry logic should be removed once the issue is fixed on the resource
		// provider layer.

		var (
			nRetries      = 6
			retryInterval = time.Minute * 1
		)

		for nRetries > 0 {
			if err := d.InstallDocker(); err != nil {
				nRetries--
				if nRetries == 0 {
					return err
				}
				log.Printf("install failed. remaining attempts=%d. error=%v", nRetries, err)
				log.Printf("sleeping %s", retryInterval)
				time.Sleep(retryInterval)
			} else {
				break
			}
		}
	}
	log.Printf("-- install docker")

	// Install docker-compose
	log.Printf("++ install docker-compose")
	if err := installCompose(composeBinPath(d)); err != nil {
		return fmt.Errorf("error installing docker-compose: %v", err)
	}
	log.Printf("-- install docker-compose")

	// Add user to 'docker' group to user docker as non-root
	u, err := util.GetAzureUser()
	if err != nil {
		return fmt.Errorf("failed to get provisioned user: %v", err)
	}
	log.Printf("++ add user to docker group")
	if out, err := executil.Exec("usermod", "-aG", "docker", u); err != nil {
		log.Printf("%s", string(out))
		return err
	}
	log.Printf("-- add user to docker group")

	settings, err := parseSettings(he.HandlerEnvironment.ConfigFolder)
	if err != nil {
		return err
	}

	// Install docker remote access certs
	log.Printf("++ setup docker certs")
	if err := installDockerCerts(*settings, dockerCfgDir); err != nil {
		return fmt.Errorf("error installing docker certs: %v", err)
	}
	log.Printf("-- setup docker certs")

	// Update dockeropts
	log.Printf("++ update dockeropts")
	restartNeeded, err := updateDockerOpts(d, getArgs(*settings, d))
	if err != nil {
		return fmt.Errorf("failed to update dockeropts: %v", err)
	}
	log.Printf("restart needed: %v", restartNeeded)
	log.Printf("-- update dockeropts")

	// Restart docker
	log.Printf("++ restart docker")
	if !restartNeeded {
		log.Printf("no restart needed. issuing only a start command.")
		_ = d.StartDocker() // ignore error as it already may be running due to multiple calls to enable
	} else {
		log.Printf("restarting docker-engine")
		if err := d.RestartDocker(); err != nil {
			return err
		}
	}
	time.Sleep(3 * time.Second) // wait for instance to come up
	log.Printf("-- restart docker")

	// Login Docker registry server
	log.Printf("++ login docker registry")
	if err := loginRegistry(settings.Login); err != nil {
		return err
	}
	log.Printf("-- login docker registry")

	// Compose Up
	log.Printf("++ compose up")
	if err := composeUp(d, settings.ComposeJson, settings.Environment); err != nil {
		return fmt.Errorf("'compose up' failed: %v", err)
	}
	log.Printf("-- compose up")
	return nil
}

// installCompose download docker-compose and saves to the specified path if it
// is not already installed.
func installCompose(path string) error {
	// Check if already installed at path.
	if ok, err := util.PathExists(path); err != nil {
		return err
	} else if ok {
		log.Printf("docker-compose is already installed at %s", path)
		return nil
	}

	// Create dir if not exists
	dir := filepath.Dir(path)
	ok, err := util.PathExists(dir)
	if err != nil {
		return err
	} else if !ok {
		if err := os.MkdirAll(dir, 755); err != nil {
			return err
		}
	}

	log.Printf("Downloading compose from %s", composeUrl)
	resp, err := http.Get(composeUrl)
	if err != nil {
		return fmt.Errorf("error downloading docker-compose: %v", err)
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("response status code from %s: %s", composeUrl, resp.Status)
	}
	defer resp.Body.Close()

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0777)
	if err != nil {
		return fmt.Errorf("error creating %s: %v", path, err)
	}

	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("failed to save response body to %s: %v", path, err)
	}
	return nil
}

// loginRegistry calls the `docker login` command to authenticate the engine to the
// specified registry with given credentials.
func loginRegistry(s dockerLoginSettings) error {
	if !s.HasLoginInfo() {
		log.Println("registry login not specificied")
		return nil
	}
	opts := []string{
		"login",
		"--email=" + s.Email,
		"--username=" + s.Username,
		"--password=" + s.Password,
	}
	if s.Server != "" {
		opts = append(opts, s.Server)
	}
	_, err := executil.Exec("docker", opts...)
	if err != nil {
		return errors.New("'docker login' failed")
	}
	return nil
}

// composeBinPath returns the path docker-compose binary should be installed at
// on the host operating system.
func composeBinPath(d driver.DistroDriver) string {
	return filepath.Join(d.DockerComposeDir(), composeBin)
}

// composeUp converts given json to yaml, saves to a file on the host and
// uses `docker-compose up -d` to create the containers.
func composeUp(d driver.DistroDriver, json map[string]interface{}, env map[string]string) error {
	if len(json) == 0 {
		log.Println("docker-compose config not specified, noop")
		return nil
	}

	// Convert json to yaml
	yaml, err := yaml.Marshal(json)
	if err != nil {
		return fmt.Errorf("error converting to compose.yml: %v", err)
	}

	if err := os.MkdirAll(composeYmlDir, 0777); err != nil {
		return fmt.Errorf("failed creating %s: %v", composeYmlDir, err)
	}
	log.Printf("Using compose yaml:>>>>>\n%s\n<<<<<", string(yaml))
	ymlPath := filepath.Join(composeYmlDir, composeYml)
	if err := ioutil.WriteFile(ymlPath, yaml, 0666); err != nil {
		return fmt.Errorf("error writing %s: %v", ymlPath, err)
	}

	// set timeout for docker-compose -> docker-engine interactions.
	// When downloading large images, docker-compose intermittently times out
	// (gh#docker/compose/issues/2186).
	os.Setenv("COMPOSE_HTTP_TIMEOUT", fmt.Sprintf("%d", composeTimeoutSecs))  // versions <= 1.4.2
	os.Setenv("DOCKER_CLIENT_TIMEOUT", fmt.Sprintf("%d", composeTimeoutSecs)) // version  >= 1.5.0
	defer os.Unsetenv("COMPOSE_HTTP_TIMEOUT")
	defer os.Unsetenv("DOCKER_CLIENT_TIMEOUT")

	// set protected environment variables to be used in docker-compose
	for k, v := range env {
		log.Printf("Setting protected environment variable '%s'.", k)
		os.Setenv(k, v)
		defer os.Unsetenv(k)
	}

	return executil.ExecPipeToFds(executil.Fds{Out: ioutil.Discard}, composeBinPath(d), "-p", composeProject, "-f", ymlPath, "up", "-d")
}

// installDockerCerts saves the configured certs to the specified dir
// if and only if the certs are not already placed there. If no certs
// are provided  or some certs already exist, nothing is written.
func installDockerCerts(s DockerHandlerSettings, dstDir string) error {
	m := []struct {
		src string
		dst string
	}{
		{s.Certs.CABase64, filepath.Join(dstDir, dockerCaCert)},
		{s.Certs.ServerCertBase64, filepath.Join(dstDir, dockerSrvCert)},
		{s.Certs.ServerKeyBase64, filepath.Join(dstDir, dockerSrvKey)},
	}

	// Check if certs are provided
	for _, v := range m {
		if len(v.src) == 0 {
			log.Printf("Docker certificate %s is not provided in the extension settings, skipping docker certs installation", v.dst)
			return nil
		}
	}

	// Check the target directory, if not create
	if ok, err := util.PathExists(dstDir); err != nil {
		return fmt.Errorf("error checking cert dir: %v", err)
	} else if !ok {
		if err := os.MkdirAll(dstDir, 0755); err != nil {
			return err
		}
	}

	// Write the certs
	for _, v := range m {
		// Decode base64
		in := strings.TrimSpace(v.src)
		f, err := base64.StdEncoding.DecodeString(in)
		if err != nil {
			// Fallback to original file input
			f = []byte(in)
		}

		if err := ioutil.WriteFile(v.dst, f, 0600); err != nil {
			return fmt.Errorf("error writing certificate: %v", err)
		}
	}
	return nil
}

func updateDockerOpts(dd driver.DistroDriver, args string) (bool, error) {
	log.Printf("Updating daemon args to: %s", args)
	restartNeeded, err := dd.UpdateDockerArgs(args)
	if err != nil {
		return false, fmt.Errorf("error updating DOCKER_OPTS: %v", err)
	}
	return restartNeeded, nil
}

// getArgs provides set of arguments that should be used in updating Docker
// daemon options based on the distro.
func getArgs(s DockerHandlerSettings, dd driver.DistroDriver) string {
	args := dd.BaseOpts()

	if s.Certs.HasDockerCerts() {
		tls := []string{"--tlsverify",
			fmt.Sprintf("--tlscacert=%s", filepath.Join(dockerCfgDir, dockerCaCert)),
			fmt.Sprintf("--tlscert=%s", filepath.Join(dockerCfgDir, dockerSrvCert)),
			fmt.Sprintf("--tlskey=%s", filepath.Join(dockerCfgDir, dockerSrvKey)),
		}
		args = append(args, tls...)
	}

	if s.Docker.Port != "" {
		args = append(args, fmt.Sprintf("-H=0.0.0.0:%s", s.Docker.Port))
	}

	if len(s.Docker.Options) > 0 {
		args = append(args, s.Docker.Options...)
	}

	return strings.Join(args, " ")
}

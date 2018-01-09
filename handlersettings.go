package main

import (
	"fmt"

	"github.com/Azure/azure-docker-extension/pkg/vmextension"
)

// publicSettings is the type deserialized from public configuration section.
type publicSettings struct {
	Installation installerSettings      `json:"installation,omitempty"`
	Docker       dockerEngineSettings   `json:"docker"`
	ComposeJson  map[string]interface{} `json:"compose"`
	ComposeEnv   map[string]string      `json:"compose-environment"`
	AzureEnv     string                 `json:"azure-environment"`
}

type installerSettings struct {
	NumRetry          int `json:"num-retry,omitempty"`
	RetryDelaySeconds int `json:"retry-delay-seconds,omitempty"`
}

// protectedSettings is the type decoded and deserialized from protected
// configuration section.
type protectedSettings struct {
	Certs               dockerCertSettings  `json:"certs"`
	Login               dockerLoginSettings `json:"login"`
	ComposeProtectedEnv map[string]string   `json:"environment"`
}

type dockerEngineSettings struct {
	Port    string   `json:"port"`
	Options []string `json:"options"`
}

type dockerLoginSettings struct {
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
}

type dockerCertSettings struct {
	CABase64         string `json:"ca"`
	ServerKeyBase64  string `json:"key"`
	ServerCertBase64 string `json:"cert"`
}

func (e dockerCertSettings) HasDockerCerts() bool {
	return e.CABase64 != "" && e.ServerKeyBase64 != "" && e.ServerCertBase64 != ""
}

func (e dockerLoginSettings) HasLoginInfo() bool {
	return e.Username != "" && e.Password != ""
}

type DockerHandlerSettings struct {
	publicSettings
	protectedSettings
}

func parseSettings(configFolder string) (*DockerHandlerSettings, error) {
	pubSettingsJSON, protSettingsJSON, err := vmextension.ReadSettings(configFolder)
	if err != nil {
		return nil, fmt.Errorf("error reading handler settings: %v", err)
	}

	var pub publicSettings
	var prot protectedSettings
	if err := vmextension.UnmarshalHandlerSettings(pubSettingsJSON, protSettingsJSON, &pub, &prot); err != nil {
		return nil, fmt.Errorf("error parsing handler settings: %v", err)
	}
	return &DockerHandlerSettings{pub, prot}, nil
}

package infraclient

import (
	"fmt"
	"os"
	"os/exec"
)

type doProviderClient interface {
	NewNode(region, size, nodeId string, values map[string]string) error
	DeleteNode(region, nodeId string) error

	AttachNode() error

	// mkdir(folder string) error
	// rmdir(folder string) error
	// getFolder(region, nodeId string) string

	// initTFdir(region, nodeId string) error

	// applyTF(region, nodeId string, values map[string]string) error
	// destroyNode(folder string) error
}

type doProvider struct {
	apiToken    string
	accountId   string
	keyPath     string
	joinToken   string
	providerDir string
	storePath   string
	tfTemplates string
	doImageId   string
}

// getFolder implements doProviderClient
func (d *doProvider) getFolder(region string, nodeId string) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s", d.storePath, d.providerDir, region, d.accountId, nodeId)
}

// initTFdir implements doProviderClient
func (d *doProvider) initTFdir(region string, nodeId string) error {

	folder := d.getFolder(region, nodeId)

	// initialize templates
	initCMD := exec.Command("cp", "-r", fmt.Sprintf("%s/%s/*", d.tfTemplates, d.providerDir), folder)
	err := initCMD.Run()

	if err != nil {
		return err
	}

	cmd := exec.Command("terraform", "init")
	cmd.Dir = folder

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// NewNode implements doProviderClient
func (d *doProvider) NewNode(region string, size string, nodeId string, values map[string]string) error {
	values["image"] = d.doImageId
	values["cluster-id"] = CLUSTER_ID
	// making dir
	err := mkdir(d.getFolder(region, nodeId))
	if err != nil {
		return err
	}

	// initialize directory
	err = d.initTFdir(region, nodeId)
	if err != nil {
		return err
	}

	// apply terraform
	return applyTF(d.getFolder(region, nodeId), values)
}

// DeleteNode implements ProviderClient
func (d *doProvider) DeleteNode(region, nodeId string) error {

	//TODO: remove node from cluster after drain proceed following

	// destroy node
	err := destroyNode(d.getFolder(region, nodeId))
	if err != nil {
		return err
	}

	// remove node state
	return rmdir(d.getFolder(region, nodeId))
}

// AttachNode implements ProviderClient
func (*doProvider) AttachNode() error {
	panic("unimplemented")
}

func NewDOProvider(apiToken, accountId, keyPath string) doProviderClient {
	return &doProvider{
		apiToken:    apiToken,
		accountId:   accountId,
		keyPath:     keyPath,
		joinToken:   "",
		providerDir: "do",
		storePath:   "/tmp",
		tfTemplates: "./terraform",
		doImageId:   "",
	}
}

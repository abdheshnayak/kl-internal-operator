package cluster

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	gonanoid "github.com/matoous/go-nanoid/v2"
)

type Client struct {
	name         string
	id           string
	kubeconfig   string
	tempFilePath string
}

func (c *Client) KubectlApply(input string) error {
	cmd := exec.Command("kubectl", "--kubeconfig", c.tempFilePath, "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(input)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (c *Client) KubectlDelete(input string) error {
	cmd := exec.Command("kubectl", "--kubeconfig", c.tempFilePath, "delete", "-f", "-")
	cmd.Stdin = strings.NewReader(input)
	return cmd.Run()
}

func (c *Client) Exec(cmd string, args ...string) ([]byte, error) {
	C := exec.Command(cmd, args...)
	C.Stderr = os.Stderr
	C.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", c.tempFilePath))
	return C.Output()
}

func (c *Client) GetName() string {
	return c.name
}

func NewCluster(kubeconfig, name string) *Client {
	// wriete kubeconfig to file

	return &Client{
		name:       name,
		kubeconfig: kubeconfig,
	}
}

func (c *Client) Init() error {
	tempFileName, err := gonanoid.New()
	if err != nil {
		return err
	}

	c.tempFilePath = fmt.Sprintf("/tmp/clus_%s", tempFileName)

	err = ioutil.WriteFile(c.tempFilePath, []byte(c.kubeconfig), 0644)
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) Destroy() error {
	return os.Remove(c.tempFilePath)
}

package infraclient

import (
	"fmt"
	"os"
	"os/exec"
)

const (
	CLUSTER_ID = "kl"
)

// rmTFdir implements doProviderClient
func rmdir(folder string) error {
	return exec.Command("rm", "-rf", folder).Run()
}

// makeTFdir implements doProviderClient
func mkdir(folder string) error {
	return exec.Command("mkdir", "-p", folder).Run()
}

// destroyNode implements doProviderClient
func destroyNode(folder string) error {
	cmd := exec.Command("terraform", "destroy")
	cmd.Dir = folder

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	return err
}

// applyTF implements doProviderClient
func applyTF(folder string, values map[string]string) error {

	vars := []string{"apply", "-auto-approve"}

	for k, v := range values {
		vars = append(vars, fmt.Sprintf("-var=%v=%v", k, v))
	}

	cmd := exec.Command("terraform", vars...)
	cmd.Dir = folder

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.Dir = folder

	return cmd.Run()
}

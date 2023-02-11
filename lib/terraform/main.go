package terraform

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Output struct {
	Value string `json:"value"`
}

func GetOutputsBytes(dir string) ([]byte, error) {
	vars := []string{"output", "-json"}
	fmt.Printf("[#] terraform %s\n", strings.Join(vars, " "))
	cmd := exec.Command("terraform", vars...)
	cmd.Dir = dir

	// cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Output()
}

func GetOutputs(dir string) (map[string]Output, error) {
	out, err := GetOutputsBytes(dir)
	if err != nil {
		return nil, err

	}

	var resp map[string]Output

	if err = json.Unmarshal(out, &resp); err != nil {
		return nil, err
	}

	if len(resp) == 0 {
		return nil, fmt.Errorf("err: no tf outputs found")
	}

	return resp, nil
}

func GetOutput(dir, key string) (string, error) {
	if resp, err := GetOutputs(dir); err != nil {
		return "", err
	} else {
		return resp[key].Value, nil
	}
}

func GetOutputBytes(dir, key string) ([]byte, error) {
	if resp, err := GetOutput(dir, key); err != nil {
		return nil, err
	} else {
		return []byte(resp), nil
	}
}

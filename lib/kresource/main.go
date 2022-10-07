package kresource

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"operators.kloudlite.io/lib/functions"
)

type Res struct {
	Cpu    int
	Memory int
}

func TestFunc() {
	totalAvailableRes, err := GetTotalResource(map[string]string{
		// "hi": "hello",
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	totalUsedRes, err := GetTotalPodRequest(map[string]string{}, "requests")

	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println((totalUsedRes.Memory * 100 / totalAvailableRes.Memory), "%")
}

// label of pool needs to be provided
func GetTotalPodRequest(labels map[string]string, limitOrSecret string) (*Res, error) {
	l := " -l "
	ls := []string{}
	for k, v := range labels {
		ls = append(ls, fmt.Sprintf("%s=%s", k, v))
	}

	l += strings.Join(ls, ",")

	out, err := functions.ExecCmd(fmt.Sprintf(`kubectl get pods -A -o jsonpath-as-json={.items[*].spec.containers[*].resources.%s}`, limitOrSecret)+l, "")
	if err != nil {
		return nil, err
	}

	// fmt.Println(string(out))

	type resource struct {
		Cpu    string
		Memory string
	}

	requests := make([]resource, 0)

	totalMemory := 0
	totalCPU := 0
	if err := json.Unmarshal(out, &requests); err != nil {
		return nil, err
	}

	for _, v := range requests {

		m := strings.ReplaceAll(v.Memory, "Mi", "")
		if strings.TrimSpace(m) != "" {

			if memory, e := strconv.ParseInt(m, 10, 32); e != nil {
				return nil, e
			} else {
				totalMemory += int(memory)
			}

		}

		c := strings.ReplaceAll(v.Cpu, "m", "")
		if strings.TrimSpace(c) != "" {
			if cpu, e := strconv.ParseInt(c, 10, 32); e != nil {
				return nil, e
			} else {
				totalCPU += int(cpu)
			}
		}

	}

	return &Res{
		Cpu:    totalCPU,
		Memory: totalMemory,
	}, nil
}

// label of pool needs to be provided
func GetTotalResource(labels map[string]string) (*Res, error) {
	l := " -l "
	ls := []string{}
	for k, v := range labels {
		ls = append(ls, fmt.Sprintf("%s=%s", k, v))
	}

	l += strings.Join(ls, ",")

	// out, err := functions.ExecCmd(
	// 	`kubectl get nodes -o jsonpath-as-json={.items[*].status.capacity}`+l, "")
	out, err := functions.ExecCmd(
		`kubectl get nodes -o jsonpath-as-json={.items[*].status.allocatable}`+l, "")

	// fmt.Println(string(out))

	if err != nil {
		return nil, err
	}

	type resource struct {
		Cpu    string
		Memory string
	}

	res := []resource{}

	err = json.Unmarshal(out, &res)
	if err != nil {
		return nil, err
	}

	totalCPU := 0
	totalMemory := 0

	for _, r := range res {

		m := strings.ReplaceAll(r.Memory, "Ki", "")
		if strings.TrimSpace(m) != "" {

			if memory, e := strconv.ParseInt(m, 10, 32); e != nil {
				return nil, e
			} else {
				totalMemory += int(memory)
			}

		}

		if strings.TrimSpace(r.Cpu) != "" {
			var cpu int64
			var e error
			c := strings.ReplaceAll(r.Cpu, "m", "")
			if c != r.Cpu {

				if cpu, e = strconv.ParseInt(c, 10, 32); e != nil {
					return nil, e
				} else {
					totalCPU += int(cpu)
				}

			} else {

				if cpu, e = strconv.ParseInt(c, 10, 32); e != nil {
					return nil, e
				} else {
					totalCPU += int(cpu) * 1000
				}

			}

		}

	}

	return &Res{
		Cpu:    totalCPU,
		Memory: totalMemory / 1024,
	}, err
}

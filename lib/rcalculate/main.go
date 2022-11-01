package rcalculate

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	ADD_NODE     int = 1
	DEL_NODE     int = -1
	ADD_STATEFUL int = 2
	DEL_STATEFUL int = -2
	NEUTRAL      int = 0
)

func Test() {
	i := Input{
		MinNode: 0,
		MaxNode: 8,
		Nodes: []Node{
			{
				Name:     "sample",
				Stateful: false,
				index:    "0",
				Size: Size{
					Memory: "1Gb",
					Cpu:    "100i",
				},
			},
		},
		TotalUsed: 0,
		Threshold: 80,
	}

	fmt.Println(i.Calculate())
}

type Size struct {
	Memory string
	Cpu    string
}

func (s *Size) getCpu() (int, error) {
	if strings.TrimSpace(s.Cpu) != "" {
		var cpu int64
		var e error
		c := strings.ReplaceAll(s.Cpu, "m", "")
		if c != s.Cpu {

			if cpu, e = strconv.ParseInt(c, 10, 32); e != nil {
				return 0, e
			} else {
				return int(cpu), nil
			}

		} else {

			if cpu, e = strconv.ParseInt(c, 10, 32); e != nil {
				return 0, e
			} else {
				return int(cpu) * 1000, nil
			}

		}

	}

	return 0, nil
}

func (s *Size) getMemory() (int, error) {
	if strings.TrimSpace(s.Memory) != "" {
		var memory int64
		var e error
		c := strings.ReplaceAll(s.Memory, "Ki", "")
		if c != s.Memory {
			if memory, e = strconv.ParseInt(c, 10, 32); e != nil {
				return 0, e
			} else {
				return int(memory) / 1024, nil
			}
		}

		c = strings.ReplaceAll(s.Memory, "Mi", "")
		if c != s.Memory {
			if memory, e = strconv.ParseInt(c, 10, 32); e != nil {
				return 0, e
			} else {
				return int(memory), nil
			}
		}

	} else {
		return 0, nil
	}

	m := strings.ReplaceAll(s.Memory, "Mi", "")
	m = strings.ReplaceAll(m, "Ki", "")
	if strings.TrimSpace(m) != "" {
		if memory, e := strconv.ParseInt(m, 10, 32); e != nil {
			return 0, e
		} else {
			return int(memory), nil
		}
	}

	return 0, nil
}

type Node struct {
	Name     string
	Stateful bool
	index    string
	Size     Size
}

type Input struct {
	MinNode      int
	MaxNode      int
	Nodes        []Node
	StatefulUsed int
	TotalUsed    int
	Threshold    int
	Buffer       int

	statefulCount     int
	statefulThreshold int

	statefulAllocatable *IntSize
	allocatable         int
}

type IntSize struct {
	Cpu    int
	Memory int
}

func (i *Input) GetStatefulCount() int {
	if i.statefulCount != 0 {
		return i.statefulCount
	}
	count := 0
	for _, n := range i.Nodes {
		if n.Stateful {
			count++
		}
	}
	i.statefulCount = count
	return count
}

func (i *Input) getStatefulAllocatable() (*IntSize, error) {
	if i.statefulAllocatable != nil {
		return i.statefulAllocatable, nil
	}
	size := IntSize{
		Cpu:    0,
		Memory: 0,
	}
	for _, n := range i.Nodes {
		if !n.Stateful {
			continue
		}

		if cpu, err := n.Size.getCpu(); err != nil {
			return nil, err
		} else {
			size.Cpu += cpu
		}

		if memory, err := n.Size.getMemory(); err != nil {
			return nil, err
		} else {
			size.Memory += memory
		}
	}

	i.statefulAllocatable = &size
	return &size, nil
}

func (i *Input) getAllocatable() (*IntSize, error) {

	size := IntSize{
		Cpu:    0,
		Memory: 0,
	}
	for _, n := range i.Nodes {
		if cpu, err := n.Size.getCpu(); err != nil {
			return nil, err
		} else {
			size.Cpu += cpu
		}

		if memory, err := n.Size.getMemory(); err != nil {
			return nil, err
		} else {
			size.Memory += memory
		}
	}
	return &size, nil
}

func (i *Input) getStatefulFilled() (int, error) {
	if i.StatefulUsed == 0 {
		return 0, nil
	}

	allocatable, err := i.getStatefulAllocatable()
	if err != nil {
		return 0, err
	}

	if allocatable.Memory == 0 && i.TotalUsed > 0 {
		return 200, nil
	} else if allocatable.Memory == 0 {
		return 0, nil
	}

	threshold := i.StatefulUsed * 100 / allocatable.Memory

	return threshold, nil
}

func (i *Input) getTotalFilled() (int, error) {
	if i.TotalUsed == 0 {
		return 0, nil
	}

	allocatable, err := i.getAllocatable()
	if err != nil {
		return 0, err
	}
	if allocatable.Memory == 0 && i.TotalUsed > 0 {
		return 200, nil
	} else if allocatable.Memory == 0 {
		return 0, nil
	}

	threshold := (i.TotalUsed + i.Buffer) * 100 / (allocatable.Memory)

	return threshold, nil
}

func (i *Input) getPerCent(val, total int) int {
	return val * 100 / total
}

func (i *Input) getFilledByAssumingLess() (int, error) {
	if i.TotalUsed == 0 {
		return 0, nil
	}
	if len(i.Nodes) == 0 {
		return 200, nil
	}

	allocatable, err := i.getAllocatable()
	if err != nil {
		return 0, err
	}

	s, err := i.Nodes[0].Size.getMemory()
	if err != nil {
		return 0, err
	}
	if (allocatable.Memory-s) == 0 && i.TotalUsed > 0 {
		return 200, nil
	}

	threshold := (i.TotalUsed + i.Buffer) * 100 / (allocatable.Memory - s)

	return threshold, nil
}

// return action, message, error
// action 1 -> add, 0 -> leave, -1 -> delete one
// ( stateful ) action 2 -> tag stateful,  -2 -> untag one Stateful
func (i *Input) Calculate() (int, *string, error) {

	fmt.Println("..................................................")
	fmt.Println("Scale-> Total Used", i.TotalUsed)
	fmt.Println("Scale-> Stateful Used", i.StatefulUsed)
	fmt.Println("Scale-> Min Node", i.MinNode)
	fmt.Println("Scale-> Max Node", i.MaxNode)
	fmt.Printf("Scale-> Filled:")
	fmt.Println(i.getTotalFilled())
	fmt.Printf("Scale-> If node delete filled:")
	fmt.Println(i.getFilledByAssumingLess())
	fmt.Printf("Scale-> Allocatable: ")
	fmt.Println(i.getAllocatable())
	fmt.Println("..................................................")

	// upscaling logincs
	{
		if len(i.Nodes) < i.MaxNode {

			if len(i.Nodes) < i.MinNode {
				return ADD_NODE, pString("nodes are less than minimum requirement, adding one"), nil
			}

			filled, err := i.getTotalFilled()
			if err != nil {
				return NEUTRAL, nil, err
			}

			if filled >= i.Threshold && len(i.Nodes) < i.MaxNode {
				return ADD_NODE, pString(fmt.Sprintf("nodes are filled %d%s and node can be added, adding one", i.Threshold, "%")), nil
			}

		}

		if len(i.Nodes) > 0 && i.GetStatefulCount() == 0 {
			return ADD_STATEFUL, pString("no stateful nodes present, convert one node to stateful"), nil
		}

		filled, err := i.getStatefulFilled()
		if err != nil {
			return NEUTRAL, nil, err
		}

		if filled >= i.Threshold && len(i.Nodes) > i.GetStatefulCount() {
			return ADD_STATEFUL, pString(fmt.Sprintf("stateful app is filled %d%s, convert one node to stateful", i.Threshold, "%s")), nil
		}

	}
	// downscaling logincs
	{
		if len(i.Nodes) > i.MinNode {
			filled, err := i.getFilledByAssumingLess()
			if err != nil {
				return NEUTRAL, nil, err
			}

			if filled < i.Threshold-5 && i.GetStatefulCount() < len(i.Nodes) {
				return DEL_NODE, pString(fmt.Sprintf("Node usage is less than %d%s if node will be deleted, deleting last node", i.Threshold-5, "%")), nil
			}
			// TODO: downgrading statefull node
		}
	}

	// fmt.Printf(`
	// ################## SCALE STATUS #####################

	// #####################################################
	// `)

	fmt.Printf(`

<==|~ Auto Scale: [ Nothing to do ] ~|==>

`)

	return NEUTRAL, pString("we can't find any actions needs to be perfomed"), nil
}

func pString(str string) *string {
	return &str
}

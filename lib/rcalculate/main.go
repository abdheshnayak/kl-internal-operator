package rcalculate

import "fmt"

func Test() {
	i := Input{
		MinNode:          0,
		MaxNode:          8,
		CurrentNodeCount: 3,
		TotalCapacity:    3000,
		Used:             2800,
		// NodeSize:         1000,
		// UpThreshold:   75,
		// DownThreshold: 50,
	}

	fmt.Println(i.Calculate())
}

type Input struct {
	MinNode          int
	MaxNode          int
	CurrentNodeCount int
	TotalCapacity    int
	Used             int
	nodeSize         int
	// UpThreshold   int
	// DownThreshold int
}

// return action, message, error
// action 1 -> add, 0 -> leave, -1 -> delete one
func (i *Input) Calculate() (int, string, error) {
	// theresold is for testing it's value can be updated according to best fit
	theresold := 100

	// calculating nodeSize ( dynamic nodeSize alog will be used later )
	if i.TotalCapacity != 0 && i.CurrentNodeCount != 0 {
		i.nodeSize = i.TotalCapacity / i.CurrentNodeCount
	}

	// validation checks
	if i.MinNode < 0 {
		return 0, "", fmt.Errorf("min node can't be negative value")
	} else if i.MaxNode <= 0 {
		return 0, "", fmt.Errorf("max node can't be negative value or zero")
	}

	// match to min and max if it's on wrong correct it
	if i.CurrentNodeCount < i.MinNode {
		return 1, fmt.Sprintf("node is less than min requirement, expanding to %d", i.CurrentNodeCount+1), nil
	} else if i.CurrentNodeCount > i.MaxNode {
		return -1, fmt.Sprintf("node is greater than max requirement, reducing to %d", i.CurrentNodeCount-1), nil
	}

	// if usage is 0 reduce node count to minimum
	if i.Used == 0 && i.CurrentNodeCount > i.MinNode {
		return -1, fmt.Sprintf("delete node due to, less uses. reducing node to %d", i.CurrentNodeCount-1), nil
	} else if i.Used == 0 && i.CurrentNodeCount == i.MinNode {
		return 0, "no resource created, and node already present on it's minimum count", nil
	}

	// if by mistake usage is more than TotalCapacity and posible to expand, expand it
	if i.Used > (i.TotalCapacity-theresold) && i.CurrentNodeCount < i.MaxNode {
		return 1, fmt.Sprintf("node is less than min requirement, expanding to %d", i.CurrentNodeCount+1), nil
	} else if i.Used > i.TotalCapacity {
		fmt.Print(i.Used, i.TotalCapacity)
		err := fmt.Sprintf("node needs to scale up, but max count reached: %d", i.CurrentNodeCount)
		return 0, err, fmt.Errorf(err)
	}

	// if it will work even we remove one node remove it ( here the better soln. can be added later )
	if i.Used < (i.TotalCapacity - i.nodeSize + theresold) {
		return -1, fmt.Sprintf("node count can be reduced by one, reducing to %d", i.CurrentNodeCount+1), nil
	}

	// if no nodes created yet create one
	if i.TotalCapacity == 0 && i.CurrentNodeCount < i.MaxNode {
		return 1, "no nodes created yet create one", nil
	}

	// any of the above case not matched
	fmt.Println()
	fmt.Println("##### may be no change needed 😇 #####")
	fmt.Println("min", i.MinNode)
	fmt.Println("max", i.MaxNode)
	fmt.Println("NodeCount", i.CurrentNodeCount)
	fmt.Println("TotalCapacity", i.TotalCapacity)
	fmt.Println("CurrentUsage", i.Used)
	fmt.Println("NodeSize", i.nodeSize)
	fmt.Println()

	return 0, "we can't process this type of combination, may be it's already in shape or our algo can't handle it", nil
}

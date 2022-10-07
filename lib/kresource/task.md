## AutoScaling Algorithm

#### requirements
- nodes ( stateless, statefull )( allocatable )
- pods ( stateless, statefull )( resources )
- pools details( min,max, threshold )

#### Steps to be performed ($ means optional)
1. while node_length < min -> create one
2. while node_length > max -> delete one
3. if statefull pod is pending and stateless node available -> convert stateless node to statefull
$. if stateless pod is pending and node_length < max -> create one
4. if usage is greater up_threshold -> create one
5. check if node can be deleted -> if true delete one


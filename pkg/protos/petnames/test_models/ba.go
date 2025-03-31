package testmodels

import (
	"math/rand"
	"slices"
)

// BarabasiAlbert generates a scale-free network using the Barabási-Albert model.
// n: Number of nodes in the network.
// m: Number of edges to add per new node (m <= n0).
// n0: Initial number of connected nodes.
func BarabasiAlbert(n, m, n0 int) map[int][]int {
	if m > n0 {
		panic("m must be less than or equal to n0")
	}

	// Initialize the network with n0 fully connected nodes.
	network := make(map[int][]int)
	for i := 0; i < n0; i++ {
		network[i] = make([]int, 0, n) // Allocate space for connections
		for j := 0; j < n0; j++ {
			if i != j {
				network[i] = append(network[i], j)
			}
		}
	}

	// Add new nodes and edges.
	for i := n0; i < n; i++ {
		network[i] = make([]int, 0, n) // Allocate space for connections

		// Select m existing nodes with probability proportional to their degree.
		for j := 0; j < m; j++ {
			selectedNode := selectNode(network)
			if i == selectedNode {
				continue
			}

			// Add edges between the new node and the selected node.
			if !slices.Contains(network[i], selectedNode) {
				network[i] = append(network[i], selectedNode)
			}

			// Add a small chance that the second node does not return the connection
			if !slices.Contains(network[selectedNode], i) {
				if rand.Float32() < 0.8 {
					network[selectedNode] = append(network[selectedNode], i)
				}
			}
		}
	}

	return network
}

// selectNode selects a node with probability proportional to its degree.
func selectNode(network map[int][]int) int {
	degrees := make([]int, 0, len(network))
	nodes := make([]int, 0, len(network))
	for node, edges := range network {
		degrees = append(degrees, len(edges))
		nodes = append(nodes, node)
	}

	totalDegree := 0
	for _, degree := range degrees {
		totalDegree += degree
	}

	if totalDegree == 0 {
		// If all nodes have degree 0, select a random node.
		return nodes[rand.Intn(len(nodes))]
	}

	// Select a node with probability proportional to its degree.
	r := rand.Intn(totalDegree)
	cumulativeDegree := 0
	for i, degree := range degrees {
		cumulativeDegree += degree
		if r < cumulativeDegree {
			return nodes[i]
		}
	}

	// Should not reach here, but return the last node as a fallback.
	return nodes[len(nodes)-1]
}

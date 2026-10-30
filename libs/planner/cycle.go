package planner

import "slices"

// stronglyConnected returns the strongly connected components of the graph given by edges
// (Tarjan's algorithm), visiting nodes in the given order so the result is deterministic.
func stronglyConnected(nodes []string, edges map[string][]string) [][]string {
	var (
		index   = map[string]int{}
		low     = map[string]int{}
		onStack = map[string]bool{}
		stack   []string
		groups  [][]string
		next    int
	)
	var visit func(n string)
	visit = func(n string) {
		index[n], low[n] = next, next
		next++
		stack = append(stack, n)
		onStack[n] = true
		for _, m := range edges[n] {
			if _, seen := index[m]; !seen {
				visit(m)
				low[n] = min(low[n], low[m])
			} else if onStack[m] {
				low[n] = min(low[n], index[m])
			}
		}
		if low[n] != index[n] {
			return
		}
		var group []string
		for {
			m := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[m] = false
			group = append(group, m)
			if m == n {
				break
			}
		}
		groups = append(groups, group)
	}
	for _, n := range nodes {
		if _, seen := index[n]; !seen {
			visit(n)
		}
	}
	return groups
}

// cycleThrough returns a path from start back to start inside group, taking edges in sorted
// order, like [a b c a].
func cycleThrough(start string, group []string, edges map[string][]string) []string {
	visited := map[string]bool{}
	var walk func(n string, path []string) []string
	walk = func(n string, path []string) []string {
		for _, m := range edges[n] {
			if !slices.Contains(group, m) {
				continue
			}
			if m == start {
				return append(path, m)
			}
			if visited[m] {
				continue
			}
			visited[m] = true
			if found := walk(m, append(path, m)); found != nil {
				return found
			}
		}
		return nil
	}
	return walk(start, []string{start})
}

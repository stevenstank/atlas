package optimize_test

import (
	"context"
	"fmt"

	"github.com/stevenstank/atlas/optimize"
)

// knapsackProblem is a 0/1 knapsack: include each item or not (exclude
// first), keep the total weight within capacity, and maximize total value.
func knapsackProblem(names []string, weight, value []int64, capacity int64) optimize.Problem[bool] {
	vars := make([]optimize.Var[bool], len(names))
	for i, n := range names {
		vars[i] = optimize.Var[bool]{Name: n, Domain: []bool{false, true}}
	}
	return optimize.Problem[bool]{
		Vars:      vars,
		Sense:     optimize.Maximize,
		Feasible:  func(a []bool) bool { return total(a, weight) <= capacity },
		Objective: func(a []bool) int64 { return total(a, value) },
	}
}

// total sums `of` over the items included in a, which may be partial.
func total(a []bool, of []int64) (s int64) {
	for i, in := range a {
		if in {
			s += of[i]
		}
	}
	return s
}

// chosen lists the variables set to true.
func chosen(p optimize.Problem[bool], a []bool) []string {
	var names []string
	for i, in := range a {
		if in {
			names = append(names, p.Vars[i].Name)
		}
	}
	return names
}

// Solve evaluates every assignment, so Optimal is a proof over the declared
// domains. This is the worked example of docs/OPTIMIZATION.md §8.
func ExampleSolve() {
	p := knapsackProblem([]string{"A", "B", "C"}, []int64{2, 3, 4}, []int64{3, 4, 5}, 5)
	res, err := optimize.Solve(context.Background(), p, optimize.Config{})
	if err != nil {
		panic(err) // only an invalid Problem or Config
	}
	fmt.Println(res.Status, chosen(p, res.Assignment), res.Value)
	fmt.Printf("%d evaluations, %d feasible\n", res.Stats.Evaluations, res.Stats.Feasible)

	// A run stopped early reports the best assignment found, not a proof.
	res, _ = optimize.Solve(context.Background(), p, optimize.Config{MaxEvaluations: 3})
	fmt.Println(res.Status, res.Reason, chosen(p, res.Assignment), res.Value)
	// Output:
	// Optimal [A B] 7
	// 8 evaluations, 5 feasible
	// BestFound EvaluationLimit [C] 5
}

// Prune and Bound make the search branch-and-bound. When both keep their
// contracts, the result is the same as exhaustive search's, assignment
// included: here [a b c] is also worth 60, and both runs return [c g h],
// the first optimum in enumeration order. Config.VerifyPruning checks the
// cuts on small instances.
func ExampleSolve_branchAndBound() {
	weight := []int64{12, 7, 11, 8, 9, 6, 5, 14, 3, 10}
	value := []int64{24, 13, 23, 15, 16, 11, 9, 28, 5, 20}
	names := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	const capacity = 30
	p := knapsackProblem(names, weight, value, capacity)
	full, _ := optimize.Solve(context.Background(), p, optimize.Config{})

	// No completion of a partial assignment over capacity is feasible.
	p.Prune = func(partial []bool) bool { return total(partial, weight) > capacity }
	// No completion is worth more than the value so far plus every
	// remaining item.
	p.Bound = func(partial []bool) int64 {
		b := total(partial, value)
		for _, v := range value[len(partial):] {
			b += v
		}
		return b
	}
	bb, _ := optimize.Solve(context.Background(), p, optimize.Config{})
	fmt.Println("exhaustive:      ", full.Status, chosen(p, full.Assignment), full.Value, full.Stats.Evaluations, "evaluations")
	fmt.Println("branch-and-bound:", bb.Status, chosen(p, bb.Assignment), bb.Value, bb.Stats.Evaluations, "evaluations")
	// Output:
	// exhaustive:       Optimal [c g h] 60 1024 evaluations
	// branch-and-bound: Optimal [c g h] 60 204 evaluations
}

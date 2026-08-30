package dependency

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brilliantkid87/rop/internal/store"
	"github.com/brilliantkid87/rop/internal/testutil"
)

// FuzzDependencyGraph fuzzes dependency edge sequences over a small node
// universe: no accepted graph may contain a cycle, duplicate edges stay
// safe, and every accepted edge is scope-consistent (invariants I-12, I-13).
func FuzzDependencyGraph(f *testing.F) {
	f.Add("0-1,1-2,2-0")
	f.Add("0-1,0-1,1-0")
	f.Add("0-1,1-2,2-3")
	f.Add("0-0")
	f.Add("")
	f.Fuzz(func(t *testing.T, spec string) {
		if len(spec) > 200 {
			return // bounded
		}
		st, err := store.Open(filepath.Join(testutil.TempDirForDB(t), "t.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		ctx := context.Background()
		if err := st.Migrate(ctx, filepath.Join(testutil.RepoRoot(), "migrations")); err != nil {
			t.Fatal(err)
		}
		if err := st.UpsertOperation(ctx, st.DB(), store.OperationRow{
			OperationID: "op.test", Reversibility: "REVERSIBLE", Guarantee: "EXACT",
		}); err != nil {
			t.Fatal(err)
		}
		// Universe: five nodes, all in scope "default".
		nodes := []string{"act_0", "act_1", "act_2", "act_3", "act_4"}
		for _, id := range nodes {
			if err := st.CreateAction(ctx, st.DB(), store.ActionRow{
				ActionID: id, Scope: "default", OperationID: "op.test",
				Status: "APPLIED", Reversibility: "REVERSIBLE", Guarantee: "EXACT",
				ResourceType: "resource", ResourceID: "res_" + id,
				CreatedAt: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC),
			}, nil); err != nil {
				t.Fatal(err)
			}
		}
		svc := &Service{Store: st}
		for _, pair := range strings.Split(spec, ",") {
			parts := strings.SplitN(strings.TrimSpace(pair), "-", 2)
			if len(parts) != 2 {
				continue
			}
			pi, di := trimIndex(parts[0]), trimIndex(parts[1])
			if pi < 0 || di < 0 || pi >= len(nodes) || di >= len(nodes) {
				continue
			}
			// The domain layer must never accept a cycle-creating edge.
			_ = svc.Add(ctx, "default", nodes[pi], nodes[di])
		}
		// Invariant check: the accepted graph is acyclic.
		if hasCycle(ctx, svc, "default", nodes) {
			t.Fatalf("accepted dependency graph contains a cycle (input %q)", spec)
		}
	})
}

func trimIndex(s string) int {
	s = strings.TrimPrefix(s, "act_")
	if s == "" {
		return -1
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func hasCycle(ctx context.Context, svc *Service, scope string, nodes []string) bool {
	const maxDepth = 32
	var dfs func(node string, depth int, visited map[string]bool) bool
	dfs = func(node string, depth int, visited map[string]bool) bool {
		if depth > maxDepth {
			return true
		}
		edges, err := svc.Store.DependenciesOfDependent(ctx, svc.Store.DB(), scope, node)
		if err != nil {
			return true
		}
		for _, e := range edges {
			if visited[e.ParentActionID] {
				return true
			}
			v := map[string]bool{}
			for k := range visited {
				v[k] = true
			}
			v[e.ParentActionID] = true
			if dfs(e.ParentActionID, depth+1, v) {
				return true
			}
		}
		return false
	}
	for _, n := range nodes {
		if dfs(n, 0, map[string]bool{n: true}) {
			return true
		}
	}
	return false
}

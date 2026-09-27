package agents

import (
	"reflect"
	"testing"
)

func TestNames(t *testing.T) {
	// pid ppid comm — a small synthetic process table.
	//   100 = tmux pane shell (zsh)
	//   200 = node launched by 100
	//   300 = claude (full path) launched by 200
	//   400 = unrelated process
	//   500 = another pane shell
	//   600 = codex under 500
	//   700 = pi directly under a pane root
	snapshot := `  100     1 /bin/zsh
  200   100 /usr/bin/node
  300   200 /Users/dt/.local/bin/claude
  400     1 /usr/sbin/syslogd
  500     1 -zsh
  600   500 /opt/homebrew/bin/codex
  700   100 /usr/local/bin/pi`
	d := newDetectorFromSnapshot(snapshot)

	tests := []struct {
		name  string
		roots []string
		want  []string
	}{
		{"nested descendant", []string{"100"}, []string{"claude", "pi"}},
		{"direct root match", []string{"300"}, []string{"claude"}},
		{"separate subtree", []string{"500"}, []string{"codex"}},
		{"multiple roots", []string{"200", "500"}, []string{"claude", "codex"}},
		{"no agents", []string{"400"}, nil},
		{"empty roots", nil, nil},
		{"unknown pid", []string{"9999"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.Names(tt.roots...)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Names(%v) = %v, want %v", tt.roots, got, tt.want)
			}
		})
	}
}

func TestNamesDistinctInOrder(t *testing.T) {
	// Two claude processes in the subtree should collapse to one, preserving
	// ps order for distinct names.
	snapshot := `  10     1 /bin/zsh
  11    10 /bin/claude
  12    10 /bin/codex
  13    10 /bin/claude`
	d := newDetectorFromSnapshot(snapshot)
	got := d.Names("10")
	want := []string{"claude", "codex"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAgentPIDs(t *testing.T) {
	// 100 -> claude (101); 200 -> codex (201); 300 is a bare shell.
	snapshot := `  100     1 /bin/zsh
  101   100 /bin/claude
  200     1 /bin/zsh
  201   200 /bin/codex
  300     1 /bin/zsh
  301   300 /bin/go`
	d := newDetectorFromSnapshot(snapshot)

	tests := []struct {
		name  string
		roots []string
		want  []int
	}{
		{"claude subtree", []string{"100"}, []int{101}},
		{"codex subtree", []string{"200"}, []int{201}},
		{"bare shell", []string{"300"}, nil},
		{"both subtrees", []string{"100", "200"}, []int{101, 201}},
		{"empty roots", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.AgentPIDs(tt.roots...)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("AgentPIDs(%v) = %v, want %v", tt.roots, got, tt.want)
			}
		})
	}
}

func TestAgents(t *testing.T) {
	t.Run("nested chain marks outermost", func(t *testing.T) {
		// 100 = pane shell; 101 = claude under it; 102 = codex nested under claude.
		snapshot := `  100     1 /bin/zsh
  101   100 /bin/claude
  102   101 /bin/codex`
		d := newDetectorFromSnapshot(snapshot)
		got := d.Agents("100")
		want := []AgentProc{
			{PID: 101, ID: "claude", Outer: true},
			{PID: 102, ID: "codex", Outer: false},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Agents = %v, want %v", got, want)
		}
	})

	t.Run("siblings both outer, ps order preserved", func(t *testing.T) {
		// Two independent agents under the same shell; neither nests the other.
		snapshot := `  100     1 /bin/zsh
  102   100 /bin/codex
  101   100 /bin/claude`
		d := newDetectorFromSnapshot(snapshot)
		got := d.Agents("100")
		want := []AgentProc{
			{PID: 102, ID: "codex", Outer: true},
			{PID: 101, ID: "claude", Outer: true},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Agents = %v, want %v", got, want)
		}
	})

	t.Run("no roots", func(t *testing.T) {
		d := newDetectorFromSnapshot("  100 1 /bin/claude")
		if got := d.Agents(); got != nil {
			t.Errorf("Agents() = %v, want nil", got)
		}
	})

	t.Run("agent above queried root is not an ancestor", func(t *testing.T) {
		// 100 = outer claude; 200 = pane shell under it; 201 = inner claude.
		// Querying only the pane subtree (200) must treat the inner claude as
		// outermost — the enclosing claude at 100 lives outside the queried
		// subtree, so it does not count as an agent ancestor.
		snapshot := `  100     1 /bin/claude
  200   100 /bin/zsh
  201   200 /bin/claude`
		d := newDetectorFromSnapshot(snapshot)
		got := d.Agents("200")
		want := []AgentProc{
			{PID: 201, ID: "claude", Outer: true},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Agents = %v, want %v", got, want)
		}
	})

	t.Run("deep claude->codex->claude chain: only topmost is outer", func(t *testing.T) {
		// 100 = pane shell; 101 = claude; 102 = codex nested; 103 = claude nested.
		// Only the topmost agent in the chain is outermost; both descendants
		// have an agent ancestor within the queried subtree.
		snapshot := `  100     1 /bin/zsh
  101   100 /bin/claude
  102   101 /bin/codex
  103   102 /bin/claude`
		d := newDetectorFromSnapshot(snapshot)
		got := d.Agents("100")
		want := []AgentProc{
			{PID: 101, ID: "claude", Outer: true},
			{PID: 102, ID: "codex", Outer: false},
			{PID: 103, ID: "claude", Outer: false},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Agents = %v, want %v", got, want)
		}
	})

	t.Run("roots present but no agents", func(t *testing.T) {
		snapshot := `  100     1 /bin/zsh
  101   100 /usr/bin/vim`
		d := newDetectorFromSnapshot(snapshot)
		if got := d.Agents("100"); got != nil {
			t.Errorf("Agents(\"100\") = %v, want nil", got)
		}
	})
}

func TestEmptyDetector(t *testing.T) {
	d := newDetectorFromSnapshot("")
	if got := d.Names("1"); got != nil {
		t.Errorf("empty detector returned %v, want nil", got)
	}
}

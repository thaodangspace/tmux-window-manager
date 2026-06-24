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

func TestEmptyDetector(t *testing.T) {
	d := newDetectorFromSnapshot("")
	if got := d.Names("1"); got != nil {
		t.Errorf("empty detector returned %v, want nil", got)
	}
}

package agents

import (
	"reflect"
	"testing"
)

func TestAgentGroups(t *testing.T) {
	tests := []struct {
		name     string
		snapshot string
		roots    []string
		want     []AgentGroup
	}{
		{
			name:     "nested chain groups under outer",
			snapshot: "100 1 zsh\n110 100 claude\n120 110 codex\n",
			roots:    []string{"100"},
			want:     []AgentGroup{{PID: 110, ID: "claude", Members: []int{110, 120}}},
		},
		{
			name:     "sibling agents are separate groups",
			snapshot: "100 1 zsh\n110 100 claude\n210 100 codex\n",
			roots:    []string{"100"},
			want: []AgentGroup{
				{PID: 110, ID: "claude", Members: []int{110}},
				{PID: 210, ID: "codex", Members: []int{210}},
			},
		},
		{
			name:     "no agents",
			snapshot: "100 1 zsh\n200 100 vim\n",
			roots:    []string{"100"},
			want:     nil,
		},
		{
			name:     "deep chain claude->codex->pi",
			snapshot: "100 1 zsh\n110 100 claude\n120 110 codex\n130 120 pi\n",
			roots:    []string{"100"},
			want:     []AgentGroup{{PID: 110, ID: "claude", Members: []int{110, 120, 130}}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDetectorFromSnapshot(tc.snapshot)
			got := d.AgentGroups(tc.roots...)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("AgentGroups() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
